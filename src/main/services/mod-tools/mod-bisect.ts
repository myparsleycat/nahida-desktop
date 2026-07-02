import path from "node:path";
import { isNteImporter } from "@shared/mod";
import type { BisectSnapshot, BisectStatus, GameConfig } from "@shared/types";
import { delay, retry } from "es-toolkit";
import fg from "fast-glob";
import fse from "fs-extra";
import pLimit from "p-limit";
import type { NahidaDesktop } from "@/main";
import {
    BISECT_DISABLED_SUFFIX,
    BISECT_KEEP_DISABLED_PREFIX,
    BisectJournal,
} from "./mod-bisect-journal";
import { disabledPathFor, renameIniDisable, renameIniEnable } from "./mod-bisect-rename";

interface InternalSession {
    game: string;
    modRootPath: string;
    candidates: string[];
    round: number;
    batchSize: number;
    currentBatch: string[];
    undoStack: UndoEntry[];
    finalBadPath: string | null;
    phase: "scanning" | "active";
}

interface UndoEntry {
    disabled: string[];
    candidatesBefore: string[];
    remainingAfter: string[];
    round: number;
    batchSize: number;
}

const FIRST_BATCH_SIZE = 100;
const MEDIUM_BATCH_SIZE = 20;
const NARROW_BATCH_SIZE = 5;
const FINE_BATCH_SIZE = 1;
const RENAME_CONCURRENCY = 8;
const DISABLED_PATH_PATTERN = /^disabled/i;
const BISECT_INCONCLUSIVE_ERROR = "Bisect inconclusive";

export class ModBisect {
    private session: InternalSession | null = null;
    private readonly journal = new BisectJournal();
    public recovering: Promise<void> = Promise.resolve();

    private d3dxWatcherId: string | null = null;
    private d3dxUserIniPath: string | null = null;
    private d3dxUserIniInitial: string | null = null;
    private d3dxRestoreLock: Promise<void> = Promise.resolve();

    constructor(private readonly desktop: NahidaDesktop) {}

    public getState(): BisectSnapshot | null {
        if (!this.session) return null;
        const status = this.session.finalBadPath
            ? "done"
            : this.session.phase === "scanning"
              ? "scanning"
              : "round";
        return this.toSnapshot(this.session, null, status);
    }

    public async start(game: string): Promise<BisectSnapshot> {
        await this.recovering;
        if (this.session && !this.session.finalBadPath) {
            throw new Error("A bisect session is already running. Cancel it first.");
        }
        if (this.session?.finalBadPath) {
            await this.cancel();
        }

        const games = await this.desktop.service.mod.get.games();
        const gameConfig = games.find((g) => g.game === game);
        if (!gameConfig) {
            throw new Error(`Game not found: ${game}`);
        }
        if (isNteImporter(gameConfig.importer)) {
            throw new Error("NTE modders are not supported yet.");
        }
        if (!gameConfig.modFolderPath) {
            throw new Error(`Mod folder path is not configured for ${game}.`);
        }

        const scanningSnapshot: BisectSnapshot = {
            status: "scanning",
            game,
            modRootPath: gameConfig.modFolderPath,
            round: 0,
            batchSize: FIRST_BATCH_SIZE,
            candidates: [],
            currentBatch: [],
            undoStackDepth: 0,
            finalBadPath: null,
            error: null,
        };
        this.session = {
            game,
            modRootPath: gameConfig.modFolderPath,
            candidates: [],
            round: 0,
            batchSize: FIRST_BATCH_SIZE,
            currentBatch: [],
            undoStack: [],
            finalBadPath: null,
            phase: "scanning",
        };
        this.broadcast(scanningSnapshot);

        let iniPaths: string[];
        try {
            iniPaths = await this.scanEnabledInis(gameConfig.modFolderPath);
        } catch (error) {
            this.session = null;
            throw error;
        }

        if (iniPaths.length === 0) {
            this.session = null;
            const doneSnapshot: BisectSnapshot = {
                ...scanningSnapshot,
                status: "done",
                finalBadPath: null,
            };
            this.broadcast(doneSnapshot);
            return doneSnapshot;
        }

        if (await this.desktop.setting.get("general.bisectPreserveD3dx")) {
            await this.startD3dxGuard(game, gameConfig);
        }

        const firstBatch = iniPaths.slice(0, FIRST_BATCH_SIZE);
        try {
            await this.disableInis(firstBatch);
        } catch (error) {
            await this.enableInis(firstBatch);
            await this.stopD3dxGuard(game);
            this.session = null;
            throw error;
        }

        this.session = {
            game,
            modRootPath: gameConfig.modFolderPath,
            candidates: iniPaths,
            round: 1,
            batchSize: FIRST_BATCH_SIZE,
            currentBatch: firstBatch,
            undoStack: [],
            finalBadPath: null,
            phase: "active",
        };

        const snapshot = this.toSnapshot(this.session, null);
        this.broadcast(snapshot);
        return snapshot;
    }

