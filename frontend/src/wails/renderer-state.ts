import { Setting } from "@bindings/setting";
import { Logger } from "@renderer/lib/logger";

// These keys lived in localStorage before renderer state moved to the application database.
const LEGACY_LOCAL_STORAGE_KEYS = [
    "nahida.gamebanana.view",
    "nahida.menu-maker.drafts",
    "nahida.menu-maker.settings",
    "nahida.menu-maker.iconify.favorites",
];

const cache = new Map<string, string>();
// Until the stored rows are loaded the cache is not a view of the database, and a value derived
// from it would overwrite rows it never saw. Writes stay in memory for the session in that case.
let loaded = false;
// Keys whose last write failed, so an identical value is still sent again.
const unsaved = new Set<string>();
// Bound calls are handled concurrently, so they are sent one at a time to keep the last value last.
let writes = Promise.resolve(true);
// The latest queued write of each key, which carries the value the cache already holds.
const pending = new Map<string, Promise<boolean>>();

function persist(key: string, value: string | null) {
    if (!loaded) {
        unsaved.add(key);
        return Promise.resolve(false);
    }
    const write = writes
        .then(() => Setting.SetRendererState(key, value))
        .then(
            () => {
                unsaved.delete(key);
                return true;
            },
            (error: unknown) => {
                unsaved.add(key);
                Logger.capture("wails/renderer-state.ts", "Failed to save renderer state", {
                    key,
                    error,
                });
                return false;
            },
        );
    writes = write;

    pending.set(key, write);
    void write.then(() => {
        if (pending.get(key) === write) pending.delete(key);
    });
    return write;
}

/**
 * Synchronous view of the renderer-owned rows in app_state. Reads are served from memory, so
 * hydrateRendererState must finish before anything reads a stored value.
 */
export const rendererState = {
    getItem: (key: string) => cache.get(key) ?? null,
    /** Resolves to whether the value reached the database; the in-memory value changes either way. */
    setItem(key: string, value: string) {
        if (cache.get(key) === value && !unsaved.has(key))
            return pending.get(key) ?? Promise.resolve(true);
        cache.set(key, value);
        return persist(key, value);
    },
    removeItem(key: string) {
        if (!cache.delete(key) && !unsaved.has(key))
            return pending.get(key) ?? Promise.resolve(true);
        return persist(key, null);
    },
};

export function isRendererStateLoaded() {
    return loaded;
}

export async function hydrateRendererState() {
    for (const [key, value] of Object.entries((await Setting.GetRendererState()) ?? {})) {
        if (value !== undefined) cache.set(key, value);
    }
    loaded = true;

    for (const key of LEGACY_LOCAL_STORAGE_KEYS) {
        const report = (error: unknown) =>
            Logger.capture("wails/renderer-state.ts", "Failed to migrate legacy renderer state", {
                key,
                error,
            });

        // localStorage can throw, and one unreadable key must not strand the others.
        try {
            const legacy = localStorage.getItem(key);
            if (legacy === null) continue;
            if (cache.has(key)) {
                localStorage.removeItem(key);
                continue;
            }
            void rendererState
                .setItem(key, legacy)
                .then((saved) => {
                    if (saved) localStorage.removeItem(key);
                })
                .catch(report);
        } catch (error) {
            report(error);
        }
    }
}
