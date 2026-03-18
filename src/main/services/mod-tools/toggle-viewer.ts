import path from "node:path";
import toggleViewerUtilityWorker from "@main/worker/mod-tools/toggle-viewer.utility?modulePath";
import { toggleViewerArtifact } from "@main/internal/db/schema";
import { replaceHotkeyInGeneratedIni, sha256 } from "@main/lib/toggle-viewer-core";
import { findFiles } from "@native/native-fs";
import { and, eq } from "drizzle-orm";
import { utilityProcess, type UtilityProcess } from "electron";
import { debounce } from "es-toolkit";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "@/main";

type ToggleViewerTaskType = "scan" | "generate" | "delete";
const DEFAULT_TOGGLE_VIEWER_HOTKEY = "ctrl H";

interface ToggleViewerWorkerArtifact {
    targetIniPath: string;
    toggleTxtPath: string;
    toggleIniPath: string;
    toggleTxtHash: string;
    toggleIniHash: string;
    txtContent: string;
    iniContent: string;
}

interface ToggleViewerWorkerSuccessResponse {
    type: "success";
    reqId: string;
    artifacts: ToggleViewerWorkerArtifact[];
    seenTargetIniPaths?: string[];
    invalidIniPaths?: string[];
    logs: string[];
}

interface ToggleViewerWorkerErrorResponse {
    type: "error";
    reqId: string;
    error: string;
}

type ToggleViewerWorkerResponse =
    | ToggleViewerWorkerSuccessResponse
    | ToggleViewerWorkerErrorResponse;

interface ToggleViewerWorkerRequestBase {
    reqId: string;
}

interface ToggleViewerScanModsRequest extends ToggleViewerWorkerRequestBase {
    type: "scanModsPaths";
    modsPaths: string[];
    hotkey: string;
}

interface ToggleViewerProcessIniRequest extends ToggleViewerWorkerRequestBase {
    type: "processIniPaths";
    iniPaths: string[];
    hotkey: string;
}

type ToggleViewerWorkerRequest = ToggleViewerScanModsRequest | ToggleViewerProcessIniRequest;
type ToggleViewerWorkerRequestInput =
    | {
          type: "scanModsPaths";
          modsPaths: string[];
          hotkey: string;
      }
    | {
          type: "processIniPaths";
          iniPaths: string[];
          hotkey: string;
      };

export class ToggleViewer {
    private watcherIds: string[] = [];
    private scanDebouncer: (() => void) | null = null;
    private logs: string[] = [];
    private isScanning = false;
    private pendingScan = false;
    private pendingChangedIniPaths = new Set<string>();
    private activeAbortController: AbortController | null = null;
    private currentTask: ToggleViewerTaskType | null = null;
    private utilityChild: UtilityProcess | null = null;
    private pendingRequests = new Map<
        string,
        {
            resolve: (value: ToggleViewerWorkerSuccessResponse) => void;
            reject: (reason?: unknown) => void;
        }
    >();

    constructor(private readonly desktop: NahidaDesktop) {}

