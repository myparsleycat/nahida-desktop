import path from "node:path";
import isDev from "@main/internal/isDev";
import type { BrowserWindow } from "electron";

export function getDefaultWebPreferences() {
    return {
        backgroundThrottling: false,
        autoplayPolicy: "no-user-gesture-required" as const,
        contextIsolation: true,
        nodeIntegration: false,
        experimentalFeatures: false,
        devTools: isDev,
        zoomFactor: 1,
        sandbox: true as const,
        // cjs
        preload: path.join(__dirname, "../preload/index.js"),

        // esm
        // preload: fileURLToPath(new URL("../preload/index.mjs", import.meta.url)),
    } as Electron.WebPreferences;
}

export function focus(window: BrowserWindow) {
    if (window.isDestroyed()) return;

    if (window.isMinimized()) window.restore();

    window.setAlwaysOnTop(true);
    window.show();
    window.focus();
    window.setAlwaysOnTop(false);

    window.moveTop();
}
