import os from "node:os";
import path from "node:path";
import { electronApp, optimizer } from "@electron-toolkit/utils";
import { BACKEND_URL } from "@shared/const";
import AutoLaunch from "auto-launch";
import Database from "better-sqlite3";
import { drizzle } from "drizzle-orm/better-sqlite3";
import { app, crashReporter, protocol, session } from "electron";
import { installExtension, REACT_DEVELOPER_TOOLS } from "electron-devtools-installer";
import { IS_ELECTRON } from "./const";
import { startInit } from "./init";
import { DB_FILE_NAME } from "./internal/const";
import * as schema from "./internal/db/schema";
import Logger from "./internal/logger";
import Updater from "./internal/updater";
import { IPC } from "./ipc";
import { CompactService } from "./lib/compact";
import Compressor from "./lib/compressor";
import CryptoLib from "./lib/crypto";
import CustomDownloader from "./lib/custom-downloader";
import { FS } from "./lib/fs";
import { NativeLib } from "./lib/native";
import { PathSelector } from "./lib/path-selector";
import Tray from "./lib/tray";
import Utils from "./lib/utils";
import Watcher from "./lib/watcher";
import { registerProtocal } from "./protocals";
import ArchiveService from "./services/archive";
import Auth from "./services/auth";
import { DriveService } from "./services/drive";
import ModManager from "./services/mod-manager";
import OverlayService from "./services/overlay";
import { Tools } from "./services/tools";
import TransferService from "./services/transfer";
import { XXMI } from "./services/xxmi";
import Setting from "./setting";
import LoginWindow from "./windows/login";
import MainWindow from "./windows/main";
import OverlayWindow from "./windows/overlay";
import SettingWindow from "./windows/setting";

if (IS_ELECTRON) {
    // Needs to be here, otherwise Chromium's FileSystemAccess API won't work. Waiting for the electron team to fix it.
    // Ref: https://github.com/electron/electron/issues/28422
    app?.commandLine.appendSwitch("enable-experimental-web-platform-features");
    app?.commandLine.appendSwitch("disable-renderer-backgrounding");
    app?.commandLine.appendSwitch("disable-pinch-zoom");
    app?.commandLine.appendSwitch("disable-pinch");
}

const dbPath = !app.isPackaged ? DB_FILE_NAME : path.join(app.getPath("userData"), "data.db");
const sqlite = new Database(dbPath);
const db = drizzle(sqlite, { schema });

export class NahidaDesktop {
    public initialized: boolean = false;
    public userAgent: string;

    public setting: Setting;
    public readonly ipc: IPC;
    public updater: Updater;
    public logger: Logger;
    public minimizeToTray: boolean = false;
    public shouldExitOnQuit: boolean = false;

    public window: {
        main: MainWindow;
        auth: LoginWindow;
        setting: SettingWindow;
        overlay: OverlayWindow;
    };

    public lib: {
        db: typeof db;
        fs: FS;
        utils: Utils;
        tray: Tray;
        crypto: CryptoLib;
        compressor: Compressor;
        customDownloader: CustomDownloader;
        pathSelector: PathSelector;
        watcher: Watcher;
        native: NativeLib;
        compact: CompactService;
    };

    public service: {
        auth: Auth;
        drive: DriveService;
        transfer: TransferService;
        mod: ModManager;
        archive: ArchiveService;
        tools: Tools;
        xxmi: XXMI;
        overlay: OverlayService;
    };

    public constructor() {
        this.userAgent = `Nahida Desktop/${app.getVersion()}`;
        this.setting = new Setting(this);
        this.ipc = new IPC(this);
        this.updater = new Updater(this);
        this.logger = new Logger(false, false);
        this.window = {
            main: new MainWindow(this),
            auth: new LoginWindow(this),
            setting: new SettingWindow(this),
            overlay: new OverlayWindow(this),
        };
        this.lib = {
            db: db,
            fs: new FS(this),
            utils: new Utils(this),
            tray: new Tray(this),
            crypto: new CryptoLib(this),
            compressor: new Compressor(this),
            customDownloader: new CustomDownloader(this),
            pathSelector: new PathSelector(this),
            watcher: new Watcher(this),
            native: new NativeLib(this),
            compact: new CompactService(this),
        };

        this.service = {
            auth: new Auth(this),
            drive: new DriveService(this),
            transfer: new TransferService(this),
            mod: new ModManager(this),
            archive: new ArchiveService(this),
            tools: new Tools(this),
            xxmi: new XXMI(this),
            overlay: new OverlayService(this),
        };
    }

