import { BrowserWindow, shell } from "electron";
import path from "node:path";
import icon from "../../../resources/nahida.png?asset";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { getDefaultWebPreferences } from "./utils";
import { fileURLToPath } from "node:url";

export function createLoginWindow(desktop: NahidaDesktop) {
    if (desktop.window.auth && !desktop.window.auth.isDestroyed()) {
        desktop.window.auth.show();
        desktop.window.auth.focus();
        return;
    }

    desktop.window.auth = new BrowserWindow({
        title: "로그인",
        width: 350,
        height: 350,
        resizable: false,
        show: false,
        frame: false,
        maximizable: false,
        autoHideMenuBar: true,
        ...(process.platform === "linux" ? { icon } : {}),
        webPreferences: {
            ...getDefaultWebPreferences(),
        },
        icon,
    });

    desktop.window.auth.on("ready-to-show", () => {
        desktop.window.auth?.show();
    });

    desktop.window.auth.on("close", () => {
        desktop.window.auth = null;
    });

    if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
        desktop.window.auth.loadURL(`${process.env["ELECTRON_RENDERER_URL"]}/#/auth`);
    } else {
        // cjs
        // desktop.window.auth.loadFile(path.join(__dirname, "../renderer/index.html"), {
        //     hash: "auth",
        // });

        // esm
        desktop.window.auth.loadFile(
            fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
            {
                hash: "auth",
            },
        );
    }
}
