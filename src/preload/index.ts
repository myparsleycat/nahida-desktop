// preload/index.ts
import { contextBridge, ipcRenderer } from "electron";
import { electronAPI } from "@electron-toolkit/preload";
import { webUtils } from "electron";
import { IPC_EVENT_CHANNELS, IPC_HANDLER_CHANNELS } from "@shared/ipc-spec.gen";

const api = {
    invoke: (channel: string, ...args: any[]) => {
        if (!IPC_HANDLER_CHANNELS.includes(channel as any)) {
            throw new Error(`Unauthorized IPC channel: ${channel}`);
        }
        return ipcRenderer.invoke(channel, ...args);
    },
    send: (channel: string, ...args: any[]) => {
        if (!IPC_HANDLER_CHANNELS.includes(channel as any)) {
            throw new Error(`Unauthorized IPC channel: ${channel}`);
        }
        ipcRenderer.send(channel, ...args);
    },
    on: (channel: string, listener: (...args: any[]) => void) => {
        if (!IPC_EVENT_CHANNELS.includes(channel as any)) {
            throw new Error(`Unauthorized IPC channel: ${channel}`);
        }
        const subscription = (_event: any, ...args: any[]) => listener(...args);
        ipcRenderer.on(channel, subscription);
        return () => ipcRenderer.removeListener(channel, subscription);
    },
};

const customWebUtils = {
    getPathForFile: (file: File) => webUtils.getPathForFile(file),
};

if (process.contextIsolated) {
    try {
        contextBridge.exposeInMainWorld("electron", electronAPI);
        contextBridge.exposeInMainWorld("api", api);
        contextBridge.exposeInMainWorld("webUtils", customWebUtils);
    } catch (error) {
        console.error(error);
    }
} else {
    // @ts-expect-error
    window.electron = electronAPI;
    // @ts-expect-error
    window.api = api;
    // @ts-expect-error
    window.webUtils = customWebUtils;
}