    public async respond(fixed: boolean): Promise<BisectSnapshot> {
        this.assertSession();
        const session = this.session!;
        if (session.currentBatch.length === 0) {
            throw new Error("No active batch to respond to.");
        }

        const currentRound = session.round;
        const currentBatchSize = session.batchSize;
        const currentBatch = [...session.currentBatch];
        const candidatesBefore = [...session.candidates];

        let remaining: string[];
        if (fixed) {
            remaining = currentBatch;
        } else {
            const batchSet = new Set(currentBatch.map((p) => p.toLowerCase()));
            remaining = candidatesBefore.filter((p) => !batchSet.has(p.toLowerCase()));
        }

        if (remaining.length === 1) {
            const culprit = remaining[0];
            if (!fixed) {
                await this.disableInis([culprit]);
            }
            session.candidates = remaining;
            session.undoStack.push({
                disabled: currentBatch,
                candidatesBefore,
                remainingAfter: remaining,
                round: currentRound,
                batchSize: currentBatchSize,
            });
            session.currentBatch = fixed ? [] : [culprit];
            session.finalBadPath = culprit;

            const finalSnapshot = this.toSnapshot(session, null, "done");
            this.broadcast(finalSnapshot);
            return finalSnapshot;
        }

        if (remaining.length === 0) {
            const session = this.session!;
            this.session = null;
            const toRestore = this.disabledSet(session);
            await this.revertAll(session, toRestore);
            await this.stopD3dxGuard(session.game);
            const snapshot = this.toSnapshot(session, BISECT_INCONCLUSIVE_ERROR, "done");
            this.broadcast(snapshot);
            return snapshot;
        }

        const nextBatchSize = this.chooseNextBatchSize(remaining.length, currentBatchSize);
        const nextBatch = remaining.slice(0, nextBatchSize);
        await this.enableInis(currentBatch);
        await this.disableInis(nextBatch);

        session.undoStack.push({
            disabled: currentBatch,
            candidatesBefore,
            remainingAfter: remaining,
            round: currentRound,
            batchSize: currentBatchSize,
        });
        session.candidates = remaining;
        session.currentBatch = nextBatch;
        session.batchSize = nextBatchSize;
        session.round = currentRound + 1;

        const snapshot = this.toSnapshot(session, null);
        this.broadcast(snapshot);
        return snapshot;
    }

    public async undoLastRound(): Promise<BisectSnapshot> {
        this.assertSession();
        const session = this.session!;
        const lastRound = session.undoStack.pop();
        if (!lastRound) {
            throw new Error("Nothing to undo.");
        }

        await this.enableInis(session.currentBatch);

        if (lastRound.disabled.length > 0) {
            await this.disableInis(lastRound.disabled);
        }

        session.candidates = lastRound.candidatesBefore;
        session.currentBatch = lastRound.disabled;
        session.batchSize = lastRound.batchSize;
        session.round = lastRound.round;
        if (session.finalBadPath) {
            session.finalBadPath = null;
        }

        const snapshot = this.toSnapshot(session, null);
        this.broadcast(snapshot);
        return snapshot;
    }

