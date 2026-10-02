import type { Content } from "./types";

export type BackendStatus = "unknown" | "online" | "offline" | "maintenance";

export function isBackendDown(status: BackendStatus) {
    return status === "offline" || status === "maintenance";
}

// storing is whether the folder shows a file still on its way to object
// storage: such a folder is read again every 5 seconds, so the mark clears soon
// after the server is done. A hidden window keeps the slow interval either way.
// hasStoringContent answers it, and stops answering for a file that has been
// storing for long.
export function driveContentsRefetchInterval(
    status: BackendStatus,
    hidden: boolean,
    storing = false,
): number | false {
    if (isBackendDown(status)) return false;
    if (hidden) return 180_000;
    if (storing) return 5_000;
    return 30_000;
}

// How long after it appeared a storing file keeps its folder on the fast
// refresh. The server stores a file within seconds; one that is still on its
// way after this long is waiting for the object storage to come back, and the
// ordinary refresh is soon enough to see that end.
const STORING_FAST_REFRESH_WINDOW = 10 * 60_000;

// Whether the listing shows a file that is still on its way to object storage
// and appeared recently enough to be worth watching closely.
export function hasStoringContent(
    children?: Pick<Content, "storing" | "createdAt">[] | null,
    now = Date.now(),
) {
    return (
        children?.some((child) => {
            if (!child.storing) return false;
            const created = new Date(child.createdAt).getTime();
            // A row without a readable date is treated as new.
            return Number.isNaN(created) || now - created < STORING_FAST_REFRESH_WINDOW;
        }) ?? false
    );
}

export function driveContentsRetry(status: BackendStatus): number | false {
    if (isBackendDown(status)) return false;
    return 3;
}
