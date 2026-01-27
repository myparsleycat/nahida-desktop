import { NahidaDesktop } from "..";
import path from "node:path";
import { nanoid } from "nanoid";
import { Client } from "fb-watchman";
import isDev from "@main/internal/isDev";
import { app } from "electron";

const watchmanBinaryPath = app.isPackaged
    ? path.join(app.getAppPath(), "..", "watchman", "watchman.exe")
    : path.join(
          app.getAppPath(),
          "build",
          "watchman-v2025.02.24.00-windows",
          "watchman",
          "watchman.exe",
      );

interface WatcherOptions {
    recursive?: boolean;
    depth?: number;
}

interface WatchData {
    watchRoot: string;
    subscriptionName: string;
}

export class Watcher {
    private readonly desktop: NahidaDesktop;
    private client: Client;
    private pathToWatchData: Map<string, WatchData>;
    private pathToIds: Map<string, Set<string>>;
    private idToPath: Map<string, string>;
    private pathToRecursive: Map<string, boolean>;
    private callbacks: Map<string, (eventName: string, path: string) => void>;
    private subscriptionToPath: Map<string, string>;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.client = new Client({ watchmanBinaryPath });
        this.pathToWatchData = new Map();
        this.pathToIds = new Map();
        this.idToPath = new Map();
        this.pathToRecursive = new Map();
        this.callbacks = new Map();
        this.subscriptionToPath = new Map();

        this.client.on("subscription", (resp: any) => {
            this.handleEvents(resp);
        });

        this.client.on("error", (err: Error) => {
            this.desktop.logger.error(err, "Watcher:watchman:error");
        });
    }

    private handleEvents(resp: any) {
        const subscriptionName = resp.subscription;
        const watchedPath = this.subscriptionToPath.get(subscriptionName);
        if (!watchedPath) return;

        const ids = this.pathToIds.get(watchedPath);
        if (!ids) return;

        const isRecursive = this.pathToRecursive.get(watchedPath);

        for (const file of resp.files) {
            // file.name is relative to the subscription root
            const fullPath = path.join(watchedPath, file.name);

            // respect depth: 0 (non-recursive)
            if (isRecursive === false) {
                if (path.dirname(fullPath) !== watchedPath) {
                    continue;
                }
            }

            let eventName: string;
            if (file.exists === false) {
                eventName = "unlink";
            } else if (file.new) {
                eventName = "add";
            } else {
                eventName = "update";
            }

            for (const id of ids) {
                const callback = this.callbacks.get(id);
                if (callback) {
                    callback(eventName, fullPath);
                }
            }
        }
    }

    public async createWatcher(
        dest: string | string[],
        options: WatcherOptions = {},
        callback: (eventName: string, path: string) => void,
    ): Promise<string> {
        const id = nanoid();
        const paths = Array.isArray(dest) ? dest : [dest];

        let recursive = true;
        if (options.recursive === false) recursive = false;
        if (options.depth === 0) recursive = false;

        for (const p of paths) {
            const normalizedPath = path.resolve(p);

            if (!this.pathToIds.has(normalizedPath)) {
                this.pathToIds.set(normalizedPath, new Set());
                this.pathToRecursive.set(normalizedPath, recursive);

                try {
                    await this.subscribe(normalizedPath);
                } catch (error) {
                    this.desktop.logger.error(error, `Watcher:subscribe:${normalizedPath}`);
                }
            }
            this.pathToIds.get(normalizedPath)!.add(id);
            this.idToPath.set(id, normalizedPath);
        }

        this.callbacks.set(id, callback);
        return id;
    }

    private subscribe(normalizedPath: string): Promise<void> {
        return new Promise((resolve, reject) => {
            this.client.capabilityCheck({ optional: [], required: ["relative_root"] }, (err) => {
                if (err) return reject(err);

                this.client.command(["watch-project", normalizedPath], (err, resp: any) => {
                    if (err) return reject(err);

                    if (resp.warning) {
                        this.desktop.logger.warn(resp.warning, "Watcher:watchman:warning");
                    }

                    const watchRoot = resp.watch;
                    const relativePath = resp.relative_path;

                    this.client.command(["clock", watchRoot], (err, clockResp: any) => {
                        if (err) return reject(err);

                        const subscriptionName = `sub-${nanoid()}`;
                        const sub: any = {
                            fields: ["name", "size", "mtime_ms", "exists", "type", "new"],
                            since: clockResp.clock,
                        };

                        if (relativePath) {
                            sub.relative_root = relativePath;
                        }

                        this.client.command(
                            ["subscribe", watchRoot, subscriptionName, sub],
                            (err) => {
                                if (err) return reject(err);

                                this.pathToWatchData.set(normalizedPath, {
                                    watchRoot,
                                    subscriptionName,
                                });
                                this.subscriptionToPath.set(subscriptionName, normalizedPath);
                                resolve();
                            },
                        );
                    });
                });
            });
        });
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
                        const data = this.pathToWatchData.get(watchedPath);
                        if (data) {
                            this.client.command(
                                ["unsubscribe", data.watchRoot, data.subscriptionName],
                                (err) => {
                                    if (err) {
                                        this.desktop.logger.error(
                                            err,
                                            `Watcher:unsubscribe:${watchedPath}`,
                                        );
                                    }
                                },
                            );
                            this.subscriptionToPath.delete(data.subscriptionName);
                            this.pathToWatchData.delete(watchedPath);
                        }
                        this.pathToIds.delete(watchedPath);
                        this.pathToRecursive.delete(watchedPath);
                    }
                }
                this.idToPath.delete(id);
            }
        }
    }
}

export default Watcher;
