import { IpcHandlers, IpcEvents } from "@shared/types.gen";

export function invoke<K extends keyof IpcHandlers>(
    channel: K,
    ...args: Parameters<IpcHandlers[K]>
): Promise<Awaited<ReturnType<IpcHandlers[K]>>> {
    return (window as any).api.invoke(channel, ...args);
}

export function on<K extends keyof IpcEvents>(channel: K, listener: IpcEvents[K]): () => void {
    return (window as any).api.on(channel, listener);
}
