import { fileURLToPath } from "node:url";
import { is } from "@electron-toolkit/utils";
import { BrowserWindow } from "electron";
import icon from "../../../resources/nahida.png?asset";
import type { NahidaDesktop } from "..";
import { focus, getDefaultWebPreferences } from "./utils";

export class ReportWindow {
    private readonly desktop: NahidaDesktop;
    public window: BrowserWindow | null;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
        this.window = null;
    }

    public async focus() {
        if (!this.window) {
            await this.createReportWindow();
        } else {
            focus(this.window);
        }
    }

    async createReportWindow() {
        if (this.window && !this.window.isDestroyed()) {
            this.window.show();
            this.window.focus();
            return;
        }

        if (!this.desktop.window.main.window) {
            return;
        }

        this.window = new BrowserWindow({
            title: "Report",
            width: 540,
            height: 670,
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
            parent: this.desktop.window.main.window,
            modal: true,
        });

        this.window.on("ready-to-show", () => {
            this.window?.show();
        });

        this.window.on("close", () => {
            this.window = null;
        });

        if (is.dev && process.env.ELECTRON_RENDERER_URL) {
            this.window.loadURL(`${process.env.ELECTRON_RENDERER_URL}/#/report`);
        } else {
            this.window.loadFile(
                fileURLToPath(new URL("../renderer/index.html", import.meta.url)),
                {
                    hash: "report",
                },
            );
        }

        return this.window;
    }
}

export default ReportWindow;
