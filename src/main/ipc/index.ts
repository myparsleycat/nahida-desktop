import { BrowserWindow } from "electron";
import type { NahidaDesktop } from "../index";
import { registerAuthHandlers } from "./handlers/auth";
import { registerDriveHandlers } from "./handlers/drive";
import { registerSettingHandlers } from "./handlers/setting";
import { registerUtilHandlers } from "./handlers/util";
import { registerWindowHandlers } from "./handlers/window";
import { registerTransferHandlers } from "./handlers/transfer";
import { registerModHandlers } from "./handlers/mod";
import { registerLoggerHandlers } from "./handlers/logger";
import { registerPathSelectorHandlers } from "./handlers/path-selector";
import { IpcEvents } from "@shared/types";

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
        registerModHandlers(this.d);
        registerLoggerHandlers(this.d);
        registerPathSelectorHandlers();
    }

    public postMessageToWindow<K extends keyof IpcEvents>(
        window: BrowserWindow,
        channel: K,
        ...args: Parameters<IpcEvents[K]>
    ) {
        window.webContents.send(channel, ...args);
    }
}
