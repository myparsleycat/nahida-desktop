import { ipcMain } from "electron";
import type { NahidaDesktop } from "@main/index";

export function registerLoggerHandlers(desktop: NahidaDesktop) {
    ipcMain.on("logger:log", (_event, level, object, where) => {
        desktop.logger.log(level, object, where);
    });
}
