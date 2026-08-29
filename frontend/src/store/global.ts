import type { BackendStatus } from "@shared/backend";
import type { Session } from "@shared/schemas/auth";
import type { AppStatus, TransferWithoutData } from "@shared/types";
import type { UpdaterEngineError, UpdaterReleaseNotes, UpdaterStatus } from "@shared/updater";
import { createStore, useStore } from "zustand";

interface GlobalStore {
    appStatus: AppStatus | null;
    setAppStatus: (appStatus: AppStatus) => void;
    updateAvailable: boolean;
    setUpdateAvailable: (updateAvailable: boolean) => void;
    updateDownloaded: boolean;
    setUpdateDownloaded: (updateDownloaded: boolean) => void;
    releaseVersion: string | null;
    releaseNotes: UpdaterReleaseNotes | null;
    shouldPromptForUpdate: boolean;
    setShouldPromptForUpdate: (shouldPromptForUpdate: boolean) => void;
    updaterMode: UpdaterStatus["mode"];
    setUpdaterMode: (mode: UpdaterStatus["mode"]) => void;
    updaterChecking: boolean;
    setUpdaterChecking: (updaterChecking: boolean) => void;
    updaterDownloading: boolean;
    setUpdaterDownloading: (updaterDownloading: boolean) => void;
    downloadWritten: number;
    downloadTotal: number;
    updaterLastError: UpdaterEngineError | null;
    setUpdaterDownloadProgress: (written: number, total: number) => void;
    setUpdaterLastError: (error: UpdaterEngineError | null) => void;
    setUpdaterStatus: (status: UpdaterStatus) => void;
    session: Session | null;
    sessionInitialized: boolean;
    pendingSessionRestore: boolean;
    setSession: (session: Session | null) => void;
    setSessionInitialized: (initialized: boolean) => void;
    setPendingSessionRestore: (pending: boolean) => void;
    backendStatus: BackendStatus;
    setBackendStatus: (backendStatus: BackendStatus) => void;
    hasToken: boolean;
    setHasToken: (hasToken: boolean) => void;
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
        releaseNotes: null,
        shouldPromptForUpdate: false,
        setShouldPromptForUpdate: (shouldPromptForUpdate) => set({ shouldPromptForUpdate }),
        updaterMode: "auto",
        setUpdaterMode: (updaterMode) => set({ updaterMode }),
        updaterChecking: false,
        setUpdaterChecking: (updaterChecking) => set({ updaterChecking }),
        updaterDownloading: false,
        setUpdaterDownloading: (updaterDownloading) => set({ updaterDownloading }),
        downloadWritten: 0,
        downloadTotal: 0,
        updaterLastError: null,
        setUpdaterDownloadProgress: (downloadWritten, downloadTotal) =>
            set({ downloadWritten, downloadTotal, updaterLastError: null }),
        setUpdaterLastError: (updaterLastError) => set({ updaterLastError }),
        setUpdaterStatus: (status) =>
            set({
                updaterMode: status.mode,
                updateAvailable: status.updateAvailable,
                updateDownloaded: status.updateDownloaded,
                releaseVersion: status.releaseVersion,
                releaseNotes: status.releaseNotes,
                shouldPromptForUpdate: status.shouldPromptForUpdate,
                updaterChecking: status.isChecking,
                updaterDownloading: status.isDownloading,
                ...(status.isDownloading ? {} : { downloadWritten: 0, downloadTotal: 0 }),
                ...(status.isChecking || status.updateDownloaded ? { updaterLastError: null } : {}),
            }),
        session: null,
        sessionInitialized: false,
        pendingSessionRestore: false,
        setSession: (session) => set({ session, sessionInitialized: true }),
        setSessionInitialized: (sessionInitialized) => set({ sessionInitialized }),
        setPendingSessionRestore: (pendingSessionRestore) => set({ pendingSessionRestore }),
        backendStatus: "unknown",
        setBackendStatus: (backendStatus) => set({ backendStatus }),
        hasToken: false,
        setHasToken: (hasToken) => set({ hasToken }),
        transfers: [],
        setTransfers: (transfers) => set({ transfers }),
    };
});

export function useGlobalStore<T>(selector: (state: GlobalStore) => T): T {
    return useStore(globalStore, selector);
}
