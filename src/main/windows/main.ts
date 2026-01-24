import { BrowserWindow, shell, screen } from "electron";
import path from "node:path";
import icon from "../../../resources/nahida.png?asset";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import { getDefaultWebPreferences } from "./utils";
import { fileURLToPath } from "node:url";
import { debounce } from "es-toolkit";
import { focus } from "./utils";

export class MainWindow {
    private readonly desktop: NahidaDesktop;
    public window: BrowserWindow | null;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.window = null;
    }

    public focus() {
        if (!this.window) return;
        focus(this.window);
    }

    async createMainWindow() {
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

        this.window = new BrowserWindow({
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

        this.window.on("ready-to-show", async () => {
            this.window?.show();
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
            if (
                this.window.isMaximized() ||
                this.window.isMinimized() ||
                this.window.isFullScreen()
            ) {
                this.window = null;
                return;
            }
            const bounds = this.window?.getBounds();
            if (bounds) await this.desktop.setting.setBounds(bounds);
            this.window = null;
        });

        this.window.webContents.setWindowOpenHandler((details) => {
            shell.openExternal(details.url);
            return { action: "deny" };
        });

        if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
            this.window.loadURL(process.env["ELECTRON_RENDERER_URL"]);
        } else {
            // cjs
            // this.window.loadFile(path.join(__dirname, "../renderer/index.html"));

            // esm
            this.window.loadFile(fileURLToPath(new URL("../renderer/index.html", import.meta.url)));
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
