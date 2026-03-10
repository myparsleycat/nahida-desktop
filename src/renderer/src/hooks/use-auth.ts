import { useEffect } from "react";
import { useGlobalStore } from "@renderer/store/global";

export function useInitializeAuth() {
  const setSession = useGlobalStore((state) => state.setSession);
  const setSessionInitialized = useGlobalStore((state) => state.setSessionInitialized);

  useEffect(() => {
    let mounted = true;

    const loadSession = async () => {
      try {
        const session = await window.api.invoke("auth:getSession");
        if (!mounted) {
          return;
        }
        setSession(session);
      } catch (error) {
        console.error("Failed to load session", error);
        if (!mounted) {
          return;
        }
        setSessionInitialized(true);
      }
    };

    void loadSession();

    return () => {
      mounted = false;
    };
  }, [setSession, setSessionInitialized]);
}

export function useAuth() {
  const session = useGlobalStore((state) => state.session);
  const sessionInitialized = useGlobalStore((state) => state.sessionInitialized);
  const setSession = useGlobalStore((state) => state.setSession);

  return {
    session,
    sessionInitialized,
    isLoggedIn: !!session,
    refreshSession: async () => {
      const nextSession = await window.api.invoke("auth:getSession");
      setSession(nextSession);
      return nextSession;
    },
    startLogin: () => window.api.invoke("auth:startLogin"),
    startLogout: () => window.api.invoke("auth:startLogout"),
  };
}
