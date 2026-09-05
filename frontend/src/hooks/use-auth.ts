import { Auth } from "@bindings/auth";
import { Logger } from "@renderer/lib/logger";
import { globalStore, useGlobalStore } from "@renderer/store/global";
import type { Session } from "@shared/schemas/auth";
import { useEffect } from "react";

export function useInitializeAuth() {
    useEffect(() => {
        let mounted = true;

        const loadSession = async () => {
            let session: Session | null = null;
            let hasToken = false;
            let backendStatus = globalStore.getState().backendStatus;

            try {
                session = await Auth.GetSession();
            } catch (error) {
                Logger.capture("hooks/use-auth.ts", "Failed to load session", error);
            }

            try {
                const [nextHasToken, nextBackendStatus] = await Promise.all([
                    Auth.HasToken(),
                    Auth.GetBackendStatus(),
                ]);
                hasToken = nextHasToken;
                backendStatus = nextBackendStatus as typeof backendStatus;
            } catch (error) {
                Logger.capture("hooks/use-auth.ts", "Failed to load auth bootstrap state", error);
            }

            if (!session && hasToken && backendStatus === "online") {
                try {
                    session = await Auth.GetSession();
                } catch (error) {
                    Logger.capture(
                        "hooks/use-auth.ts",
                        "Failed to restore session after backend became online",
                        error,
                    );
                }
            }

            if (!mounted) return;

            const currentBackendStatus = globalStore.getState().backendStatus;
            globalStore.setState({
                session,
                sessionInitialized: true,
                hasToken: !!session || hasToken,
                backendStatus:
                    currentBackendStatus === "unknown" ? backendStatus : currentBackendStatus,
            });
        };

        void loadSession();

        return () => {
            mounted = false;
        };
    }, []);
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
        isBackendOffline: backendStatus === "offline" || backendStatus === "maintenance",
        refreshSession: async () => {
            const nextSession = await Auth.GetSession();
            setSession(nextSession);
            return nextSession;
        },
        startLogin: () => Auth.StartLogin(),
        startLogout: () => Auth.StartLogout(),
    };
}
