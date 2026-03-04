import type { NahidaDesktop } from "@/main";
import { DllBuilder } from "./dll-builder";
import { FixTool } from "./fix-tool";
import { TogglePersist } from "./toggle-persist";
import { ToggleViewer } from "./toggle-viewer";

export class ModTools {
    public readonly fixTool: FixTool;
    public readonly dllBuilder: DllBuilder;
    public readonly togglePersist: TogglePersist;
    public readonly toggleViewer: ToggleViewer;

    constructor(private readonly desktop: NahidaDesktop) {
        this.fixTool = new FixTool(this.desktop);
        this.dllBuilder = new DllBuilder(this.desktop);
        this.togglePersist = new TogglePersist(this.desktop);
        this.toggleViewer = new ToggleViewer(this.desktop);
    }

    public async startPersistWatcher() {
        await this.togglePersist.startPersistWatcher();
    }

    public async stopPersistWatcher() {
        await this.togglePersist.stopPersistWatcher();
    }

    public async startToggleViewerWatcher() {
        await this.toggleViewer.startWatcher();
    }

    public async stopToggleViewerWatcher() {
        await this.toggleViewer.stopWatcher();
    }
}
