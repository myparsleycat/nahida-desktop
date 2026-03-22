const { autoUpdater } = require("electron-updater");

import type { AutoUpdateMode, UpdaterStatus } from "@shared/updater";
import { app, BrowserWindow } from "electron";
import ms from "ms";
import z from "zod";
import type { NahidaDesktop } from "..";
import isDev from "./isDev";

autoUpdater.allowDowngrade = false;
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
    private releaseVersion: string | null = null;
    private releaseNotesUrl: string | null = null;
    private updateDialogDismissed: boolean = false;
    private interval: ReturnType<typeof setInterval> | undefined = undefined;
    private isCheckingForUpdates: boolean = false;
    private isDownloadingUpdate: boolean = false;
    private hasRunInitialAutoCheck: boolean = false;
    private releaseNoteInfoSchema = z.object({
        note: z.string(),
        version: z.string().optional(),
    });
    private updateInfoSchema = z.object({
        version: z.string(),
        releaseNotes: z.string().nullable(),
    });

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public initialize(): void {
        autoUpdater.on("error", (err) => {
            if (this.isDownloadingUpdate) {
                this.isCheckingForUpdates = false;
                this.isDownloadingUpdate = false;
            } else {
                this.isCheckingForUpdates = false;
                this.isDownloadingUpdate = false;
                this.updateDownloaded = false;
                this.updateAvailable = false;
                this.releaseNotesUrl = null;
                this.updateDialogDismissed = false;
            }
            this.broadcastStatus();

            this.desktop.logger.log("error", err, "updater");
        });

        autoUpdater.on("update-available", (info) => {
            const { version, releaseNotes } = this.updateInfoSchema.parse(info);
            this.isCheckingForUpdates = false;
            this.updateAvailable = true;
            this.releaseVersion = version;
            this.releaseNotesUrl = releaseNotes && this.extractReleaseNotesUrl(releaseNotes);
            this.broadcastStatus();
            this.broadcastUpdateAvailable();
        });

        autoUpdater.on("update-not-available", () => {
            this.isCheckingForUpdates = false;
            this.isDownloadingUpdate = false;
            this.updateDownloaded = false;
            this.updateAvailable = false;
            this.releaseNotesUrl = null;
            this.updateDialogDismissed = false;
            this.broadcastStatus();
        });

        autoUpdater.on("update-downloaded", async () => {
            this.isCheckingForUpdates = false;
            this.isDownloadingUpdate = false;
            this.updateDownloaded = true;
            this.updateDialogDismissed = false;
            this.broadcastStatus();
            await this.notifyUpdateReady();
        });

        autoUpdater.on("download-progress", () => {
            if (!this.isDownloadingUpdate) {
                this.isDownloadingUpdate = true;
                this.broadcastStatus();
            }
        });

        autoUpdater.on("update-cancelled", () => {
            this.isDownloadingUpdate = false;
            this.broadcastStatus();
        });

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

        const mode = await this.desktop.setting.general.getAutoUpdateMode();
        autoUpdater.autoDownload = mode === "auto";

        if (this.updateDownloaded) {
            if (userInitiated) {
                this.updateDialogDismissed = false;
            }
            await this.notifyUpdateReady();
            return;
        }

        if (this.updateAvailable) {
            if (mode === "auto" && !this.isDownloadingUpdate) {
                await this.downloadUpdate();
            }
            return;
        }
        this.isCheckingForUpdates = true;
        this.broadcastStatus();

        await autoUpdater.checkForUpdates();
    }

    private async runInitialAutomaticCheck(): Promise<void> {
        if (this.hasRunInitialAutoCheck) {
            return;
        }

        this.hasRunInitialAutoCheck = true;

        try {
            const autoUpdateMode = await this.desktop.setting.general.getAutoUpdateMode();
            if (autoUpdateMode === "off") {
                return;
            }

            await this.checkForUpdates();
        } catch (err) {
            this.desktop.logger.log("error", err, "updater.initialCheck");
            this.desktop.logger.log("error", err);
        }
    }

    private async runAutomaticCheck(): Promise<void> {
        const autoUpdateMode = await this.desktop.setting.general.getAutoUpdateMode();
        if (autoUpdateMode === "off") {
            return;
        }

        await this.checkForUpdates();
    }

    public async handleAutoUpdateModeChanged(mode: AutoUpdateMode): Promise<void> {
        autoUpdater.autoDownload = mode === "auto";

        if (
            mode === "auto" &&
            this.updateAvailable &&
            !this.updateDownloaded &&
            !this.isDownloadingUpdate
        ) {
            await this.downloadUpdate();
            return;
        }

        if (
            mode !== "off" &&
            !this.updateAvailable &&
            !this.updateDownloaded &&
            !this.isCheckingForUpdates
        ) {
            await this.checkForUpdates();
            return;
        }

        this.broadcastStatus();
    }

    public async getStatus(): Promise<UpdaterStatus> {
        const mode = await this.desktop.setting.general.getAutoUpdateMode();

        return {
            mode,
            updateAvailable: this.updateAvailable,
            updateDownloaded: this.updateDownloaded,
            releaseNotesUrl: this.releaseNotesUrl,
            shouldPromptForUpdate: this.updateDownloaded && !this.updateDialogDismissed,
            isChecking: this.isCheckingForUpdates,
            isDownloading: this.isDownloadingUpdate,
        };
    }

    private extractReleaseNotesUrl(releaseNotes: string): string | null {
        const notionLinkPattern = /<p>\s*notion:\s*<a[^>]+href="([^"]+)"[^>]*>/i;
        const notionLinkMatch = notionLinkPattern.exec(releaseNotes);
        if (notionLinkMatch?.[1]) {
            return notionLinkMatch[1];
        }

        const fallbackPattern = /<a[^>]+href="([^"]*notion\.so[^"]*)"[^>]*>/i;
        const fallbackMatch = fallbackPattern.exec(releaseNotes);
        return fallbackMatch?.[1] ?? null;
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
        this.broadcastStatus();
    }

    public async downloadUpdate(): Promise<void> {
        if (this.updateDownloaded || !this.updateAvailable || this.isDownloadingUpdate) {
            return;
        }

        this.isDownloadingUpdate = true;
        this.broadcastStatus();

        try {
            await autoUpdater.downloadUpdate();
        } catch (err) {
            this.isDownloadingUpdate = false;
            this.broadcastStatus();
            throw err;
        }
    }

    private async notifyUpdateReady(): Promise<void> {
        const mainWindow = await this.focusMainWindow();
        if (!mainWindow) {
            return;
        }

        this.desktop.ipc.postMessageToWindow(mainWindow, "updater:update-downloaded");
    }

    private broadcastUpdateAvailable(): void {
        this.desktop.ipc.broadcast("updater:update-available");
    }

    private broadcastStatus(): void {
        void this.getStatus().then((status) => {
            this.desktop.ipc.broadcast("updater:status-changed", status);
        });
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
