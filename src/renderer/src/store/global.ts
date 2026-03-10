import type { Session } from "@shared/schemas/auth";
import type { AppStatus } from "@shared/types.gen";
import { createStore, useStore } from "zustand";

interface GlobalStore {
    appStatus: AppStatus | null;
    setAppStatus: (appStatus: AppStatus) => void;
    session: Session | null;
    sessionInitialized: boolean;
    setSession: (session: Session | null) => void;
    setSessionInitialized: (initialized: boolean) => void;
}

export const globalStore = createStore<GlobalStore>((set) => {
    return {
        appStatus: null,
        setAppStatus: (appStatus) => set({ appStatus }),
        session: null,
        sessionInitialized: false,
        setSession: (session) => set({ session, sessionInitialized: true }),
        setSessionInitialized: (sessionInitialized) => set({ sessionInitialized }),
    };
});

export function useGlobalStore<T>(selector: (state: GlobalStore) => T): T {
    return useStore(globalStore, selector);
}
