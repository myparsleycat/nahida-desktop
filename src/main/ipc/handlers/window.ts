import type { NahidaDesktop } from "@main/index";
import { BrowserWindow, ipcMain } from "electron";
import { rh } from "../helper";

export function registerWindowHandlers(d: NahidaDesktop) {
    rh("window:openSetting", async () => {
        await d.window.main.focusAndNavigate("/setting/gen");
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
                if (!win.isDestroyed()) {
                    win.destroy();
                }
                break;
        }
    });
}
