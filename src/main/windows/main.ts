import { fileURLToPath } from "node:url";

import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { openExternal } from "@main/services/util";
import { BrowserWindow, screen } from "electron";
import { debounce } from "es-toolkit";

import icon from "../../../resources/nahida.png?asset";
import { focus, getDefaultWebPreferences } from "./utils";

export class MainWindow {
    private readonly desktop: NahidaDesktop;
    public window: BrowserWindow | null;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.window = null;
    }

    public focus() {
        if (!this.window || this.window.isDestroyed()) {
            this.window = null;
            void this.createMainWindow();
        } else {
            focus(this.window);
        }
    }

    public async focusAndNavigate(path: string) {
        let window = this.window;
        if (!window || window.isDestroyed()) {
            window = await this.createMainWindow(path);
            if (window && !window.isDestroyed()) focus(window);
            return window;
        }

        if (!window || window.isDestroyed()) {
            return null;
        }

        focus(window);
        if (window.webContents.isLoadingMainFrame()) {
            window.webContents.once("did-finish-load", () => {
                if (!window.isDestroyed()) {
                    this.desktop.ipc.postMessageToWindow(window, "fn:navi", path);
                }
            });
            return window;
        }

        this.desktop.ipc.postMessageToWindow(window, "fn:navi", path);
        return window;
    }

    async createMainWindow(initialRoute?: string) {
        if (this.window?.isDestroyed()) {
            this.window = null;
        }

        if (this.window) {
            focus(this.window);
            return this.window;
        }

        const savedBounds = await this.desktop.setting.getBounds();
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
            title: "Nahida Desktop",
            x: bounds?.x || undefined,
            y: bounds?.y || undefined,
            width: bounds?.width || 1200,
            height: bounds?.height || 800,
            minWidth: 800,
            minHeight: 600,
            show: false,
            frame: isNativeTitlebar,
            autoHideMenuBar: true,
            webPreferences: {
                ...getDefaultWebPreferences(),
            },
            icon,
        });

        let hasShownWindow = false;
        const showWindow = async () => {
            if (!this.window || this.window.isDestroyed() || hasShownWindow) {
                return;
            }

            hasShownWindow = true;
            this.window.show();
            void this.desktop.updater.showPendingDialogsIfNeeded();
        };

        this.window.once("ready-to-show", () => {
            void showWindow();
        });

        this.window.webContents.once("did-finish-load", () => {
            void showWindow();
        });

        const saveBounds = debounce(async () => {
            if (!this.window) return;
            if (
                this.window.isMaximized() ||
                this.window.isMinimized() ||
                this.window.isFullScreen()
            )
                return;
            const bounds = this.window.getBounds();
            await this.desktop.setting.setBounds(bounds);
        }, 1000);

        this.window.on("resize", saveBounds);
        this.window.on("move", saveBounds);

        this.window.on("close", async () => {
            saveBounds.cancel();
            if (!this.window) return;
            if (this.window.isDestroyed()) return;
            if (
                this.window.isMaximized() ||
                this.window.isMinimized() ||
                this.window.isFullScreen()
            )
                return;
            const bounds = this.window.getBounds();
            await this.desktop.setting.setBounds(bounds);
        });

        this.window.on("closed", () => {
            saveBounds.cancel();
            this.window = null;
        });

        this.window.webContents.setWindowOpenHandler((details) => {
            void openExternal(details.url);
            return { action: "deny" };
        });

        if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
            const baseUrl = process.env["ELECTRON_RENDERER_URL"];
            const routeUrl = initialRoute ? `${baseUrl}/#${initialRoute}` : baseUrl;
            void this.window.loadURL(routeUrl);
        } else {
            // cjs
            // this.window.loadFile(path.join(__dirname, "../renderer/index.html"));

            // esm
            void this.window.loadFile(
                fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
                {
                    hash: initialRoute ? initialRoute.slice(1) : undefined,
                },
            );
        }

        this.window.on("blur", () => {
            if (!this.window) return;
            this.desktop.ipc.postMessageToWindow(this.window, "window:blur");
        });

        this.window.on("focus", () => {
            if (!this.window) return;
            this.desktop.ipc.postMessageToWindow(this.window, "window:focus");
        });

        // this.window.webContents.openDevTools();
        return this.window;
    }
}

export default MainWindow;
