import { fileURLToPath } from "node:url";
import { is } from "@electron-toolkit/utils";
import type { NahidaDesktop } from "@main/index";
import type { WindowRect } from "@main/services/overlay";
import { BrowserWindow } from "electron";
import icon from "../../../resources/nahida.png?asset";
import { getDefaultWebPreferences } from "./utils";

export class OverlayWindow {
    private readonly desktop: NahidaDesktop;
    public window: BrowserWindow | null;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.window = null;
    }

    public async createOverlayWindow(titleOrOptions: string | { title: string; pid: number }) {
        const title = typeof titleOrOptions === "string" ? titleOrOptions : titleOrOptions.title;
        const pid = typeof titleOrOptions === "string" ? null : titleOrOptions.pid;

        if (this.window) {
            this.desktop.logger.info(`Attaching existing overlay to ${title}`, "OverlayWindow");
            if (pid) {
                this.desktop.service.overlay.startByPid(pid);
            } else {
                this.desktop.service.overlay.start(title);
            }
            return this.window;
        }

        this.window = new BrowserWindow({
            title: "Nahida Overlay",
            icon,
            webPreferences: {
                ...getDefaultWebPreferences(),
            },
            transparent: true,
            frame: false,
            show: false,
            alwaysOnTop: true,
            skipTaskbar: true,
        });

        this.window.hide();
        this.window.setIgnoreMouseEvents(true, { forward: true });

        if (is.dev && process.env.ELECTRON_RENDERER_URL) {
            this.window.loadURL(`${process.env.ELECTRON_RENDERER_URL}/#/overlay`);
        } else {
            this.window.loadFile(
                fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
                { hash: "overlay" },
            );
        }

        const getCenteredBounds = (rect: WindowRect) => {
            const width = Math.round(rect.width / 1.5);
            const height = Math.round(rect.height / 1.5);
            // const height = 200;
            const x = Math.round(rect.x + (rect.width - width) / 2);
            const y = Math.round(rect.y + (rect.height - height) / 2);
            // const y = rect.y + 16;
            return { x, y, width, height };
        };

        const setupOverlayEvents = () => {
            const overlay = this.desktop.service.overlay;

            overlay.on("attach", (rect: WindowRect) => {
                if (this.window && rect) {
                    this.window.setBounds(getCenteredBounds(rect));
                }
            });

            overlay.on("detach", () => {
                if (this.window) {
                    this.window.hide();
                }
            });

            overlay.on("move", (rect: WindowRect) => {
                if (this.window && rect) {
                    this.window.setBounds(getCenteredBounds(rect));
                }
            });

            overlay.on("resize", (rect: WindowRect) => {
                if (this.window && rect) {
                    this.window.setBounds(getCenteredBounds(rect));
                }
            });

            overlay.on("focus", () => {
                if (this.window) {
                    // Logic: If the game gains focus while the overlay is visible,
                    // assume the user clicked the game to play, so we auto-close the overlay.
                    if (this.desktop.service.overlay.isVisible) {
                        this.desktop.service.overlay.toggle();
                    }
                    this.window.webContents.send("overlay:focus");
                }
            });

            overlay.on("blur", () => {
                if (this.window) {
                    if (this.window.isFocused()) return;

                    this.window.setIgnoreMouseEvents(true, { forward: true });
                    this.window.hide();
                    this.window.webContents.send("overlay:blur");
                }
            });

            overlay.on("toggle-visible", (isVisible: boolean) => {
                if (this.window) {
                    if (isVisible) {
                        this.window.showInactive();
                        this.window.setAlwaysOnTop(true, "screen-saver");
                        this.window.setIgnoreMouseEvents(false);
                    } else {
                        this.window.setIgnoreMouseEvents(true, { forward: true });
                        this.window.hide();
                    }
                }
            });

            overlay.on("input", (key: string) => {
                if (this.window) {
                    this.window.webContents.send("overlay:input", key);
                }
            });
        };

        setupOverlayEvents();

        if (pid) {
            this.desktop.service.overlay.startByPid(pid);
        } else {
            this.desktop.service.overlay.start(title);
        }

        this.window.on("closed", () => {
            this.window = null;
            this.desktop.service.overlay.stop();
        });

        this.window.on("blur", () => {
            if (this.window) {
                this.desktop.service.overlay.setOverlayFocused(false);
                this.window.setIgnoreMouseEvents(true, { forward: true });
                this.window.hide();
            }
        });

        this.window.on("focus", () => {
            if (this.window) {
                this.desktop.service.overlay.setOverlayFocused(true);
            }
        });

        return this.window;
    }

    public setIgnoreMouseEvents(ignore: boolean) {
        if (this.window) {
            this.window.setIgnoreMouseEvents(ignore, { forward: true });
        }
    }
}

export default OverlayWindow;
