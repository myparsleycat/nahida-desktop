export type AutoUpdateMode = "auto" | "notify" | "off";

export type ReleaseNoteTranslationLanguage = "ko" | "ja" | "zh";

export interface UpdaterReleaseNotes {
    original: string | null;
    translated: string | null;
    translatedLanguage: ReleaseNoteTranslationLanguage | null;
}

export interface UpdaterStatus {
    mode: AutoUpdateMode;
    updateAvailable: boolean;
    updateDownloaded: boolean;
    releaseVersion: string | null;
    releaseNotes: UpdaterReleaseNotes | null;
    shouldPromptForUpdate: boolean;
    isChecking: boolean;
    isDownloading: boolean;
}
