import type { NahidaDesktop } from "@main/index";
import { BrowserWindow, ipcMain } from "electron";
import { rh } from "../helper";

export function registerWindowHandlers(d: NahidaDesktop) {
    rh("window:openSetting", async () => {
        d.window.setting.createSettingWindow();
    });

    rh("overlay:setIgnoreMouseEvents", async (ignore: boolean) => {
        d.window.overlay.setIgnoreMouseEvents(ignore);
    });

    ipcMain.on("window-control", (event, command) => {
        const win = BrowserWindow.fromWebContents(event.sender);
        if (!win) return;

        switch (command) {
            case "minimize":
                win.minimize();
                break;
            case "maximize":
                if (win.isMaximized()) {
                    win.unmaximize();
                } else {
                    win.maximize();
                }
                break;
            case "close":
                if (win === d.window.main.window) {
                    win.hide();
                } else if (!win.isDestroyed()) {
                    win.destroy();
                }
                break;
        }
    });
}
