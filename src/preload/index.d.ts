import { ElectronAPI } from "@electron-toolkit/preload";
import { IpcHandlers, IpcEvents } from "../shared/types";

declare global {
    interface Window {
        electron: ElectronAPI;
        webUtils: {
            getPathForFile: (file: File) => string;
        };
        api: {
            invoke<T extends keyof IpcHandlers>(
                channel: T,
                ...args: Parameters<IpcHandlers[T]>
            ): Promise<Awaited<ReturnType<IpcHandlers[T]>>>;
            send<T extends keyof IpcHandlers>(
                channel: T,
                ...args: Parameters<IpcHandlers[T]>
            ): void;
            on<T extends keyof IpcEvents>(
                channel: T,
                listener: (...args: Parameters<IpcEvents[T]>) => void,
            ): () => void;
        };
    }
}
