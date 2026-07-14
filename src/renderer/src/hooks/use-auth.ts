import { globalStore, useGlobalStore } from "@renderer/store/global";
import type { Session } from "@shared/schemas/auth";
import { useEffect } from "react";

export function useInitializeAuth() {
    const setSession = useGlobalStore((state) => state.setSession);
    const setSessionInitialized = useGlobalStore((state) => state.setSessionInitialized);
    const setBackendStatus = useGlobalStore((state) => state.setBackendStatus);
    const setHasToken = useGlobalStore((state) => state.setHasToken);

    useEffect(() => {
        let mounted = true;

        const loadSession = async () => {
            let session: Session | null = null;
            let hasToken = false;
            let backendStatus = globalStore.getState().backendStatus;

            try {
                session = await window.api.invoke("auth:getSession");
            } catch (error) {
                console.error("Failed to load session", error);
            }

            try {
                const [nextHasToken, nextBackendStatus] = await Promise.all([
                    window.api.invoke("auth:hasToken"),
                    window.api.invoke("backend:getStatus"),
                ]);
                hasToken = nextHasToken;
                backendStatus = nextBackendStatus;
            } catch (error) {
                console.error("Failed to load auth bootstrap state", error);
            }

            if (!mounted) return;

            setSession(session);
            setHasToken(!!session || hasToken);
            setBackendStatus(backendStatus);
            setSessionInitialized(true);
        };

        void loadSession();

        return () => {
            mounted = false;
        };
    }, [setSession, setSessionInitialized, setBackendStatus, setHasToken]);
}

export function useAuth() {
    const session = useGlobalStore((state) => state.session);
    const sessionInitialized = useGlobalStore((state) => state.sessionInitialized);
    const setSession = useGlobalStore((state) => state.setSession);
    const backendStatus = useGlobalStore((state) => state.backendStatus);
    const hasToken = useGlobalStore((state) => state.hasToken);

    return {
        session,
        sessionInitialized,
        backendStatus,
        hasToken,
        isLoggedIn: !!session,
        isBackendOffline: backendStatus === "offline",
        refreshSession: async () => {
            const nextSession = await window.api.invoke("auth:getSession");
            setSession(nextSession);
            return nextSession;
        },
        startLogin: () => window.api.invoke("auth:startLogin"),
        startLogout: () => window.api.invoke("auth:startLogout"),
    };
}