    public async finalize(keepDisabled: string[]): Promise<BisectSnapshot> {
        this.assertSession();
        const session = this.session!;

        const keepSet = new Set(keepDisabled.map((p) => p.toLowerCase()));
        const disabledEntries = this.disabledSet(session);
        const toReEnable = disabledEntries.filter((p) => !keepSet.has(p.toLowerCase()));
        const toKeep = disabledEntries.filter((p) => keepSet.has(p.toLowerCase()));

        if (toReEnable.length > 0) {
            await this.revertAll(session, toReEnable);
        }

        for (const originalPath of toKeep) {
            await this.keepDisabled(originalPath);
        }

        await this.stopD3dxGuard(session.game);

        this.session = null;
        const finalSnapshot = this.toSnapshot(session, null);
        this.broadcast({ ...finalSnapshot, status: "reverting" });
        this.broadcast({ ...finalSnapshot, status: "idle" });
        return { ...finalSnapshot, status: "idle" };
    }

    public async cancel(): Promise<BisectSnapshot> {
        if (!this.session) {
            const idleSnapshot = this.emptySnapshot("idle");
            this.broadcast(idleSnapshot);
            return idleSnapshot;
        }
        const session = this.session;
        this.broadcast({ ...this.toSnapshot(session, null), status: "cancelled" });
        const toRestore = this.disabledSet(session);
        await this.revertAll(session, toRestore);
        await this.stopD3dxGuard(session.game);
        this.session = null;
        this.broadcast({ ...this.toSnapshot(session, null), status: "reverting" });
        const finalSnapshot = this.emptySnapshot("idle");
        this.broadcast(finalSnapshot);
        return finalSnapshot;
    }

    public async recover(games: GameConfig[]): Promise<void> {
        for (const game of games) {
            if (!game.modFolderPath || isNteImporter(game.importer)) continue;
            await this.recoverD3dxBackup(game);
            const orphans = await this.journal.listOrphans(game.modFolderPath);
            if (orphans.length === 0) continue;
            const restored = await this.recoverInis(orphans);
            for (const p of restored) {
                this.desktop.logger.info(`Restored orphan mod file: ${p}`, "ModBisect");
            }
        }
    }

    public recoverOrphans(game: string): Promise<number> {
        if (this.session) {
            return Promise.reject(new Error("Cannot recover while a bisect session is active."));
        }
        const result = this.recovering.then(() => this.performRecoverOrphans(game));
        this.recovering = result.then(
            () => undefined,
            () => undefined,
        );
        return result;
    }

    private async performRecoverOrphans(game: string): Promise<number> {
        const games = await this.desktop.service.mod.get.games();
        const gameConfig = games.find((g) => g.game === game);
        if (!gameConfig) {
            throw new Error(`Game not found: ${game}`);
        }
        if (isNteImporter(gameConfig.importer)) {
            throw new Error("NTE modders are not supported yet.");
        }
        if (!gameConfig.modFolderPath) {
            throw new Error(`Mod folder path is not configured for ${game}.`);
        }
        const orphans = await this.journal.listOrphans(gameConfig.modFolderPath);
        return (await this.recoverInis(orphans)).length;
    }

    private assertSession() {
        if (!this.session) {
            throw new Error("No active bisect session.");
        }
    }

    private disabledSet(session: InternalSession): string[] {
        return [...session.undoStack.flatMap((e) => e.disabled), ...session.currentBatch];
    }

    private async scanEnabledInis(modRootPath: string): Promise<string[]> {
        const enabledPaths = await fg(["**/*.ini"], {
            cwd: modRootPath,
            absolute: true,
            onlyFiles: true,
            dot: false,
            ignore: [
                "**/disabled*/**",
                "**/DISABLED*/**",
                "**/Disabled*/**",
                "**/disabled *",
                "**/DISABLED *",
            ],
        });

        const limit = pLimit(8);
        const filtered = await Promise.all(
            enabledPaths.map((iniPath) =>
                limit(async () =>
                    (await this.isPathDisabled(iniPath, modRootPath)) ? null : iniPath,
                ),
            ),
        );
        return filtered.filter((p): p is string => p !== null).sort();
    }

