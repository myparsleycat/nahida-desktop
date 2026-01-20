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

    public createWatcher(dest: string | string[], options: ChokidarOptions) {
        const id = nanoid();
        // TODO: Implement watcher
    }

    public removeWatcher(id: string) {
        // TODO: Implement watcher
    }
}

export default Watcher;
