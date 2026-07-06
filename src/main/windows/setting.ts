import { fileURLToPath } from "node:url";

import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { openExternal } from "@main/services/util";
import { BrowserWindow, screen } from "electron";
import { debounce } from "es-toolkit";

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

        const savedBounds = await this.desktop.setting.getSettingBounds();
        let bounds = savedBounds;

        if (bounds) {
            const displays = screen.getAllDisplays();
            const isValid = displays.some((display) => {
                const area = display.workArea;
                return (
                    bounds!.x >= area.x &&
                    bounds!.y >= area.y &&
                    bounds!.x < area.x + area.width &&
                    bounds!.y < area.y + area.height
                );
            });

            if (!isValid) {
                bounds = null;
            }
        }

        const titlebarSetting = await this.desktop.setting.general.getTitlebarStyle();
        const isNativeTitlebar = titlebarSetting === "native";

        this.window = new BrowserWindow({
            title: "설정",
            width: bounds?.width || 580,
            height: bounds?.height || 740,
            minWidth: 580,
            minHeight: 740,
            maxWidth: 1080,
            maxHeight: 2180,
            show: false,
            frame: isNativeTitlebar,
            maximizable: false,
            autoHideMenuBar: true,
            webPreferences: {
                ...getDefaultWebPreferences(),
            },
            icon,
            ...(parentWindow ? { parent: parentWindow } : {}),
        });
        const window = this.window;
        const saveBounds = debounce(async () => {
            if (!this.window || this.window !== window) return;
            if (window.isMaximized() || window.isMinimized() || window.isFullScreen()) return;
            await this.desktop.setting.setSettingBounds(window.getBounds());
        }, 1000);

        window.webContents.setWindowOpenHandler(({ url }) => {
            if (url.startsWith("http")) {
                void openExternal(url);
                return { action: "deny" };
            }
            return { action: "allow" };
        });

        window.on("ready-to-show", () => {
            window.show();
        });

        window.on("resize", saveBounds);
        window.on("move", saveBounds);

        window.on("close", async () => {
            saveBounds.cancel();
            if (window.isDestroyed()) return;
            if (window.isMaximized() || window.isMinimized() || window.isFullScreen()) return;
            await this.desktop.setting.setSettingBounds(window.getBounds());
        });

        window.on("closed", () => {
            saveBounds.cancel();
            if (this.window === window) {
                this.window = null;
            }
        });

        if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
            void window.loadURL(`${process.env["ELECTRON_RENDERER_URL"]}/#/setting`);
        } else {
            // cjs
            // desktop.window.setting.loadFile(path.join(__dirname, "../renderer/index.html"), {
            //     hash: "setting",
            // });

            // esm
            void window.loadFile(
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