    public async startWatcher() {
        if (!this.desktop.service.xxmi) return;
        const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
        const xxmiConfig = this.desktop.service.xxmi.getXXMIConfig();
        if (!xxmiPath || !xxmiConfig) return;

        const enabled = await this.desktop.setting.xxmi.getToggleViewerAutoGenerate();
        if (!enabled) return;

        await this.stopWatcher();

        const importers = this.desktop.service.xxmi.getEnabledImporters();
        const modsPaths: string[] = [];
        for (const importer of importers) {
            const modsPath = path.join(importer.importerFolder, "mods");
            if (await fse.pathExists(modsPath)) {
                modsPaths.push(modsPath);
            }
        }

        if (modsPaths.length === 0) {
            this.logInfo("No mods folder found for enabled importers");
            return;
        }

        this.scanDebouncer = debounce(async () => {
            if (this.currentTask && this.currentTask !== "scan") return;
            if (this.currentTask === "scan" && this.activeAbortController?.signal.aborted) {
                return;
            }

            if (!this.currentTask) {
                this.currentTask = "scan";
                this.activeAbortController = new AbortController();
            }

            try {
                await this.scanChangedIniPaths({
                    signal: this.activeAbortController?.signal,
                });
            } finally {
                if (this.currentTask === "scan") {
                    this.currentTask = null;
                    this.activeAbortController = null;
                }
            }
        }, 500);

        for (const modsPath of modsPaths) {
            const watcherId = await this.desktop.lib.watcher.create(
                modsPath,
                { compareContents: true },
                async (eventName, changedPath) => {
                    const lowerChangedPath = changedPath.toLowerCase();
                    if (
                        lowerChangedPath.endsWith("\\toggle-viewer.txt") ||
                        lowerChangedPath.endsWith("\\toggle-viewer.ini") ||
                        lowerChangedPath.endsWith("/toggle-viewer.txt") ||
                        lowerChangedPath.endsWith("/toggle-viewer.ini")
                    ) {
                        return;
                    }

                    await this.handleWatcherChange(eventName, changedPath);
                    this.scanDebouncer?.();
                },
            );
            this.watcherIds.push(watcherId);
        }

        this.logInfo(`Started toggle viewer watcher (${modsPaths.length})`);

        if (!this.currentTask) {
            this.currentTask = "scan";
            this.activeAbortController = new AbortController();
            void this.scanAllImporters({ signal: this.activeAbortController.signal })
                .catch((error) => {
                    this.logError(`Initial toggle viewer scan failed: ${error}`);
                })
                .finally(() => {
                    if (this.currentTask === "scan") {
                        this.currentTask = null;
                        this.activeAbortController = null;
                    }
                });
        }
    }

    public async stopWatcher() {
        this.cancelCurrentWork();
        const count = this.watcherIds.length;
        for (const watcherId of this.watcherIds) {
            await this.desktop.lib.watcher.remove(watcherId);
        }
        this.watcherIds = [];
        this.scanDebouncer = null;
        this.pendingChangedIniPaths.clear();
        if (count > 0) {
            this.logInfo(`Stopped toggle viewer watcher (${count})`);
        }
        if (!this.currentTask) {
            this.disposeUtilityProcess();
        }
    }

    public getLogs() {
        return [...this.logs];
    }

    public getState() {
        return {
            isRunning: this.currentTask !== null,
            mode: this.currentTask,
        };
    }

    public cancelCurrentWork() {
        if (!this.activeAbortController) return;
        this.activeAbortController.abort();
        this.logInfo("Requested stop for current toggle viewer task");
    }

    public async runBatchGenerate() {
        if (this.currentTask) {
            throw new Error("Toggle viewer task is already running");
        }

        this.currentTask = "generate";
        this.activeAbortController = new AbortController();
        try {
            await this.scanAllImporters({ signal: this.activeAbortController.signal });
        } finally {
            this.currentTask = null;
            this.activeAbortController = null;

            const autoGenerateEnabled =
                await this.desktop.setting.xxmi.getToggleViewerAutoGenerate();
            if (autoGenerateEnabled && this.watcherIds.length === 0) {
                try {
                    await this.startWatcher();
                } catch (error) {
                    this.logError(
                        `Failed to start watcher after manual generate completion: ${error}`,
                    );
                }
            } else if (!autoGenerateEnabled) {
                this.disposeUtilityProcess();
            }
        }
    }

