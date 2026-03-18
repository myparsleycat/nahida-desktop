const { autoUpdater } = require("electron-updater");

import { app, BrowserWindow } from "electron";
import ms from "ms";
import type { NahidaDesktop } from "..";
import isDev from "./isDev";

autoUpdater.allowDowngrade = false;
autoUpdater.autoDownload = true;
autoUpdater.autoInstallOnAppQuit = true;
autoUpdater.disableDifferentialDownload = true;
autoUpdater.autoRunAppAfterInstall = true;
autoUpdater.allowPrerelease = false;
autoUpdater.disableWebInstaller = true;
if (isDev) {
    autoUpdater.forceDevUpdateConfig = true;
}

export class Updater {
    private readonly desktop: NahidaDesktop;
    public updateDownloaded: boolean = false;
    public updateAvailable: boolean = false;
    private updateDialogDismissed: boolean = false;
    private interval: ReturnType<typeof setInterval> | undefined = undefined;
    private isCheckingForUpdates: boolean = false;
    private hasRunInitialAutoCheck: boolean = false;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public initialize(): void {
        autoUpdater.on("error", (err) => {
            this.isCheckingForUpdates = false;
            this.updateDownloaded = false;
            this.updateAvailable = false;
            this.updateDialogDismissed = false;

            this.desktop.logger.log("error", err, "updater");
        });

        autoUpdater.on("update-available", () => {
            this.isCheckingForUpdates = false;
            this.updateAvailable = true;
        });

        autoUpdater.on("update-not-available", () => {
            this.isCheckingForUpdates = false;
            this.updateDownloaded = false;
            this.updateAvailable = false;
            this.updateDialogDismissed = false;
        });

        autoUpdater.on("update-downloaded", async () => {
            this.isCheckingForUpdates = false;
            this.updateDownloaded = true;
            this.updateDialogDismissed = false;
            await this.notifyUpdateReady();
        });

        autoUpdater.on("update-cancelled", () => {});

        clearInterval(this.interval);

        this.interval = setInterval(async () => {
            this.runAutomaticCheck().catch((err) => {
                this.desktop.logger.log("error", err, "updater.interval");
                this.desktop.logger.log("error", err);
            });
        }, ms("1h"));

        void this.runInitialAutomaticCheck();
    }

    public async checkForUpdates(userInitiated: boolean = false): Promise<void> {
        if (this.isCheckingForUpdates) {
            return;
        }

        if (this.updateDownloaded) {
            if (userInitiated) {
                this.updateDialogDismissed = false;
            }
            await this.notifyUpdateReady();
            return;
        }

        if (this.updateAvailable) {
            return;
        }
        this.isCheckingForUpdates = true;

        await autoUpdater.checkForUpdates();
    }

    private async runInitialAutomaticCheck(): Promise<void> {
        if (this.hasRunInitialAutoCheck) {
            return;
        }

        this.hasRunInitialAutoCheck = true;

        try {
            const autoUpdate = await this.desktop.setting.general.getAutoUpdate();
            if (!autoUpdate) {
                return;
            }

            await this.checkForUpdates();
        } catch (err) {
            this.desktop.logger.log("error", err, "updater.initialCheck");
            this.desktop.logger.log("error", err);
        }
    }

    private async runAutomaticCheck(): Promise<void> {
        const autoUpdate = await this.desktop.setting.general.getAutoUpdate();
        if (!autoUpdate) {
            return;
        }

        await this.checkForUpdates();
    }

    public getStatus(): {
        updateAvailable: boolean;
        updateDownloaded: boolean;
        shouldPromptForUpdate: boolean;
    } {
        return {
            updateAvailable: this.updateAvailable,
            updateDownloaded: this.updateDownloaded,
            shouldPromptForUpdate: this.updateDownloaded && !this.updateDialogDismissed,
        };
    }

    public async showPendingDialogsIfNeeded(): Promise<void> {
        const mainWindow = this.desktop.window.main.window;
        if (this.updateDownloaded && !this.updateDialogDismissed && mainWindow) {
            this.desktop.ipc.postMessageToWindow(mainWindow, "updater:update-downloaded");
        }
    }

    public dismissUpdateDialog(): void {
        if (!this.updateDownloaded) {
            return;
        }

        this.updateDialogDismissed = true;
    }

    private async notifyUpdateReady(): Promise<void> {
        const mainWindow = await this.focusMainWindow();
        if (!mainWindow) {
            return;
        }

        this.desktop.ipc.postMessageToWindow(mainWindow, "updater:update-downloaded");
    }

    private async focusMainWindow(): Promise<BrowserWindow | null> {
        let mainWindow = this.desktop.window.main.window;

        if (!mainWindow || mainWindow.isDestroyed()) {
            mainWindow = await this.desktop.window.main.createMainWindow();
        }

        if (!mainWindow || mainWindow.isDestroyed()) {
            return null;
        }

        if (mainWindow.isMinimized()) {
            mainWindow.restore();
        }

        mainWindow.show();
        mainWindow.focus();

        return mainWindow;
    }
    public async installUpdate(): Promise<void> {
        if (!this.updateDownloaded || !this.updateAvailable) {
            throw new Error("No update available to install.");
        }

        this.desktop.shouldExitOnQuit = true;

        app.removeAllListeners("window-all-closed");
        app.removeAllListeners("will-quit");

        this.desktop.window.main.window?.removeAllListeners("close");
        this.desktop.window.main.window?.removeAllListeners("show");
        this.desktop.window.main.window?.removeAllListeners("minimize");
        this.desktop.window.main.window?.removeAllListeners("maximize");

        try {
            for (const window of BrowserWindow.getAllWindows()) {
                window.destroy();
            }
        } catch (e) {
            this.desktop.logger.log("error", e, "updater.installUpdate.destroyWindows");
            this.desktop.logger.log("error", e);
        }

        if (process.platform === "darwin") {
            autoUpdater.quitAndInstall(true, true);

            setTimeout(() => {
                app.exit(0);
            }, 1500);
        } else {
            autoUpdater.quitAndInstall(false, true);

            if (process.platform === "win32") {
                setTimeout(() => {
                    app.exit(0);
                }, 1000);
            }
        }
    }
}

export default Updater;
