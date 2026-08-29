import type { BackendStatus } from "../shared/backend";

import { getFallbackStartPage, resolveStartPage } from "../lib/start-page";

export interface BootstrapSession {
    drive: {
        rootId: string;
    };
}

export interface BootstrapServices<S extends BootstrapSession = BootstrapSession> {
    getSession: () => Promise<S | null>;
    hasToken: () => Promise<boolean>;
    getBackendStatus: () => Promise<string>;
    getDefaultStartPage: () => Promise<string>;
}

export interface FrontendBootstrap<S extends BootstrapSession = BootstrapSession> {
    session: S | null;
    hasToken: boolean;
    backendStatus: BackendStatus;
    configuredStartPage: string;
    startPage: string;
}

const BACKEND_STATUSES = new Set<BackendStatus>(["unknown", "online", "offline", "maintenance"]);

export function normalizeBackendStatus(status: string): BackendStatus {
    return BACKEND_STATUSES.has(status as BackendStatus) ? (status as BackendStatus) : "unknown";
}

export async function loadFrontendBootstrap<S extends BootstrapSession>(
    services: BootstrapServices<S>,
): Promise<FrontendBootstrap<S>> {
    let session: S | null = null;
    let hasToken = false;
    let backendStatus = "unknown" as BackendStatus;
    let configuredStartPage = getFallbackStartPage();

    try {
        session = await services.getSession();
    } catch {
        // Preserve the Electron renderer's local-first startup: session failure alone
        // must not prevent the mod manager from opening.
    }

    try {
        const [nextHasToken, backendStatusValue] = await Promise.all([
            services.hasToken(),
            services.getBackendStatus(),
        ]);
        hasToken = nextHasToken;
        backendStatus = normalizeBackendStatus(backendStatusValue);
    } catch {
        // Authentication and backend connectivity must not block local-first routes.
    }

    try {
        configuredStartPage = await services.getDefaultStartPage();
    } catch {
        // A missing or unreadable setting uses the same /mod fallback as Electron.
    }

    if (!session && hasToken && backendStatus === "online") {
        try {
            session = await services.getSession();
        } catch {
            // An online probe does not guarantee that session restoration succeeds.
        }
    }

    return {
        session,
        hasToken: Boolean(session) || hasToken,
        backendStatus,
        configuredStartPage,
        startPage: resolveStartPage(configuredStartPage, {
            isLoggedIn: Boolean(session),
            sessionRootId: session?.drive.rootId,
        }),
    };
}