    private async isPathDisabled(filePath: string, modRootPath: string): Promise<boolean> {
        const relative = path.relative(modRootPath, filePath);
        const segments = relative.split(/[\\/]+/);
        const basename = path.basename(filePath);
        for (const segment of [...segments, basename]) {
            if (DISABLED_PATH_PATTERN.test(segment)) {
                return true;
            }
        }
        return false;
    }

    private async disableInis(paths: string[]): Promise<void> {
        if (paths.length === 0) return;
        const limit = pLimit(RENAME_CONCURRENCY);
        await Promise.all(
            paths.map((iniPath) => limit(() => renameIniDisable(iniPath, BISECT_DISABLED_SUFFIX))),
        );
    }

    private async enableInis(paths: string[]): Promise<void> {
        if (paths.length === 0) return;
        const limit = pLimit(RENAME_CONCURRENCY);
        await Promise.all(
            paths.map((iniPath) => limit(() => renameIniEnable(iniPath, BISECT_DISABLED_SUFFIX))),
        );
    }

    private async recoverInis(paths: string[]): Promise<string[]> {
        if (paths.length === 0) return [];
        const limit = pLimit(RENAME_CONCURRENCY);
        const results = await Promise.all(
            paths.map((iniPath) =>
                limit(async () => {
                    const target = disabledPathFor(iniPath, BISECT_DISABLED_SUFFIX);
                    try {
                        // The original was re-enabled by other means (re-import, backup restore, etc.),
                        // so the leftover disabled copy is a stale duplicate — remove it instead of
                        // trying to rename over the existing original (which fails on Windows).
                        if (await fse.pathExists(iniPath)) {
                            await fse.remove(target);
                        } else {
                            await fse.rename(target, iniPath);
                        }
                        return iniPath;
                    } catch (error) {
                        this.desktop.logger.error(error, "ModBisect:recover");
                        return null;
                    }
                }),
            ),
        );
        return results.filter((p): p is string => p !== null);
    }

    private async keepDisabled(originalPath: string): Promise<boolean> {
        const disabledPath = disabledPathFor(originalPath, BISECT_DISABLED_SUFFIX);
        const target = path.join(
            path.dirname(originalPath),
            `${BISECT_KEEP_DISABLED_PREFIX}${path.basename(originalPath)}`,
        );
        try {
            await fse.rename(disabledPath, target);
            return true;
        } catch (err) {
            const code = (err as NodeJS.ErrnoException).code;
            if (code === "ENOENT") {
                try {
                    await fse.rename(originalPath, target);
                    return true;
                } catch (inner) {
                    const innerCode = (inner as NodeJS.ErrnoException).code;
                    if (innerCode === "ENOENT") return false;
                    if (innerCode === "EEXIST" || innerCode === "EPERM") return true;
                    throw inner;
                }
            }
            if (code === "EEXIST" || code === "EPERM") return true;
            throw err;
        }
    }

    private async revertAll(session: InternalSession, paths: string[]): Promise<void> {
        if (paths.length === 0) return;
        await this.enableInis(paths);
    }

    private async resolveD3dxUserIniPath(gameConfig: GameConfig): Promise<string | null> {
        try {
            await this.desktop.service.xxmi?.init();
        } catch {
            // xxmi not configured
        }
        const importers = this.desktop.service.xxmi?.getEnabledImporters() ?? [];
        const importer = importers.find((i) => i.key === gameConfig.importer);
        if (!importer) return null;
        const d3dxPath = path.join(importer.importerFolder, "d3dx_user.ini");
        return (await fse.pathExists(d3dxPath)) ? d3dxPath : null;
    }