    public async init() {
        crashReporter.start({
            submitURL: `${BACKEND_URL}/desktop/crash-report`,
            globalExtra: {
                cpus: os.cpus().length.toString(),
                ram: os.totalmem().toString(),
                platform: os.platform(),
                release: os.release(),
            },
        });

        await startInit(this);
        this.updater.initialize();

        if (app.isPackaged) {
            const runOnStartup = await this.setting.general.getRunOnStartup();
            const autoLaunch = new AutoLaunch({
                name: "Nahida Desktop",
                path: app.getPath("exe"),
                isHidden: true,
            });

            if (runOnStartup) {
                autoLaunch.enable();
            } else {
                autoLaunch.disable();
            }
        }

        await this.updateProxy();

        // Initialize Compact Service
        await this.lib.compact.initialize();
    }

    public async updateProxy() {
        const proxy = await this.setting.net.getProxy();
        if (proxy && proxy.type !== "disabled" && proxy.host && proxy.port) {
            const protocol = proxy.type === "socks5" ? "socks5" : "http";
            const proxyRules = `${protocol}://${proxy.host}:${proxy.port}`;
            await app.whenReady();
            await session.defaultSession.setProxy({ proxyRules });
            this.logger.info(`Proxy updated: ${proxyRules}`, "NahidaDesktop:updateProxy");
        } else {
            await app.whenReady();
            await session.defaultSession.setProxy({ proxyRules: "" });
            this.logger.info("Proxy disabled", "NahidaDesktop:updateProxy");
        }
    }
}

export const desktop = new NahidaDesktop();

// 딥링크
if (process.defaultApp) {
    if (process.argv.length >= 2) {
        app.setAsDefaultProtocolClient("nahida", process.execPath, [path.resolve(process.argv[1])]);
    }
} else {
    app.setAsDefaultProtocolClient("nahida");
}

protocol.registerSchemesAsPrivileged([
    {
        scheme: "local",
        privileges: {
            standard: true,
            secure: true,
            supportFetchAPI: true,
            bypassCSP: true,
            stream: true,
        },
    },
]);

// This method will be called when Electron has finished
// initialization and is ready to create browser windows.
// Some APIs can only be used after this event occurs.
app.whenReady().then(async () => {
    const gotTheLock = app.requestSingleInstanceLock();

    if (!gotTheLock) {
        desktop.logger.warn("앱이 이미 실행중임");
        app.quit();
        return;
    }

    if (!app.isPackaged) {
        installExtension(REACT_DEVELOPER_TOOLS)
            .then((ext) => console.log(`Added Extension: ${ext.name}`))
            .catch((err) => console.log("An error occurred: ", err));
    }

    app.on("second-instance", (_event, commandLine, _workingDirectory) => {
        const deepLinkUrl = commandLine.find((arg) => arg.startsWith("nahida://"));
        if (deepLinkUrl?.startsWith("nahida://auth")) {
            // AuthService.handleOAuth2Callback(deepLinkUrl);
        }

        if (desktop.window.main.window) {
            if (desktop.window.main.window.isMinimized()) desktop.window.main.window.restore();
            desktop.window.main.window.show();
            desktop.window.main.window.focus();
        }
    });
    // Set app user model id for windows
    electronApp.setAppUserModelId("com.nahida");

    registerProtocal(desktop);

    // Default open or close DevTools by F12 in development
    // and ignore CommandOrControl + R in production.
    // see https://github.com/alex8088/electron-toolkit/tree/master/packages/utils
    app.on("browser-window-created", (_, window) => {
        optimizer.watchWindowShortcuts(window);
    });

    await desktop.init();

    desktop.service.xxmi.startMonitor();

    // const loggedIn = await desktop.service.auth.isLoggedIn();
    // if (loggedIn) {
    desktop.lib.tray.createTray();
    await desktop.window.main.createMainWindow();
    // } else {
    //     await desktop.window.auth.createLoginWindow();
    // }

    // app.on('activate', async () => {
    //     // On macOS it's common to re-create a window in the app when the
    //     // dock icon is clicked and there are no other windows open.
    //     if (BrowserWindow.getAllWindows().length === 0) await createMainWindow()
    // })
});

// Quit when all windows are closed, except on macOS. There, it's common
// for applications and their menu bar to stay active until the user quits
// explicitly with Cmd + Q.
app.on("window-all-closed", async () => {
    const loggedIn = await desktop.service.auth.isLoggedIn();
    if (process.platform !== "darwin" && !loggedIn) {
        app.quit();
    }
});
