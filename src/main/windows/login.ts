import { fileURLToPath } from "node:url";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { BrowserWindow } from "electron";
import icon from "../../../resources/nahida.png?asset";
import { getDefaultWebPreferences } from "./utils";

export class LoginWindow {
    private readonly desktop: NahidaDesktop;
    public window: BrowserWindow | null;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.window = null;
    }

    async createLoginWindow() {
        if (this.window && !this.window.isDestroyed()) {
            this.window.show();
            this.window.focus();
            return;
        }

        const titlebarSetting = await this.desktop.setting.general.getTitlebarStyle();
        const isNativeTitlebar = titlebarSetting === "native";

        this.window = new BrowserWindow({
            title: "로그인",
            width: 350,
            height: 350,
            resizable: false,
            show: false,
            frame: isNativeTitlebar,
            maximizable: false,
            autoHideMenuBar: true,
            webPreferences: {
                ...getDefaultWebPreferences(),
            },
            icon,
        });

        this.window.on("ready-to-show", () => {
            this.window?.show();
        });

        this.window.on("closed", () => {
            this.window = null;
        });

        if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
            this.window.loadURL(`${process.env["ELECTRON_RENDERER_URL"]}/#/auth`);
        } else {
            // cjs
            // desktop.window.auth.loadFile(path.join(__dirname, "../renderer/index.html"), {
            //     hash: "auth",
            // });

            // esm
            this.window.loadFile(
                fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
                {
                    hash: "auth",
                },
            );
        }
    }
}

export default LoginWindow;
