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

export interface UpdaterEngineProgress {
    written: number;
    total: number;
    rate?: number;
}

export interface UpdaterEngineError {
    stage: string;
    message: string;
    provider?: string;
}

export function updaterEventPayload(data: unknown): Record<string, unknown> | null {
    if (Array.isArray(data)) {
        return updaterEventPayload(data[0]);
    }
    if (data && typeof data === "object") {
        return data as Record<string, unknown>;
    }
    return null;
}

export function parseUpdaterProgress(data: unknown): UpdaterEngineProgress | null {
    const payload = updaterEventPayload(data);
    if (!payload) {
        return null;
    }
    const written = Number(payload.written);
    const total = Number(payload.total);
    if (!Number.isFinite(written) || !Number.isFinite(total)) {
        return null;
    }
    const rate = Number(payload.rate);
    return {
        written,
        total,
        rate: Number.isFinite(rate) ? rate : undefined,
    };
}

export function parseUpdaterError(data: unknown): UpdaterEngineError | null {
    const payload = updaterEventPayload(data);
    if (!payload) {
        return null;
    }
    const message = typeof payload.message === "string" ? payload.message : "";
    if (!message) {
        return null;
    }
    return {
        stage: typeof payload.stage === "string" ? payload.stage : "",
        message,
        provider: typeof payload.provider === "string" ? payload.provider : undefined,
    };
}
