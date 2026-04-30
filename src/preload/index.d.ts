import { ElectronAPI } from "@electron-toolkit/preload";
import { IpcSendChannel } from "../shared/ipc-keys.gen";
import type { IpcHandlers, IpcEvents } from "../shared/types";

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
            send(channel: IpcSendChannel, ...args: any[]): void;
            on<T extends keyof IpcEvents>(
                channel: T,
                listener: (...args: Parameters<IpcEvents[T]>) => void,
            ): () => void;
        };
    }
}
