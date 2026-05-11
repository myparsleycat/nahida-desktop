import { NativeWatcher, type WatchEvent } from "@native/fs";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";

interface WatcherOptions {
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

    public async create(
        dest: string | string[],
        options: WatcherOptions = {},
        callback: (eventName: WatchEvent["eventName"], path: string) => void,
    ): Promise<string> {
        const id = nanoid();
        const paths = Array.isArray(dest) ? dest : [dest];

        const depth = options.depth ?? -1;

        const watcher = new NativeWatcher();

        try {
            watcher.watch(
                paths,
                depth,
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
            this.watchers.set(id, watcher);
            return id;
        } catch (error) {
            this.desktop.logger.error(error, `Watcher:subscribe:${paths}`);
            throw error;
        }
    }

    public async remove(id: string) {
        const watcher = this.watchers.get(id);
        if (watcher) {
            watcher.unwatch();
            this.watchers.delete(id);
        }
    }
}

export default Watcher;
