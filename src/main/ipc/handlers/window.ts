import { BrowserWindow, ipcMain } from "electron";
import { rh } from "../helper";
import type { NahidaDesktop } from "@main/index";

export function registerWindowHandlers(d: NahidaDesktop) {
    rh("window:openSetting", async () => {
        d.window.setting.createSettingWindow();
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
                } else {
                    win.close();
                }
                break;
        }
    });
}
