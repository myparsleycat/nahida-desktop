import { app, Menu, Tray } from "electron";
import icon from "../../../resources/nahida.png?asset";
import type { NahidaDesktop } from "@main/index";

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
                label: "Check Update",
                type: "normal",
                click: async () => {
                    await this.desktop.updater.checkForUpdates(true);
                },
            },
            {
                label: "Setting",
                type: "normal",
                click: async () => {
                    const loggedIn = await this.desktop.service.auth.isLoggedIn();
                    if (loggedIn) {
                        this.desktop.window.setting.createSettingWindow();
                    } else {
                        this.desktop.window.auth.createLoginWindow();
                    }
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
            const loggedIn = await this.desktop.service.auth.isLoggedIn();
            if (loggedIn) {
                this.desktop.window.main.createMainWindow();
            } else {
                this.desktop.window.auth.createLoginWindow();
            }
        });
    }
}

export default TrayManager;
