import { NativeWatcher, type WatchEvent } from "@native/native-fs";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";

interface WatcherOptions {
    recursive?: boolean;
    depth?: number;
    compareContents?: boolean;
    pollIntervalMs?: number;
}

export class Watcher {
    private readonly desktop: NahidaDesktop;
    private watchers: Map<string, NativeWatcher>;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.watchers = new Map();
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

        const watcher = new NativeWatcher();

        try {
            watcher.watch(
                paths,
                recursive,
                {
                    compareContents: options.compareContents,
                    pollIntervalMs: options.pollIntervalMs,
                },
                (err: Error | null, event: WatchEvent) => {
                    if (err) {
                        this.desktop.logger.error(err, `Watcher:error:${id}`);
                        return;
                    }
                    if (event) {
                        callback(event.eventName, event.path);
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, `Watcher:subscribe:${paths}`);
        }

        this.watchers.set(id, watcher);
        return id;
    }

    public async removeWatcher(id: string) {
        const watcher = this.watchers.get(id);
        if (watcher) {
            watcher.unwatch();
            this.watchers.delete(id);
        }
    }
}

export default Watcher;
