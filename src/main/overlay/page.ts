import { is } from "@electron-toolkit/utils";
import { type WebContents } from "electron";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Load ingame configuration page
 */
export async function loadMainPage(webContents: WebContents) {
    if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
        webContents.loadURL(`${process.env["ELECTRON_RENDERER_URL"]}/#/overlay`);
    } else {
        webContents.loadFile(fileURLToPath(new URL("../renderer/index.html", import.meta.url)), {
            hash: "overlay",
        });
    }
}

/**
 * Path to preload script
 */
export const preloadPath = path.join(__dirname, "../preload/index.js");
