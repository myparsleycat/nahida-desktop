import { fileURLToPath } from "node:url";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { openExternal } from "@main/services/util";
import { BrowserWindow } from "electron";
import icon from "../../../resources/nahida.png?asset";
import { focus, getDefaultWebPreferences } from "./utils";

export class SettingWindow {
    private readonly desktop: NahidaDesktop;
    public window: BrowserWindow | null;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.window = null;
    }

    public focus() {
        if (!this.window) {
            this.createSettingWindow();
        } else {
            focus(this.window);
        }
    }

    async createSettingWindow() {
        if (this.window) {
            focus(this.window);
            return this.window;
        }

        this.window = new BrowserWindow({
            title: "설정",
            width: 580,
            height: 740,
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

        this.window.webContents.setWindowOpenHandler(({ url }) => {
            if (url.startsWith("http")) {
                openExternal(url);
                return { action: "deny" };
            }
            return { action: "allow" };
        });

        this.window.on("ready-to-show", () => {
            this.window?.show();
        });

        this.window.on("close", () => {
            this.window = null;
        });

        if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
            this.window.loadURL(`${process.env["ELECTRON_RENDERER_URL"]}/#/setting`);
        } else {
            // cjs
            // desktop.window.setting.loadFile(path.join(__dirname, "../renderer/index.html"), {
            //     hash: "setting",
            // });

            // esm
            this.window.loadFile(
                fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
                {
                    hash: "setting",
                },
            );
        }

        return this.window;
    }
}

export default SettingWindow;
