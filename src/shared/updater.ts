export type AutoUpdateMode = "auto" | "notify" | "off";

export interface UpdaterStatus {
    mode: AutoUpdateMode;
    updateAvailable: boolean;
    updateDownloaded: boolean;
    shouldPromptForUpdate: boolean;
    isChecking: boolean;
    isDownloading: boolean;
}
