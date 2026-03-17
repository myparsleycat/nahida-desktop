import { supportsWindowsDesktopFeatures } from "@shared/platform";
import type { IpcEvents } from "@shared/types.gen";
import { BrowserWindow } from "electron";
import type { NahidaDesktop } from "../index";
import { registerAuthHandlers } from "./handlers/auth";
import { registerDriveHandlers } from "./handlers/drive";
import { registerFixToolsManagerHandlers } from "./handlers/fix-tools-manager";
import { registerLoggerHandlers } from "./handlers/logger";
import { registerModHandlers } from "./handlers/mod";
import { registerPathSelectorHandlers } from "./handlers/path-selector";
import { registerSettingHandlers } from "./handlers/setting";
import { registerToolsHandlers } from "./handlers/tools";
import { registerTransferHandlers } from "./handlers/transfer";
import { registerUtilHandlers } from "./handlers/util";
import { registerWindowHandlers } from "./handlers/window";
import { registerXXMIHandlers } from "./handlers/xxmi";

export class IPC {
    private d: NahidaDesktop;

    constructor(d: NahidaDesktop) {
        this.d = d;
        this.setupHandlers();
    }

    private setupHandlers() {
        registerAuthHandlers(this.d);
        registerDriveHandlers(this.d);
        registerSettingHandlers(this.d);
        registerUtilHandlers(this.d);
        registerWindowHandlers(this.d);
        registerTransferHandlers(this.d);
        registerLoggerHandlers(this.d);
        registerPathSelectorHandlers();

        if (supportsWindowsDesktopFeatures(process.platform)) {
            registerModHandlers(this.d);
            registerFixToolsManagerHandlers(this.d);
            registerToolsHandlers(this.d);
            registerXXMIHandlers(this.d);
        }
    }

    public postMessageToWindow<K extends keyof IpcEvents>(
        window: BrowserWindow,
        channel: K,
        ...args: Parameters<IpcEvents[K]>
    ) {
        window.webContents.send(channel, ...args);
    }

    public broadcast<K extends keyof IpcEvents>(channel: K, ...args: Parameters<IpcEvents[K]>) {
        BrowserWindow.getAllWindows().forEach((win) => {
            win.webContents.send(channel, ...args);
        });
    }
}
