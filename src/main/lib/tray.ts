import type { NahidaDesktop } from "@main/index";
import { app, Menu, Tray } from "electron";
import icon from "../../../resources/nahida.png?asset";

export class TrayManager {
    private desktop: NahidaDesktop;
    public tray: Tray | null = null;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public createTray() {
        this.tray = new Tray(icon);
        const contextMenu = Menu.buildFromTemplate([
            {
                label: "Check for Updates...",
                type: "normal",
                click: async () => {
                    await this.desktop.updater.checkForUpdates();
                },
            },
            {
                label: "Setting",
                type: "normal",
                click: async () => {
                    this.desktop.window.setting.focus();
                },
            },
            { type: "separator" },
            {
                label: "Quit",
                type: "normal",
                click: () => {
                    app.quit();
                },
            },
        ]);
        this.tray.setToolTip("Nahida Desktop");
        this.tray.setContextMenu(contextMenu);
        this.tray.on("click", async () => {
            this.desktop.window.main.focus();
        });
    }
}

export default TrayManager;