    private async startD3dxGuard(game: string, gameConfig: GameConfig): Promise<void> {
        const d3dxPath = await this.resolveD3dxUserIniPath(gameConfig);
        if (!d3dxPath) return;
        try {
            this.d3dxUserIniPath = d3dxPath;
            this.d3dxUserIniInitial = await fse.readFile(d3dxPath, "utf-8");
            await fse.outputFile(this.journal.d3dxBackupPath(game), this.d3dxUserIniInitial);
            this.d3dxWatcherId = await this.desktop.lib.watcher.create(
                d3dxPath,
                { compareContents: true },
                (eventName) => {
                    if (eventName === "modify" || eventName === "create") {
                        this.enqueueD3dxRestore();
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, "ModBisect:d3dxGuard");
            this.d3dxUserIniPath = null;
            this.d3dxUserIniInitial = null;
            await fse.remove(this.journal.d3dxBackupPath(game)).catch(() => {});
        }
    }

    private enqueueD3dxRestore(): void {
        this.d3dxRestoreLock = this.d3dxRestoreLock
            .catch(() => {})
            .then(() => this.restoreD3dxIfChanged());
        void this.d3dxRestoreLock.catch((error) => {
            this.desktop.logger.error(error, "ModBisect:d3dxRestore");
        });
    }

    private async restoreD3dxIfChanged(): Promise<void> {
        const d3dxPath = this.d3dxUserIniPath;
        const initial = this.d3dxUserIniInitial;
        if (!d3dxPath || initial === null) return;
        try {
            await delay(300);
            await retry(
                async () => {
                    const current = await fse.readFile(d3dxPath, "utf-8");
                    if (current === initial) return;
                    await fse.writeFile(d3dxPath, initial, "utf-8");
                },
                { retries: 5, delay: 200 },
            );
        } catch (error) {
            this.desktop.logger.error(error, "ModBisect:d3dxRestore");
        }
    }

    private async stopD3dxGuard(game: string): Promise<void> {
        await this.d3dxRestoreLock;
        if (this.d3dxWatcherId) {
            await this.desktop.lib.watcher.remove(this.d3dxWatcherId);
            this.d3dxWatcherId = null;
        }
        const backupPath = this.journal.d3dxBackupPath(game);
        if (this.d3dxUserIniPath && (await fse.pathExists(backupPath))) {
            try {
                await fse.copy(backupPath, this.d3dxUserIniPath, { overwrite: true });
            } catch (error) {
                this.desktop.logger.error(error, "ModBisect:d3dxFinalRestore");
            }
        }
        await fse.remove(backupPath).catch(() => {});
        this.d3dxUserIniPath = null;
        this.d3dxUserIniInitial = null;
    }

    private async recoverD3dxBackup(game: GameConfig): Promise<void> {
        const backupPath = this.journal.d3dxBackupPath(game.game);
        if (!(await fse.pathExists(backupPath))) return;
        try {
            const d3dxPath = await this.resolveD3dxUserIniPath(game);
            if (!d3dxPath) return;
            await fse.copy(backupPath, d3dxPath, { overwrite: true });
            await fse.remove(backupPath);
            this.desktop.logger.info(`Restored d3dx_user.ini backup for ${game.game}`, "ModBisect");
        } catch (error) {
            this.desktop.logger.error(error, "ModBisect:d3dxRecover");
        }
    }

    private chooseNextBatchSize(candidateCount: number, currentBatchSize: number): number {
        if (candidateCount <= FINE_BATCH_SIZE * 5) return FINE_BATCH_SIZE;
        if (candidateCount <= MEDIUM_BATCH_SIZE) return NARROW_BATCH_SIZE;
        if (candidateCount <= FIRST_BATCH_SIZE) return MEDIUM_BATCH_SIZE;
        return Math.min(currentBatchSize, FIRST_BATCH_SIZE);
    }

    private toSnapshot(
        session: InternalSession,
        error: string | null = null,
        status: BisectStatus = "round",
    ): BisectSnapshot {
        return {
            status,
            game: session.game,
            modRootPath: session.modRootPath,
            round: session.round,
            batchSize: session.batchSize,
            candidates: [...session.candidates],
            currentBatch: [...session.currentBatch],
            undoStackDepth: session.undoStack.length,
            finalBadPath: session.finalBadPath,
            error,
        };
    }

    private emptySnapshot(status: BisectStatus): BisectSnapshot {
        return {
            status,
            game: "",
            modRootPath: null,
            round: 0,
            batchSize: 0,
            candidates: [],
            currentBatch: [],
            undoStackDepth: 0,
            finalBadPath: null,
            error: null,
        };
    }

    private broadcast(snapshot: BisectSnapshot) {
        this.desktop.ipc.broadcast("tools:bisectState", snapshot);
    }
}