    public async runBatchDelete() {
        if (this.currentTask) {
            throw new Error("Toggle viewer task is already running");
        }

        const autoGenerateEnabled = await this.desktop.setting.xxmi.getToggleViewerAutoGenerate();
        if (autoGenerateEnabled) {
            await this.desktop.setting.xxmi.setToggleViewerAutoGenerate(false);
            this.logInfo("Disabled toggle viewer auto-generate before batch delete");
        } else {
            await this.stopWatcher();
        }

        this.currentTask = "delete";
        this.activeAbortController = new AbortController();
        const signal = this.activeAbortController.signal;

        try {
            const importers = this.desktop.service.xxmi.getEnabledImporters();
            let deletedFiles = 0;
            let deletedRecords = 0;

            const enabledModsRoots = importers.map((importer) =>
                path.resolve(path.join(importer.importerFolder, "mods")),
            );
            if (enabledModsRoots.length === 0) {
                this.logInfo("Batch delete skipped: no enabled importer mods folder");
                return;
            }
            const enabledModsRootAliases = await this.expandRootAliases(enabledModsRoots);

            const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
            const targetRecords = records.filter((record) =>
                enabledModsRootAliases.some((modsRoot) =>
                    this.isPathInRoot(record.targetIniPath, modsRoot),
                ),
            );

            for (const record of targetRecords) {
                if (signal.aborted) {
                    this.logInfo("Batch delete cancelled");
                    return;
                }

                const deletionTargets = [record.toggleTxtPath, record.toggleIniPath];
                let hasError = false;

                for (const targetPath of deletionTargets) {
                    const result = await this.removeManagedArtifactFile(targetPath);
                    if (result === "deleted") {
                        deletedFiles += 1;
                    } else if (result === "error") {
                        hasError = true;
                    }
                }

                if (hasError) {
                    this.logError(
                        `Keeping artifact record due to delete error: ${record.targetIniPath}`,
                    );
                    continue;
                }

                await this.desktop.lib.db
                    .delete(toggleViewerArtifact)
                    .where(
                        and(
                            eq(toggleViewerArtifact.id, record.id),
                            eq(toggleViewerArtifact.targetIniPath, record.targetIniPath),
                        ),
                    );
                deletedRecords += 1;
            }

            if (!signal.aborted) {
                this.logInfo(
                    `Batch delete completed. deletedFiles=${deletedFiles}, deletedRecords=${deletedRecords}, targetRecords=${targetRecords.length}`,
                );
            }
        } finally {
            this.currentTask = null;
            this.activeAbortController = null;
            this.disposeUtilityProcess();
        }
    }

    public async applyHotkeyToArtifacts(hotkey: string) {
        const normalizedHotkey = hotkey.trim() || DEFAULT_TOGGLE_VIEWER_HOTKEY;
        const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
        let updatedCount = 0;

        for (const record of records) {
            try {
                if (!(await fse.pathExists(record.toggleIniPath))) {
                    continue;
                }

                const currentContent = await fse.readFile(record.toggleIniPath, "utf-8");
                const nextContent = replaceHotkeyInGeneratedIni(currentContent, normalizedHotkey);
                if (nextContent === currentContent) {
                    continue;
                }

                await fse.writeFile(record.toggleIniPath, nextContent, "utf-8");
                const updatedAt = new Date().toISOString();
                await this.desktop.lib.db
                    .update(toggleViewerArtifact)
                    .set({
                        toggleIniHash: sha256(nextContent),
                        updatedAt,
                    })
                    .where(eq(toggleViewerArtifact.id, record.id));
                updatedCount += 1;
            } catch (error) {
                this.logError(`Failed to apply hotkey to ${record.toggleIniPath}: ${error}`);
            }
        }

        this.logInfo(`Applied toggle viewer hotkey to artifacts: updated=${updatedCount}`);
    }

    private async scanAllImporters(options?: { signal?: AbortSignal }) {
        if (this.isScanning) {
            this.pendingScan = true;
            return;
        }
        this.isScanning = true;
        const signal = options?.signal;

        try {
            const modsPaths = await this.getEnabledModsPaths();
            if (modsPaths.length === 0) {
                this.logInfo("Toggle viewer scan skipped: no enabled importer mods folder");
                return;
            }

            const toggleViewerHotkey = await this.getToggleViewerHotkey();
            const response = await this.callWorker(
                {
                    type: "scanModsPaths",
                    modsPaths,
                    hotkey: toggleViewerHotkey,
                },
                signal,
            );

            this.flushWorkerLogs(response.logs);
            await this.persistWorkerArtifacts(response.artifacts);

            if (signal?.aborted) {
                this.logInfo("Toggle viewer scan cancelled");
                return;
            }

            await this.deleteStaleRecords(new Set(response.seenTargetIniPaths ?? []));
            this.logInfo(
                `Scan complete. matched=${response.seenTargetIniPaths?.length ?? 0}, importers=${modsPaths.length}`,
            );
        } catch (error) {
            if ((error as Error).message === "Aborted") {
                this.logInfo("Toggle viewer scan cancelled");
                return;
            }
            throw error;
        } finally {
            this.isScanning = false;
            if (this.pendingScan) {
                this.pendingScan = false;
                if (!signal?.aborted) {
                    this.scanDebouncer?.();
                }
            }
        }
    }

