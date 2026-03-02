import os from "node:os";
import path from "node:path";
import { electronApp, optimizer } from "@electron-toolkit/utils";
import { BACKEND_URL } from "@shared/const";
import AutoLaunch from "auto-launch";
import Database from "better-sqlite3";
import { drizzle } from "drizzle-orm/better-sqlite3";
import { app, crashReporter, protocol, session } from "electron";
import { installExtension, REACT_DEVELOPER_TOOLS } from "electron-devtools-installer";
import ky from "ky";
import { Agent, Pool, ProxyAgent } from "undici";
import { IS_ELECTRON, appVersion } from "./const";
import { startInit } from "./init";
import { DB_FILE_NAME } from "./internal/const";
import * as schema from "./internal/db/schema";
import Logger from "./internal/logger";
import Updater from "./internal/updater";
import { IPC } from "./ipc";
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
import { ModTools } from "./services/mod-tools";
import TransferService from "./services/transfer";
import { XXMI } from "./services/xxmi";
import Setting from "./setting";
import LoginWindow from "./windows/login";
import MainWindow from "./windows/main";
import ReportWindow from "./windows/report";
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
    private cachedAgent: Agent | ProxyAgent | null = null;
    private cachedProxyHash: string = "";

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
        report: ReportWindow;
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
    };

    public service: {
        auth: Auth;
        drive: DriveService;
        transfer: TransferService;
        mod: ModManager;
        modTools: ModTools;
        archive: ArchiveService;
        xxmi: XXMI;
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
            report: new ReportWindow(this),
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
        };

        this.service = {
            auth: new Auth(this),
            drive: new DriveService(this),
            transfer: new TransferService(this),
            mod: new ModManager(this),
            modTools: new ModTools(this),
            archive: new ArchiveService(this),
            xxmi: new XXMI(this),
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
        await this.service.xxmi.init();

        const logLevel = await this.setting.general.getLogLevel();
        this.logger.setLevel(logLevel);

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

    public async getAgent() {
        const proxy = await this.setting.net.getProxy();
        const proxyHash = JSON.stringify(proxy);

        if (this.cachedAgent && this.cachedProxyHash === proxyHash) {
            return this.cachedAgent;
        }

        if (proxy && proxy.type !== "disabled" && proxy.host && proxy.port) {
            let url: string;
            let token: string | undefined;

            if (proxy.type === "socks5") {
                const auth =
                    proxy.requiresAuth && proxy.username && proxy.password
                        ? `${encodeURIComponent(proxy.username)}:${encodeURIComponent(proxy.password)}@`
                        : "";
                url = `socks5://${auth}${proxy.host}:${proxy.port}`;
            } else {
                url = `http://${proxy.host}:${proxy.port}`;
                if (proxy.requiresAuth && proxy.username && proxy.password) {
                    token = `Basic ${Buffer.from(`${proxy.username}:${proxy.password}`).toString("base64")}`;
                }
            }

            this.cachedAgent = new ProxyAgent({
                uri: url,
                token,
                factory(origin, options) {
                    return new Pool(origin, {
                        ...options,
                        allowH2: true,
                    });
                },
            });
        } else {
            this.cachedAgent = new Agent({
                factory(origin, options) {
                    return new Pool(origin, {
                        ...options,
                        allowH2: true,
                    });
                },
            });
        }

        this.cachedProxyHash = proxyHash;
        return this.cachedAgent;
    }

    public async getHeaders(url: string) {
        const token = await this.service.auth.getToken();
        const prefixs = ["http://localhost", "https://api.nahida.live"];
        const isNHD = prefixs.some((prefix) => url.startsWith(prefix));
        return {
            ...(token && isNHD && { Authorization: `Bearer ${token}` }),
            Origin: "https://nahida.live",
            "User-Agent": `Nahida Desktop/${appVersion}`,
        };
    }

    public async fetcher(url: string, options?: RequestInit) {
        const prefixs = ["http://localhost", "https://api.nahida.live"];
        const isNHD = prefixs.some((prefix) => url.startsWith(prefix));

        const resp = await ky(url, {
            ...options,
            headers: {
                ...options?.headers,
                ...(await this.getHeaders(url)),
            },
            timeout: 100000,
            retry: {
                limit: 2,
            },
            // @ts-expect-error - dispatcher is not in the type definition, but it's passed through to fetch.
            dispatcher: await this.getAgent(),
            hooks: {
                afterResponse: [
                    async (_req, _opt, resp) => {
                        if (resp.status === 401 && isNHD) {
                            await this.service.auth.getSession();
                        }
                    },

                    (_request, _options, response) => {
                        if (response.status === 524) {
                            return new Response("cloudflare timeout. but it's ok", { status: 200 });
                        } else {
                            return response;
                        }
                    },
                ],

                beforeError: [
                    async (error) => {
                        const { response } = error;
                        if (response && response.status === 524) {
                            return error;
                        }

                        return error;
                    },
                ],
            },
        });

        return resp;
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

    // const loggedIn = await desktop.service.auth.isLoggedIn();  // if (loggedIn) {
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
    if (desktop.shouldExitOnQuit) {
        app.quit();
        return;
    }

    const runInBackground = await desktop.setting.general.getRunInBackground();
    if (!runInBackground) {
        app.quit();
    }
});
