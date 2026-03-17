import { hasAnyAutoModActionsEnabled } from "@shared/auto-mod-actions";
import type { NahidaDesktop } from "@/main";
import { AutoModActions } from "./auto-mod-actions";
import { DllBuilder } from "./dll-builder";
import { FixTool } from "./fix-tool";
import { TogglePersist } from "./toggle-persist";
import { ToggleViewer } from "./toggle-viewer";

export class ModTools {
    public readonly autoModActions: AutoModActions;
    public readonly fixTool: FixTool;
    public readonly dllBuilder: DllBuilder;
    public readonly togglePersist: TogglePersist;
    public readonly toggleViewer: ToggleViewer;

    constructor(private readonly desktop: NahidaDesktop) {
        this.autoModActions = new AutoModActions(this.desktop);
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

    public async startAutoModActionsWatcher() {
        await this.autoModActions.startWatcher();
    }

    public async stopAutoModActionsWatcher() {
        await this.autoModActions.stopWatcher();
    }

    public async refreshAutoModActionsWatcher() {
        const config = await this.desktop.setting.xxmi.getAutoModActionsConfig();
        const importerKeys = this.desktop.service.xxmi
            .getEnabledImporters()
            .map((importer) => importer.key);

        if (hasAnyAutoModActionsEnabled(config, importerKeys)) {
            await this.startAutoModActionsWatcher();
            return;
        }

        await this.stopAutoModActionsWatcher();
    }

    public async restoreAutoModActionsBackups(importerKey: string) {
        return await this.autoModActions.restoreImporterBackups(importerKey);
    }
}
