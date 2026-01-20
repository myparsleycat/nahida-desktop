// preload/index.ts
import { contextBridge, ipcRenderer } from "electron";
import { electronAPI } from "@electron-toolkit/preload";
import { webUtils } from "electron";

const api = {
    invoke: (channel: string, ...args: any[]) => ipcRenderer.invoke(channel, ...args),
    send: (channel: string, ...args: any[]) => ipcRenderer.send(channel, ...args),
    on: (channel: string, listener: (...args: any[]) => void) => {
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
    // @ts-ignore
    window.electron = electronAPI;
    // @ts-ignore
    window.api = api;
    // @ts-ignore
    window.webUtils = customWebUtils;
}
