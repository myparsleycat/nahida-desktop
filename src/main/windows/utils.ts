import isDev from "@main/internal/isDev";
import { BrowserWindow } from "electron";
import { fileURLToPath } from "node:url";

export function getDefaultWebPreferences() {
    return {
        backgroundThrottling: false,
        autoplayPolicy: "no-user-gesture-required" as const,
        contextIsolation: true,
        experimentalFeatures: true,
        devTools: isDev,
        zoomFactor: 1,
        sandbox: false,
        // cjs
        // preload: path.join(__dirname, "../preload/index.js"),

        // esm
        preload: fileURLToPath(new URL("../preload/index.mjs", import.meta.url)),
    };
}

export function focus(window: BrowserWindow) {
    if (window.isMinimized()) window.restore();

    window.setAlwaysOnTop(true);
    window.show();
    window.focus();
    window.setAlwaysOnTop(false);

    window.moveTop();
}
