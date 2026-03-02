import type { NahidaDesktop } from "@/main";
import { DllBuilder } from "./dll-builder";
import { FixTool } from "./fix-tool";
import { TogglePersist } from "./toggle-persist";

export class ModTools {
    public readonly fixTool: FixTool;
    public readonly dllBuilder: DllBuilder;
    public readonly togglePersist: TogglePersist;

    constructor(private readonly desktop: NahidaDesktop) {
        this.fixTool = new FixTool(this.desktop);
        this.dllBuilder = new DllBuilder(this.desktop);
        this.togglePersist = new TogglePersist(this.desktop);
    }

    public async startPersistWatcher() {
        await this.togglePersist.startPersistWatcher();
    }

    public async stopPersistWatcher() {
        await this.togglePersist.stopPersistWatcher();
    }
}
