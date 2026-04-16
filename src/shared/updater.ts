export type AutoUpdateMode = "auto" | "notify" | "off";

export interface UpdaterStatus {
    mode: AutoUpdateMode;
    updateAvailable: boolean;
    updateDownloaded: boolean;
    releaseVersion: string | null;
    releaseNotesUrl: string | null;
    shouldPromptForUpdate: boolean;
    isChecking: boolean;
    isDownloading: boolean;
}
