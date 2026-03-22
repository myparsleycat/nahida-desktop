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

    private getParentWindow() {
        const mainWindow = this.desktop.window.main.window;

        if (!mainWindow || mainWindow.isDestroyed()) {
            return null;
        }

        return mainWindow;
    }

    public async focus() {
        const window = await this.createSettingWindow();
        if (window) {
            focus(window);
        }
    }

    public async recreateWithMainParentIfNeeded() {
        if (!this.window || this.window.isDestroyed()) {
            this.window = null;
            return;
        }

        const parentWindow = this.getParentWindow();
        if (!parentWindow || this.window.getParentWindow() === parentWindow) {
            return;
        }

        const existingWindow = this.window;
        this.window = null;
        existingWindow.close();
        await this.createSettingWindow();
    }

    async createSettingWindow() {
        if (this.window?.isDestroyed()) {
            this.window = null;
        }

        const parentWindow = this.getParentWindow();

        if (this.window) {
            if (this.window.getParentWindow() !== parentWindow) {
                const existingWindow = this.window;
                this.window = null;
                existingWindow.close();
            } else {
                focus(this.window);
                return this.window;
            }
        }

        if (this.window) {
            focus(this.window);
            return this.window;
        }

        const titlebarSetting = await this.desktop.setting.general.getTitlebarStyle();
        const isNativeTitlebar = titlebarSetting === "native";

        this.window = new BrowserWindow({
            title: "설정",
            width: 580,
            height: 740,
            resizable: false,
            show: false,
            frame: isNativeTitlebar,
            maximizable: false,
            autoHideMenuBar: true,
            ...(process.platform === "linux" ? { icon } : {}),
            webPreferences: {
                ...getDefaultWebPreferences(),
            },
            icon,
            ...(parentWindow ? { parent: parentWindow } : {}),
        });
        const window = this.window;

        window.webContents.setWindowOpenHandler(({ url }) => {
            if (url.startsWith("http")) {
                openExternal(url);
                return { action: "deny" };
            }
            return { action: "allow" };
        });

        window.on("ready-to-show", () => {
            window.show();
        });

        window.on("closed", () => {
            if (this.window === window) {
                this.window = null;
            }
        });

        if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
            window.loadURL(`${process.env["ELECTRON_RENDERER_URL"]}/#/setting`);
        } else {
            // cjs
            // desktop.window.setting.loadFile(path.join(__dirname, "../renderer/index.html"), {
            //     hash: "setting",
            // });

            // esm
            window.loadFile(
                fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
                {
                    hash: "setting",
                },
            );
        }

        return window;
    }
}

export default SettingWindow;
