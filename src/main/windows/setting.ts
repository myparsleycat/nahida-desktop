import { BrowserWindow, shell } from "electron";
import path from "node:path";
import icon from "../../../resources/nahida.png?asset";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { getDefaultWebPreferences } from "./utils";
import { fileURLToPath } from "node:url";

export function createSettingWindow(desktop: NahidaDesktop) {
    if (desktop.window.setting && !desktop.window.setting.isDestroyed()) {
        desktop.window.setting.show();
        desktop.window.setting.focus();
        return;
    }

    desktop.window.setting = new BrowserWindow({
        title: "설정",
        width: 580,
        minWidth: 580,
        height: 740,
        maxHeight: 740,
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

    desktop.window.setting.webContents.setWindowOpenHandler(({ url }) => {
        if (url.startsWith("http")) {
            shell.openExternal(url);
            return { action: "deny" };
        }
        return { action: "allow" };
    });

    desktop.window.setting.on("ready-to-show", () => {
        desktop.window.setting?.show();
    });

    desktop.window.setting.on("close", () => {
        desktop.window.setting = null;
    });

    if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
        desktop.window.setting.loadURL(`${process.env["ELECTRON_RENDERER_URL"]}/#/setting`);
    } else {
        // cjs
        // desktop.window.setting.loadFile(path.join(__dirname, "../renderer/index.html"), {
        //     hash: "setting",
        // });

        // esm
        desktop.window.setting.loadFile(
            fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
            {
                hash: "setting",
            },
        );
    }
}
