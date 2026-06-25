import { rh } from "@main/ipc/helper";
import { saveFileDialog, selectDirectoryDialog } from "@main/services/dialog";

export function registerDialogHandlers() {
    rh("dialog:saveFile", saveFileDialog);
    rh("dialog:selectDirectory", selectDirectoryDialog);
}
