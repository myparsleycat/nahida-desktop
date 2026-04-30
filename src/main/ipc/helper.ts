import type { IpcHandlers } from "@shared/types";
import { ipcMain } from "electron";

export function rh<K extends keyof IpcHandlers>(
    channel: K,
    handler: (
        ...args: Parameters<IpcHandlers[K]>
    ) => ReturnType<IpcHandlers[K]> | Promise<ReturnType<IpcHandlers[K]>>,
) {
    ipcMain.handle(channel, (_event, ...args) => {
        return handler(...(args as Parameters<IpcHandlers[K]>));
    });
}
