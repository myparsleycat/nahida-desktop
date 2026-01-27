import { NahidaDesktop } from "..";
import path from "node:path";
import { nanoid } from "nanoid";
import watcher, { AsyncSubscription, Event } from "@parcel/watcher";

interface WatcherOptions {
    recursive?: boolean;
    depth?: number;
}

export class Watcher {
    private readonly desktop: NahidaDesktop;
    private subscriptions: Map<string, AsyncSubscription>;
    private pathToIds: Map<string, Set<string>>;
    private idToPath: Map<string, string>;
    private pathToRecursive: Map<string, boolean>;
    private callbacks: Map<string, (eventName: string, path: string) => void>;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.subscriptions = new Map();
        this.pathToIds = new Map();
        this.idToPath = new Map();
        this.pathToRecursive = new Map();
        this.callbacks = new Map();
    }

    private handleEvents(watchedPath: string, events: Event[]) {
        const ids = this.pathToIds.get(watchedPath);
        if (!ids) return;

        const isRecursive = this.pathToRecursive.get(watchedPath);

        for (const event of events) {
            // respect depth: 0 (non-recursive)
            if (isRecursive === false) {
                if (path.dirname(event.path) !== watchedPath) {
                    continue;
                }
            }

            let eventName = event.type as string;
            if (eventName === "create") eventName = "add";
            if (eventName === "delete") eventName = "unlink";

            for (const id of ids) {
                const callback = this.callbacks.get(id);
                if (callback) {
                    callback(eventName, event.path);
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
                    const subscription = await watcher.subscribe(
                        normalizedPath,
                        (err, events) => {
                            if (err) {
                                this.desktop.logger.error(err, `Watcher:error:${normalizedPath}`);
                                return;
                            }
                            this.handleEvents(normalizedPath, events);
                        },
                        {
                            backend: "windows",
                        },
                    );
                    this.subscriptions.set(normalizedPath, subscription);
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
                        this.pathToRecursive.delete(watchedPath);
                        const subscription = this.subscriptions.get(watchedPath);
                        if (subscription) {
                            await subscription.unsubscribe();
                            this.subscriptions.delete(watchedPath);
                        }
                    }
                }
                this.idToPath.delete(id);
            }
        }
    }
}

export default Watcher;
