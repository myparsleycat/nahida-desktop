import type { NahidaDesktop } from "@main/index";
import { ipcMain } from "electron";

export function registerLoggerHandlers(desktop: NahidaDesktop) {
    ipcMain.on("logger:log", (_event, level, object, where) => {
        desktop.logger.log(level, object, where);
    });
}
