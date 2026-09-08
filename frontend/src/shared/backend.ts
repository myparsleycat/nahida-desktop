export type BackendStatus = "unknown" | "online" | "offline" | "maintenance";

export function isBackendDown(status: BackendStatus) {
    return status === "offline" || status === "maintenance";
}

export function driveContentsRefetchInterval(
    status: BackendStatus,
    hidden: boolean,
): number | false {
    if (isBackendDown(status)) return false;
    if (hidden) return 180_000;
    return 30_000;
}

export function driveContentsRetry(status: BackendStatus): number | false {
    if (isBackendDown(status)) return false;
    return 3;
}
