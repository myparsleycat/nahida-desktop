export function unavailableChannel(channel: string): Promise<any> {
    return Promise.reject(new Error(`${channel} is not available in the Wails backend`));
}
