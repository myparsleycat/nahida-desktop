import path from "node:path";
import type { NahidaDesktop } from "../..";
import fse from "fs-extra";
import type { ModLibraryService } from "./library";

const WATCHER_SETTLE_DELAY_MS = 800;
const FILE_READY_CHECK_INTERVAL_MS = 200;
const FILE_READY_MAX_ATTEMPTS = 10;

export class ModWatchersService {
    private gameWatcherId: string | null = null;
    private characterWatcherId: string | null = null;
    private gameUpdateTimer: NodeJS.Timeout | null = null;
    private characterUpdateTimer: NodeJS.Timeout | null = null;
    private gameUpdateToken = 0;
    private characterUpdateToken = 0;

    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly library: ModLibraryService,
    ) {}

    public async watchGame(game: string) {
        const modFolderPath = await this.library.gamePath(game);
        if (!modFolderPath) return;

        if (this.gameWatcherId) {
            await this.desktop.lib.watcher.remove(this.gameWatcherId);
            this.gameWatcherId = null;
        }
        if (this.gameUpdateTimer) {
            clearTimeout(this.gameUpdateTimer);
            this.gameUpdateTimer = null;
        }
        this.gameUpdateToken += 1;

        try {
            this.gameWatcherId = await this.desktop.lib.watcher.create(
                modFolderPath,
                { depth: 1 },
                (event, changedPath) => {
                    if (event === "create" || event === "modify" || event === "remove") {
                        this.scheduleGameUpdate(event, changedPath);
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:watchGame:${game}`);
        }
    }

    public async watchCharacter(characterPath: string) {
        if (this.characterWatcherId) {
            await this.desktop.lib.watcher.remove(this.characterWatcherId);
            this.characterWatcherId = null;
        }
        if (this.characterUpdateTimer) {
            clearTimeout(this.characterUpdateTimer);
            this.characterUpdateTimer = null;
        }
        this.characterUpdateToken += 1;

        try {
            this.characterWatcherId = await this.desktop.lib.watcher.create(
                characterPath,
                { depth: 1 },
                (event, changedPath) => {
                    if (event === "create" || event === "modify" || event === "remove") {
                        this.scheduleCharacterUpdate(event, changedPath);
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:watchCharacter:${characterPath}`);
        }
    }

    private scheduleGameUpdate(event: "create" | "modify" | "remove", changedPath: string) {
        this.gameUpdateToken += 1;
        const token = this.gameUpdateToken;

        if (this.gameUpdateTimer) {
            clearTimeout(this.gameUpdateTimer);
        }

        this.gameUpdateTimer = setTimeout(async () => {
            await this.waitForPathReady(event, changedPath, token, "game");
            if (token !== this.gameUpdateToken) return;

            if (this.desktop.window.main.window) {
                this.desktop.ipc.postMessageToWindow(
                    this.desktop.window.main.window,
                    "mod:update-game",
                );
            }
        }, WATCHER_SETTLE_DELAY_MS);
    }

    private scheduleCharacterUpdate(event: "create" | "modify" | "remove", changedPath: string) {
        this.characterUpdateToken += 1;
        const token = this.characterUpdateToken;

        if (this.characterUpdateTimer) {
            clearTimeout(this.characterUpdateTimer);
        }

        this.characterUpdateTimer = setTimeout(async () => {
            await this.waitForPathReady(event, changedPath, token, "character");
            if (token !== this.characterUpdateToken) return;

            if (this.desktop.window.main.window) {
                this.desktop.ipc.postMessageToWindow(
                    this.desktop.window.main.window,
                    "mod:update-mods",
                );
            }
        }, WATCHER_SETTLE_DELAY_MS);
    }

    private async waitForPathReady(
        event: "create" | "modify" | "remove",
        changedPath: string,
        token: number,
        watcherType: "game" | "character",
    ) {
        if (event === "remove") {
            return;
        }

        for (let attempt = 0; attempt < FILE_READY_MAX_ATTEMPTS; attempt += 1) {
            if (!this.isLatestToken(watcherType, token)) {
                return;
            }

            try {
                const current = await getPathSnapshot(changedPath);
                if (!current) {
                    return;
                }

                await sleep(FILE_READY_CHECK_INTERVAL_MS);

                if (!this.isLatestToken(watcherType, token)) {
                    return;
                }

                const next = await getPathSnapshot(changedPath);
                if (next && isSameSnapshot(current, next)) {
                    return;
                }
            } catch (error) {
                if (!isTransientFsError(error)) {
                    this.desktop.logger.warn(
                        `Failed to stabilize watched file: ${changedPath}`,
                        "ModWatchersService.waitForPathReady",
                    );
                    return;
                }
            }

            await sleep(FILE_READY_CHECK_INTERVAL_MS);
        }
    }

    private isLatestToken(watcherType: "game" | "character", token: number) {
        return watcherType === "game"
            ? token === this.gameUpdateToken
            : token === this.characterUpdateToken;
    }
}

function isTransientFsError(error: unknown) {
    if (!error || typeof error !== "object" || !("code" in error)) {
        return false;
    }

    return (
        error.code === "ENOENT" ||
        error.code === "EBUSY" ||
        error.code === "EPERM" ||
        error.code === "EACCES"
    );
}

async function sleep(ms: number) {
    await new Promise((resolve) => setTimeout(resolve, ms));
}

interface PathSnapshot {
    kind: "file" | "directory";
    fileCount: number;
    directoryCount: number;
    totalSize: number;
    latestMtimeMs: number;
}

async function getPathSnapshot(targetPath: string): Promise<PathSnapshot | null> {
    const stat = await fse.stat(targetPath);

    if (stat.isFile()) {
        return {
            kind: "file",
            fileCount: 1,
            directoryCount: 0,
            totalSize: stat.size,
            latestMtimeMs: stat.mtimeMs,
        };
    }

    if (!stat.isDirectory()) {
        return null;
    }

    let fileCount = 0;
    let directoryCount = 1;
    let totalSize = 0;
    let latestMtimeMs = stat.mtimeMs;
    const pending = [targetPath];

    while (pending.length > 0) {
        const currentPath = pending.pop();
        if (!currentPath) {
            continue;
        }

        const entries = await fse.readdir(currentPath, { withFileTypes: true });

        for (const entry of entries) {
            const entryPath = path.join(currentPath, entry.name);
            const entryStat = await fse.stat(entryPath);
            latestMtimeMs = Math.max(latestMtimeMs, entryStat.mtimeMs);

            if (entryStat.isDirectory()) {
                directoryCount += 1;
                pending.push(entryPath);
                continue;
            }

            if (entryStat.isFile()) {
                fileCount += 1;
                totalSize += entryStat.size;
            }
        }
    }

    return {
        kind: "directory",
        fileCount,
        directoryCount,
        totalSize,
        latestMtimeMs,
    };
}

function isSameSnapshot(a: PathSnapshot, b: PathSnapshot) {
    return (
        a.kind === b.kind &&
        a.fileCount === b.fileCount &&
        a.directoryCount === b.directoryCount &&
        a.totalSize === b.totalSize &&
        a.latestMtimeMs === b.latestMtimeMs
    );
}