    private async scanChangedIniPaths(options?: { signal?: AbortSignal }) {
        if (this.isScanning) {
            this.pendingScan = true;
            return;
        }
        this.isScanning = true;
        const signal = options?.signal;

        try {
            const targets = [...this.pendingChangedIniPaths];
            this.pendingChangedIniPaths.clear();

            if (targets.length === 0) {
                return;
            }

            const toggleViewerHotkey = await this.getToggleViewerHotkey();
            const response = await this.callWorker(
                {
                    type: "processIniPaths",
                    iniPaths: targets,
                    hotkey: toggleViewerHotkey,
                },
                signal,
            );

            this.flushWorkerLogs(response.logs);
            await this.persistWorkerArtifacts(response.artifacts);

            for (const invalidIniPath of response.invalidIniPaths ?? []) {
                await this.deleteArtifactRecordByTargetIniPath(invalidIniPath);
            }

            this.logInfo(
                `Incremental scan complete. queued=${targets.length}, processed=${response.artifacts.length}`,
            );
        } catch (error) {
            if ((error as Error).message === "Aborted") {
                this.logInfo("Toggle viewer scan cancelled");
                return;
            }
            throw error;
        } finally {
            this.isScanning = false;
            if (this.pendingScan) {
                this.pendingScan = false;
                if (!signal?.aborted) {
                    this.scanDebouncer?.();
                }
            }
        }
    }

    private async handleWatcherChange(
        eventName: "create" | "modify" | "remove",
        changedPath: string,
    ) {
        const normalizedPath = path.resolve(changedPath);
        if (this.isIniPath(normalizedPath)) {
            if (eventName === "remove") {
                await this.deleteArtifactRecordByTargetIniPath(normalizedPath);
                return;
            }
            this.pendingChangedIniPaths.add(normalizedPath);
            return;
        }

        if (eventName === "remove") {
            await this.deleteArtifactRecordsByPathPrefix(normalizedPath);
            return;
        }

        await this.queueIniCandidatesUnderPath(normalizedPath);
    }

    private async queueIniCandidatesUnderPath(targetPath: string) {
        try {
            const stat = await fse.stat(targetPath);
            if (stat.isDirectory()) {
                const iniPaths = findFiles(
                    [targetPath],
                    [".ini"],
                    ["toggle-viewer.ini", "disabled*"],
                ).map((candidate) => path.resolve(candidate));
                for (const iniPath of iniPaths) {
                    this.pendingChangedIniPaths.add(iniPath);
                }
            }
        } catch {}
    }

    private isIniPath(targetPath: string) {
        const lower = targetPath.toLowerCase();
        return lower.endsWith(".ini") && !lower.endsWith(`${path.sep}toggle-viewer.ini`);
    }

    private async getEnabledModsPaths() {
        const importers = this.desktop.service.xxmi.getEnabledImporters();
        const modsPaths: string[] = [];
        for (const importer of importers) {
            const modsPath = path.join(importer.importerFolder, "mods");
            if (await fse.pathExists(modsPath)) {
                modsPaths.push(modsPath);
            }
        }
        return modsPaths;
    }

    private async getToggleViewerHotkey() {
        try {
            return await this.desktop.setting.xxmi.getToggleViewerHotkey();
        } catch {
            return DEFAULT_TOGGLE_VIEWER_HOTKEY;
        }
    }

