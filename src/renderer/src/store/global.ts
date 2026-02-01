import { createStore, useStore } from "zustand";
import type { AppStatus } from "@shared/types";

import type { Session } from "@shared/schemas/auth";

interface GlobalStore {
    appStatus: AppStatus | null;
    setAppStatus: (appStatus: AppStatus) => void;
    session: Session | null;
    setSession: (session: Session | null) => void;
}

export const globalStore = createStore<GlobalStore>((set) => {
    return {
        appStatus: null,
        setAppStatus: (appStatus) => set({ appStatus }),
        session: null,
        setSession: (session) => set({ session }),
    };
});

export function useGlobalStore<T>(selector: (state: GlobalStore) => T): T {
    return useStore(globalStore, selector);
}
