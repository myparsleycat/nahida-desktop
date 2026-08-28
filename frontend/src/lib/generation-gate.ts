export function isCurrentRequest(requestId: number, latestId: { current: number }) {
    return requestId === latestId.current;
}

export async function resolveIfCurrent<T>(
    requestId: number,
    latestId: { current: number },
    work: () => Promise<T>,
): Promise<T | undefined> {
    const result = await work();
    if (!isCurrentRequest(requestId, latestId)) {
        return;
    }
    return result;
}
