import type { Session } from "@shared/schemas/auth";
import type { AppStatus, TransferWithoutData } from "@shared/types";
import type { UpdaterStatus } from "@shared/updater";
import { createStore, useStore } from "zustand";

interface GlobalStore {
    appStatus: AppStatus | null;
    setAppStatus: (appStatus: AppStatus) => void;
    updateAvailable: boolean;
    setUpdateAvailable: (updateAvailable: boolean) => void;
    updateDownloaded: boolean;
    setUpdateDownloaded: (updateDownloaded: boolean) => void;
    releaseVersion: string | null;
    releaseNotesUrl: string | null;
    shouldPromptForUpdate: boolean;
    setShouldPromptForUpdate: (shouldPromptForUpdate: boolean) => void;
    updaterMode: UpdaterStatus["mode"];
    setUpdaterMode: (mode: UpdaterStatus["mode"]) => void;
    updaterChecking: boolean;
    setUpdaterChecking: (updaterChecking: boolean) => void;
    updaterDownloading: boolean;
    setUpdaterDownloading: (updaterDownloading: boolean) => void;
    setUpdaterStatus: (status: UpdaterStatus) => void;
    session: Session | null;
    sessionInitialized: boolean;
    setSession: (session: Session | null) => void;
    setSessionInitialized: (initialized: boolean) => void;
    transfers: TransferWithoutData[];
    setTransfers: (transfers: TransferWithoutData[]) => void;
}

export const globalStore = createStore<GlobalStore>((set) => {
    return {
        appStatus: null,
        setAppStatus: (appStatus) => set({ appStatus }),
        updateAvailable: false,
        setUpdateAvailable: (updateAvailable) => set({ updateAvailable }),
        updateDownloaded: false,
        setUpdateDownloaded: (updateDownloaded) => set({ updateDownloaded }),
        releaseVersion: null,
        releaseNotesUrl: null,
        shouldPromptForUpdate: false,
        setShouldPromptForUpdate: (shouldPromptForUpdate) => set({ shouldPromptForUpdate }),
        updaterMode: "auto",
        setUpdaterMode: (updaterMode) => set({ updaterMode }),
        updaterChecking: false,
        setUpdaterChecking: (updaterChecking) => set({ updaterChecking }),
        updaterDownloading: false,
        setUpdaterDownloading: (updaterDownloading) => set({ updaterDownloading }),
        setUpdaterStatus: (status) =>
            set({
                updaterMode: status.mode,
                updateAvailable: status.updateAvailable,
                updateDownloaded: status.updateDownloaded,
                releaseVersion: status.releaseVersion,
                releaseNotesUrl: status.releaseNotesUrl,
                shouldPromptForUpdate: status.shouldPromptForUpdate,
                updaterChecking: status.isChecking,
                updaterDownloading: status.isDownloading,
            }),
        session: null,
        sessionInitialized: false,
        setSession: (session) => set({ session, sessionInitialized: true }),
        setSessionInitialized: (sessionInitialized) => set({ sessionInitialized }),
        transfers: [],
        setTransfers: (transfers) => set({ transfers }),
    };
});

export function useGlobalStore<T>(selector: (state: GlobalStore) => T): T {
    return useStore(globalStore, selector);
}
