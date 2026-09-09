export function unavailableChannel(channel: string): Promise<unknown> {
    return Promise.reject(new Error(`${channel} is not available in the Wails backend`));
}