    private async callWorker(
        request: ToggleViewerWorkerRequestInput,
        signal?: AbortSignal,
    ): Promise<ToggleViewerWorkerSuccessResponse> {
        const child = this.ensureUtilityProcess();
        const reqId = nanoid();

        if (signal?.aborted) {
            throw new Error("Aborted");
        }

        return new Promise<ToggleViewerWorkerSuccessResponse>((resolve, reject) => {
            let settled = false;
            const cleanup = () => {
                if (!settled) {
                    settled = true;
                }
                this.pendingRequests.delete(reqId);
                if (signal) {
                    signal.removeEventListener("abort", abortHandler);
                }
            };

            const abortHandler = () => {
                child.postMessage({ type: "abort", reqId });
                cleanup();
                reject(new Error("Aborted"));
            };

            this.pendingRequests.set(reqId, {
                resolve: (response) => {
                    cleanup();
                    resolve(response);
                },
                reject: (reason) => {
                    cleanup();
                    reject(reason);
                },
            });

            if (signal) {
                signal.addEventListener("abort", abortHandler, { once: true });
            }

            child.postMessage({ ...request, reqId });
        });
    }

    private ensureUtilityProcess() {
        if (this.utilityChild) {
            return this.utilityChild;
        }

        const child = utilityProcess.fork(toggleViewerUtilityWorker, [], {
            stdio: "ignore",
        });

        child.on("message", (event) => {
            const response = ((event as { data?: ToggleViewerWorkerResponse }).data ??
                event) as ToggleViewerWorkerResponse;
            const pending = this.pendingRequests.get(response.reqId);
            if (!pending) {
                return;
            }

            if (response.type === "success") {
                pending.resolve(response);
                return;
            }

            pending.reject(new Error(response.error));
        });

        child.on("exit", (_code) => {
            if (this.utilityChild !== child) {
                return;
            }

            const pendingEntries = [...this.pendingRequests.values()];
            this.pendingRequests.clear();
            this.utilityChild = null;

            for (const pending of pendingEntries) {
                pending.reject(new Error("Toggle viewer utility process exited"));
            }
        });

        this.utilityChild = child;
        return child;
    }

    private disposeUtilityProcess() {
        if (!this.utilityChild) {
            return;
        }

        const child = this.utilityChild;
        this.utilityChild = null;
        child.kill();
    }

    private flushWorkerLogs(logs: string[]) {
        for (const log of logs) {
            this.logError(log);
        }
    }

    private async persistWorkerArtifacts(artifacts: ToggleViewerWorkerArtifact[]) {
        for (const artifact of artifacts) {
            await this.writeIfChanged(artifact.toggleTxtPath, artifact.txtContent);
            await this.writeIfChanged(artifact.toggleIniPath, artifact.iniContent);

            const updatedAt = new Date().toISOString();
            await this.desktop.lib.db
                .insert(toggleViewerArtifact)
                .values({
                    id: nanoid(),
                    targetIniPath: artifact.targetIniPath,
                    toggleTxtPath: artifact.toggleTxtPath,
                    toggleIniPath: artifact.toggleIniPath,
                    toggleTxtHash: artifact.toggleTxtHash,
                    toggleIniHash: artifact.toggleIniHash,
                    updatedAt,
                })
                .onConflictDoUpdate({
                    target: toggleViewerArtifact.targetIniPath,
                    set: {
                        toggleTxtPath: artifact.toggleTxtPath,
                        toggleIniPath: artifact.toggleIniPath,
                        toggleTxtHash: artifact.toggleTxtHash,
                        toggleIniHash: artifact.toggleIniHash,
                        updatedAt,
                    },
                });
        }
    }

    private async writeIfChanged(filePath: string, nextContent: string) {
        if (await fse.pathExists(filePath)) {
            const currentContent = await fse.readFile(filePath, "utf-8");
            if (currentContent === nextContent) {
                return;
            }
        }
        await fse.writeFile(filePath, nextContent, "utf-8");
    }

    private isPathInRoot(targetPath: string, rootPath: string) {
        const resolvedTarget = path.resolve(targetPath).toLowerCase();
        const resolvedRoot = path.resolve(rootPath).toLowerCase();
        return (
            resolvedTarget === resolvedRoot ||
            resolvedTarget.startsWith(`${resolvedRoot}${path.sep}`)
        );
    }

