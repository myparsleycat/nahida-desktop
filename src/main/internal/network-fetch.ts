import { net, session, type Session } from "electron";

type FetchInput = string | URL | Request;

const DOWNLOAD_SESSION_PARTITION_PREFIX = "nahida-download";

const idleDownloadSessions: Session[] = [];
let downloadSessionSequence = 0;

/** Use Chromium's default network session for ordinary API requests. */
export function networkFetch(input: FetchInput, init?: RequestInit): Promise<Response> {
    if (typeof net?.fetch === "function") {
        return net.fetch(input instanceof URL ? input.toString() : input, init);
    }

    return globalThis.fetch(input, init);
}

/**
 * Lease an isolated Chromium session for one file or parallel worker.
 * Closing its connections cannot interrupt another active download context.
 */
export function createDownloadNetworkContext() {
    const downloadSession = acquireDownloadSession();
    if (!downloadSession) {
        return {
            fetch: networkFetch,
            resetConnections: () => Promise.resolve(),
            release: () => {},
        };
    }

    let connectionReset: Promise<void> | undefined;
    let released = false;

    return {
        fetch: (input: FetchInput, init?: RequestInit) =>
            downloadSession.fetch(input instanceof URL ? input.toString() : input, init),
        resetConnections: () => {
            if (connectionReset) return connectionReset;

            const reset = downloadSession.closeAllConnections().finally(() => {
                if (connectionReset === reset) connectionReset = undefined;
            });
            connectionReset = reset;
            return reset;
        },
        release: () => {
            if (released) return;
            released = true;
            if (!connectionReset) {
                idleDownloadSessions.push(downloadSession);
                return;
            }
            void connectionReset.finally(() => {
                idleDownloadSessions.push(downloadSession);
            });
        },
    };
}

function acquireDownloadSession() {
    const idleSession = idleDownloadSessions.pop();
    if (idleSession) return idleSession;
    if (typeof session?.fromPartition !== "function") return undefined;

    return session.fromPartition(
        `${DOWNLOAD_SESSION_PARTITION_PREFIX}-${downloadSessionSequence++}`,
        { cache: false },
    );
}

export function resetDownloadSessionsForTests() {
    idleDownloadSessions.length = 0;
    downloadSessionSequence = 0;
}
