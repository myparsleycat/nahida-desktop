import { Window as AppWindow } from "@bindings/app";
import { Logger } from "@renderer/lib/logger";
import { useEffect } from "react";

export function useNativeWindowTheme() {
    useEffect(() => {
        const root = document.documentElement;
        let applied: boolean | null = null;
        // Bound calls are handled concurrently, so they are sent one at a time to keep the last theme last.
        let queue = Promise.resolve();
        const sync = () => {
            // ThemeProvider has not applied a theme yet; the class change will trigger a sync.
            if (!root.classList.contains("dark") && !root.classList.contains("light")) return;
            const dark = root.classList.contains("dark");
            if (dark === applied) return;
            applied = dark;
            queue = queue
                .then(() => AppWindow.SyncTheme(dark))
                .catch((error: unknown) =>
                    Logger.capture(
                        "hooks/use-native-window-theme.ts",
                        "Failed to sync the native window theme",
                        error,
                    ),
                );
        };

        sync();
        const observer = new MutationObserver(sync);
        observer.observe(root, { attributes: true, attributeFilter: ["class"] });
        return () => observer.disconnect();
    }, []);
}