    private async expandRootAliases(roots: string[]) {
        const aliasSet = new Set<string>();

        for (const root of roots) {
            const resolvedRoot = path.resolve(root);
            aliasSet.add(resolvedRoot);

            try {
                if (await fse.pathExists(resolvedRoot)) {
                    aliasSet.add(path.resolve(await fse.realpath(resolvedRoot)));
                }
            } catch {}
        }

        return [...aliasSet];
    }

    private async removeManagedArtifactFile(filePath: string) {
        try {
            if (!(await fse.pathExists(filePath))) {
                return "missing" as const;
            }

            const stat = await fse.stat(filePath);
            if (!stat.isFile()) {
                this.logError(`Skipped non-file path while deleting artifact: ${filePath}`);
                return "error" as const;
            }

            await fse.remove(filePath);
            return "deleted" as const;
        } catch (error) {
            this.logError(`Failed to delete artifact file ${filePath}: ${error}`);
            return "error" as const;
        }
    }

    private async deleteStaleRecords(seenTargetIniPaths: Set<string>) {
        const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
        for (const record of records) {
            if (seenTargetIniPaths.has(record.targetIniPath)) continue;
            await this.deleteArtifactRecordByIdAndPath(record.id, record.targetIniPath);
        }
    }

    private async deleteArtifactRecordByTargetIniPath(targetIniPath: string) {
        const records = await this.desktop.lib.db
            .select()
            .from(toggleViewerArtifact)
            .where(eq(toggleViewerArtifact.targetIniPath, targetIniPath));
        for (const record of records) {
            await this.deleteArtifactRecordByIdAndPath(record.id, record.targetIniPath);
        }
    }

    private async deleteArtifactRecordsByPathPrefix(targetPathPrefix: string) {
        const normalizedPrefix = path.resolve(targetPathPrefix).toLowerCase();
        const records = await this.desktop.lib.db.select().from(toggleViewerArtifact);
        for (const record of records) {
            const normalizedTarget = path.resolve(record.targetIniPath).toLowerCase();
            if (
                normalizedTarget === normalizedPrefix ||
                normalizedTarget.startsWith(`${normalizedPrefix}${path.sep}`)
            ) {
                await this.deleteArtifactRecordByIdAndPath(record.id, record.targetIniPath);
            }
        }
    }

    private async deleteArtifactRecordByIdAndPath(id: string, targetIniPath: string) {
        const artifactDirPath = path.dirname(targetIniPath);
        const managedArtifactPaths = [
            path.join(artifactDirPath, "toggle-viewer.ini"),
            path.join(artifactDirPath, "toggle-viewer.txt"),
        ];

        for (const artifactPath of managedArtifactPaths) {
            try {
                await fse.unlink(artifactPath);
            } catch (error) {
                if ((error as NodeJS.ErrnoException).code === "ENOENT") {
                    continue;
                }
                this.logError(`Failed to delete managed artifact file ${artifactPath}: ${error}`);
            }
        }

        await this.desktop.lib.db
            .delete(toggleViewerArtifact)
            .where(
                and(
                    eq(toggleViewerArtifact.id, id),
                    eq(toggleViewerArtifact.targetIniPath, targetIniPath),
                ),
            );
        this.logInfo(`Removed stale toggle-viewer artifact record: ${targetIniPath}`);
    }

    private addLog(level: "INFO" | "ERROR", message: string) {
        const entry = `[${new Date().toISOString()}] [${level}] ${message}`;
        this.logs.push(entry);
        if (this.logs.length > 30) {
            this.logs = this.logs.slice(-30);
        }

        const mainWindow = this.desktop.window.main.window;
        if (mainWindow) {
            this.desktop.ipc.postMessageToWindow(
                mainWindow,
                "setting:xxmi:toggleViewerLogs",
                this.getLogs(),
            );
        }
    }

    private logInfo(message: string) {
        this.desktop.logger.info(message, "ToggleViewer");
        this.addLog("INFO", message);
    }

    private logError(message: string) {
        this.desktop.logger.error(message, "ToggleViewer");
        this.addLog("ERROR", message);
    }
}
