import { BrowserWindow, shell } from "electron";
import path from "node:path";
import icon from "../../../resources/nahida.png?asset";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { getDefaultWebPreferences } from "./utils";
import { fileURLToPath } from "node:url";
import { debounce } from "es-toolkit";

export async function createMainWindow(desktop: NahidaDesktop) {
    const bounds = await desktop.setting.getBounds();
    desktop.window.main = new BrowserWindow({
        title: "Nahida Desktop",
        x: bounds?.x || undefined,
        y: bounds?.y || undefined,
        width: bounds?.width || 1200,
        height: bounds?.height || 800,
        minWidth: 800,
        minHeight: 600,
        show: false,
        frame: false,
        autoHideMenuBar: true,
        ...(process.platform === "linux" ? { icon } : {}),
        webPreferences: {
            ...getDefaultWebPreferences(),
        },
        icon,
    });

    desktop.window.main.on("ready-to-show", async () => {
        desktop.window.main?.show();
    });

    const saveBounds = debounce(async () => {
        if (!desktop.window.main) return;
        const bounds = desktop.window.main.getBounds();
        await desktop.setting.setBounds(bounds);
    }, 1000);

    desktop.window.main.on("resize", saveBounds);
    desktop.window.main.on("move", saveBounds);

    desktop.window.main.on("close", async () => {
        saveBounds.cancel();
        const bounds = desktop.window.main?.getBounds();
        if (bounds) await desktop.setting.setBounds(bounds);
        desktop.window.main = null;
    });

    desktop.window.main.webContents.setWindowOpenHandler((details) => {
        shell.openExternal(details.url);
        return { action: "deny" };
    });

    if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
        desktop.window.main.loadURL(process.env["ELECTRON_RENDERER_URL"]);
    } else {
        // cjs
        // desktop.window.main.loadFile(path.join(__dirname, "../renderer/index.html"));

        // esm
        desktop.window.main.loadFile(
            fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
        );
    }

    desktop.window.main.on("blur", () => {
        if (!desktop.window.main) return;
        desktop.ipc.postMessageToWindow(desktop.window.main, "window:blur");
    });

    desktop.window.main.on("focus", () => {
        if (!desktop.window.main) return;
        desktop.ipc.postMessageToWindow(desktop.window.main, "window:focus");
    });

    // desktop.window.main.webContents.openDevTools();

    return desktop.window.main;
}
