import { ipcMain } from "electron";
import { IpcHandlers } from "@shared/types.gen";

export function rh<K extends keyof IpcHandlers>(
    channel: K,
    handler: (
        ...args: Parameters<IpcHandlers[K]>
    ) => ReturnType<IpcHandlers[K]> | Promise<ReturnType<IpcHandlers[K]>>,
) {
    ipcMain.handle(channel, (_event, ...args) => {
        return handler(...(args as any));
    });
}
