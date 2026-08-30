import type { TouchZoneSettings } from "@shared/touch-profile-settings";

export type TouchZoneSettingsChange = {
    componentId: string;
    zoneId: string;
    settings: TouchZoneSettings;
};

export function createTouchSettingsBatch(options: {
    debounceMs: number;
    save: (changes: TouchZoneSettingsChange[]) => Promise<{ previewChanged: boolean }>;
    onPendingChange: (pending: boolean) => void;
    onPreviewRequired: () => void;
    onError: (error: unknown) => void;
}) {
    const dirty = new Map<string, TouchZoneSettingsChange>();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let inFlight = false;
    let previewNeeded = false;
    let savedPreviewChanged = false;
    let disposed = false;

    const flush = async (): Promise<void> => {
        if (disposed || inFlight || dirty.size === 0) return;
        if (timer) clearTimeout(timer);
        timer = undefined;
        const changes = [...dirty.values()];
        dirty.clear();
        inFlight = true;
        try {
            const result = await options.save(changes);
            savedPreviewChanged = savedPreviewChanged || result.previewChanged;
        } catch (error) {
            previewNeeded = false;
            savedPreviewChanged = false;
            options.onError(error);
        } finally {
            inFlight = false;
            if (!disposed) {
                if (dirty.size > 0) {
                    void flush();
                } else {
                    if (savedPreviewChanged && previewNeeded) options.onPreviewRequired();
                    previewNeeded = false;
                    savedPreviewChanged = false;
                    options.onPendingChange(false);
                }
            }
        }
    };

    return {
        enqueue(changes: TouchZoneSettingsChange[], refreshPreview: boolean) {
            if (disposed) return;
            for (const change of changes) {
                dirty.set(`${change.componentId}:${change.zoneId}`, change);
            }
            previewNeeded = previewNeeded || refreshPreview;
            if (timer) clearTimeout(timer);
            timer = setTimeout(() => void flush(), options.debounceMs);
            options.onPendingChange(true);
        },
        flush,
        clear() {
            if (timer) clearTimeout(timer);
            timer = undefined;
            dirty.clear();
            previewNeeded = false;
            savedPreviewChanged = false;
            if (!inFlight) options.onPendingChange(false);
        },
        dispose() {
            disposed = true;
            if (timer) clearTimeout(timer);
            dirty.clear();
        },
    };
}
