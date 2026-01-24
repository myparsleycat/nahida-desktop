import { NahidaDesktop } from "..";
import chokidar, { ChokidarOptions, type FSWatcher } from "chokidar";
import { nanoid } from "nanoid";

export class Watcher {
    private readonly desktop: NahidaDesktop;
    private watchers: Map<string, FSWatcher>;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.watchers = new Map();
    }

    public createWatcher(
        dest: string | string[],
        options: ChokidarOptions,
        callback: (eventName: string, path: string) => void,
    ): string {
        const id = nanoid();
        const watcher = chokidar.watch(dest, options);

        watcher.on("all", (event, path) => {
            callback(event, path);
        });

        this.watchers.set(id, watcher);
        return id;
    }

    public async removeWatcher(id: string) {
        const watcher = this.watchers.get(id);
        if (watcher) {
            await watcher.close();
            this.watchers.delete(id);
        }
    }
}

export default Watcher;
