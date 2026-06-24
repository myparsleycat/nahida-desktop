import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import { dialog } from "electron";

export function registerDialogHandlers(d: NahidaDesktop) {
    rh("dialog:saveFile", async ({ suggestedName, filters }) => {
        const window = d.window.main.window;
        const result = await dialog.showSaveDialog(window ?? undefined!, {
            defaultPath: suggestedName,
            filters,
        });

        if (result.canceled || !result.filePath) {
            return { canceled: true as const };
        }

        return { canceled: false as const, filePath: result.filePath };
    });

    rh("dialog:selectDirectory", async () => {
        const window = d.window.main.window;
        const result = await dialog.showOpenDialog(window ?? undefined!, {
            properties: ["openDirectory", "createDirectory"],
        });

        if (result.canceled || result.filePaths.length === 0) {
            return { canceled: true as const };
        }

        return { canceled: false as const, filePath: result.filePaths[0] };
    });
}
