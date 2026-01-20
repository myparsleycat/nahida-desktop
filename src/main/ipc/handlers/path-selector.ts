import { ipcMain } from "electron";
import { desktop } from "@main/index";

export function registerPathSelectorHandlers() {
    ipcMain.handle("pathSelector:selectFolderPath", async (_event, selectionId) => {
        return desktop.lib.pathSelector.selectFolderPath(selectionId);
    });

    ipcMain.handle("pathSelector:selectModManagerPath", async (_event, selectionId, path) => {
        return desktop.lib.pathSelector.selectModManagerPath(selectionId, path);
    });

    ipcMain.handle("pathSelector:cancel", async (_event, selectionId) => {
        return desktop.lib.pathSelector.cancelSelection(selectionId);
    });
}
