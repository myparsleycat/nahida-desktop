import type { BrowserWindow, FileFilter } from "electron";
import { dialog } from "electron";
import { desktop } from "..";

async function runWithMainWindow<T>(runner: (window: BrowserWindow) => Promise<T>): Promise<T> {
    const window = desktop.window.main.window;
    if (!window) {
        throw new Error("Main window not found");
    }
    return runner(window);
}

export async function saveFileDialog({
    suggestedName,
    filters,
}: {
    suggestedName?: string;
    filters?: FileFilter[];
}) {
    const result = await runWithMainWindow((window) =>
        dialog.showSaveDialog(window, {
            defaultPath: suggestedName,
            filters,
        }),
    );

    if (result.canceled || !result.filePath) {
        return { canceled: true as const };
    }

    return { canceled: false as const, filePath: result.filePath };
}

export async function selectDirectoryDialog() {
    const result = await runWithMainWindow((window) =>
        dialog.showOpenDialog(window, {
            properties: ["openDirectory", "createDirectory"],
        }),
    );

    if (result.canceled || result.filePaths.length === 0) {
        return { canceled: true as const };
    }

    return { canceled: false as const, filePath: result.filePaths[0] };
}
