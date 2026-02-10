import { desktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerPathSelectorHandlers() {
    rh("pathSelector:selectFolderPath", async (selectionId) => {
        return desktop.lib.pathSelector.selectFolderPath(selectionId);
    });

    rh("pathSelector:selectModManagerPath", async (selectionId, path, fileName) => {
        return desktop.lib.pathSelector.selectModManagerPath(selectionId, path, fileName);
    });

    rh("pathSelector:cancel", async (selectionId) => {
        return desktop.lib.pathSelector.cancelSelection(selectionId);
    });
}
