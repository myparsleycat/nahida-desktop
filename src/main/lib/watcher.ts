import { NahidaDesktop } from "..";
import path from "node:path";
import { app } from "electron";
import { spawn, ChildProcess } from "node:child_process";
import { nanoid } from "nanoid";

interface WatcherOptions {
    recursive?: boolean;
}

export class Watcher {
    private readonly desktop: NahidaDesktop;
    private process: ChildProcess | null = null;
    private callbacks: Map<string, (eventName: string, path: string) => void>;
    private pathToIds: Map<string, Set<string>>;
    private idToPath: Map<string, string>;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.callbacks = new Map();
        this.pathToIds = new Map();
        this.idToPath = new Map();
        this.startProcess();
    }

    private getWatcherPath(): string {
        if (app.isPackaged) {
            return path.join(app.getAppPath(), "..", "watcher.exe");
        }
        return path.join(app.getAppPath(), "build", "watcher", "watcher.exe");
    }

    private startProcess() {
        const exePath = this.getWatcherPath();
        this.process = spawn(exePath);

        this.process.stdout?.on("data", (data) => {
            const lines = data.toString().split("\n");
            for (const line of lines) {
                if (!line.trim()) continue;
                try {
                    const msg = JSON.parse(line);
                    this.handleMessage(msg);
                } catch (e) {
                    console.error("Failed to parse watcher output:", line, e);
                }
            }
        });

        this.process.stderr?.on("data", (data) => {
            console.error("Watcher stderr:", data.toString());
        });

        this.process.on("close", (code) => {
            console.log("Watcher process exited with code", code);
            this.process = null;
            setTimeout(() => this.startProcess(), 1000);
        });
    }

    private handleMessage(msg: any) {
        if (msg.event === "error") {
            console.error("Watcher error:", msg.info);
            return;
        }

        for (const [watchedRoot, ids] of this.pathToIds) {
            if (msg.path.startsWith(watchedRoot)) {
                for (const id of ids) {
                    const callback = this.callbacks.get(id);
                    if (callback) {
                        callback(msg.event, msg.path);
                    }
                }
            }
        }
    }

    public createWatcher(
        dest: string | string[],
        options: { recursive?: boolean; depth?: number } = {},
        callback: (eventName: string, path: string) => void,
    ): string {
        const id = nanoid();
        const paths = Array.isArray(dest) ? dest : [dest];

        let recursive = true;
        if (options.recursive === false) recursive = false;
        if (options.depth === 0) recursive = false;

        for (const p of paths) {
            const normalizedPath = path.resolve(p);

            if (!this.pathToIds.has(normalizedPath)) {
                this.pathToIds.set(normalizedPath, new Set());
                // Send command to Go
                this.sendCommand({
                    action: "watch",
                    path: normalizedPath,
                    recursive: recursive,
                });
            }
            this.pathToIds.get(normalizedPath)!.add(id);
            this.idToPath.set(id, normalizedPath);
        }

        this.callbacks.set(id, callback);
        return id;
    }

    public async removeWatcher(id: string) {
        const callback = this.callbacks.get(id);
        if (callback) {
            this.callbacks.delete(id);
            const watchedPath = this.idToPath.get(id);
            if (watchedPath) {
                const ids = this.pathToIds.get(watchedPath);
                if (ids) {
                    ids.delete(id);
                    if (ids.size === 0) {
                        this.pathToIds.delete(watchedPath);
                        this.sendCommand({
                            action: "unwatch",
                            path: watchedPath,
                        });
                    }
                }
                this.idToPath.delete(id);
            }
        }
    }

    private sendCommand(cmd: any) {
        if (this.process && this.process.stdin) {
            this.process.stdin.write(JSON.stringify(cmd) + "\n");
        }
    }
}

export default Watcher;
