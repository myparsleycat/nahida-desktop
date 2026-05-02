const { autoUpdater } = require("electron-updater");

import type {
    AutoUpdateMode,
    ReleaseNoteTranslationLanguage,
    UpdaterStatus,
} from "@shared/updater";
import { app, BrowserWindow } from "electron";
import { convert as htmlToText } from "html-to-text";
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

const RELEASE_NOTES_TRANSLATION_URL = "https://translate.nahida.live";

export class Updater {
    private readonly desktop: NahidaDesktop;
    public updateDownloaded: boolean = false;
    public updateAvailable: boolean = false;
    private releaseVersion: string | null = null;
    private originalReleaseNotesText: string | null = null;
    private translatedReleaseNotesText: string | null = null;
    private translatedLanguage: ReleaseNoteTranslationLanguage | null = null;
    private updateDialogDismissed: boolean = false;
    private interval: ReturnType<typeof setInterval> | undefined = undefined;
    private isCheckingForUpdates: boolean = false;
    private isDownloadingUpdate: boolean = false;
    private hasRunInitialAutoCheck: boolean = false;
    private releaseNotesTranslationRequestId: number = 0;
    private releaseNoteInfoSchema = z.object({
        note: z.string(),
        version: z.string().optional().nullable(),
    });
    private updateInfoSchema = z.object({
        version: z.string(),
        releaseNotes: z
            .union([
                z.string(),
                this.releaseNoteInfoSchema,
                z.array(this.releaseNoteInfoSchema),
                z.null(),
            ])
            .optional(),
    });
    private translationResponseSchema = z.object({
        response: z.union([
            z.string(),
            z.object({
                choices: z.array(
                    z.object({
                        message: z.object({
                            content: z.string().nullable().optional(),
                        }),
                    }),
                ),
            }),
        ]),
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
                this.releaseVersion = null;
                this.clearReleaseNotes();
                this.updateDialogDismissed = false;
            }
            this.broadcastStatus();

            this.desktop.logger.log("error", err, "updater");
        });

        autoUpdater.on("update-available", async (info) => {
            const { version, releaseNotes } = this.updateInfoSchema.parse(info);
            this.isCheckingForUpdates = false;
            this.updateAvailable = true;
            this.releaseVersion = version;
            this.originalReleaseNotesText = this.normalizeReleaseNotes(releaseNotes);
            this.translatedReleaseNotesText = null;
            this.translatedLanguage = null;
            this.broadcastStatus();
            this.broadcastUpdateAvailable();

            try {
                const language = await this.desktop.setting.general.getLanguage();
                await this.translateCurrentReleaseNotes(language);
            } catch (err) {
                this.desktop.logger.log("error", err, "updater.translateReleaseNotes");
                this.desktop.logger.log("error", err);
            }
        });

        autoUpdater.on("update-not-available", () => {
            this.isCheckingForUpdates = false;
            this.isDownloadingUpdate = false;
            this.updateDownloaded = false;
            this.updateAvailable = false;
            this.releaseVersion = null;
            this.clearReleaseNotes();
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

    public async handleLanguageChanged(language: string): Promise<void> {
        if (!this.originalReleaseNotesText) {
            return;
        }

        await this.translateCurrentReleaseNotes(language);
    }

    public async getStatus(): Promise<UpdaterStatus> {
        const mode = await this.desktop.setting.general.getAutoUpdateMode();

        return {
            mode,
            updateAvailable: this.updateAvailable,
            updateDownloaded: this.updateDownloaded,
            releaseVersion: this.releaseVersion,
            releaseNotes: this.getReleaseNotes(),
            shouldPromptForUpdate: this.updateDownloaded && !this.updateDialogDismissed,
            isChecking: this.isCheckingForUpdates,
            isDownloading: this.isDownloadingUpdate,
        };
    }

    private getReleaseNotes() {
        if (!this.originalReleaseNotesText && !this.translatedReleaseNotesText) {
            return null;
        }

        return {
            original: this.originalReleaseNotesText,
            translated: this.translatedReleaseNotesText,
            translatedLanguage: this.translatedLanguage,
        };
    }

    private normalizeReleaseNotes(
        releaseNotes:
            | string
            | z.infer<typeof this.releaseNoteInfoSchema>
            | z.infer<typeof this.releaseNoteInfoSchema>[]
            | null
            | undefined,
    ): string | null {
        if (!releaseNotes) {
            return null;
        }

        if (typeof releaseNotes === "string") {
            return this.htmlToPlainText(releaseNotes);
        }

        if (Array.isArray(releaseNotes)) {
            const sections = releaseNotes
                .map((item) => this.formatReleaseNoteSection(item))
                .filter((item) => item.length > 0);
            const joined = sections.join("\n\n").trim();
            return joined.length > 0 ? joined : null;
        }

        return this.formatReleaseNoteSection(releaseNotes);
    }

    private formatReleaseNoteSection(noteInfo: z.infer<typeof this.releaseNoteInfoSchema>): string {
        const versionPrefix = noteInfo.version ? `v${noteInfo.version}\n` : "";
        const noteText = this.htmlToPlainText(noteInfo.note);
        return `${versionPrefix}${noteText}`.trim();
    }

    private htmlToPlainText(value: string): string | null {
        const text = htmlToText(value, {
            wordwrap: false,
        })
            .replace(/\r\n/g, "\n")
            .replace(/\n{3,}/g, "\n\n")
            .trim();

        return text.length > 0 ? text : null;
    }

    private clearReleaseNotes(): void {
        this.originalReleaseNotesText = null;
        this.translatedReleaseNotesText = null;
        this.translatedLanguage = null;
        this.releaseNotesTranslationRequestId += 1;
    }

    private isTranslationLanguage(language: string): language is ReleaseNoteTranslationLanguage {
        return language === "ko" || language === "ja" || language === "zh";
    }

    private async translateCurrentReleaseNotes(language: string): Promise<void> {
        const originalText = this.originalReleaseNotesText;
        const releaseVersion = this.releaseVersion;
        const requestId = ++this.releaseNotesTranslationRequestId;

        if (!originalText || !releaseVersion) {
            return;
        }

        if (!this.isTranslationLanguage(language)) {
            const hadTranslation =
                this.translatedReleaseNotesText !== null || this.translatedLanguage !== null;
            this.translatedReleaseNotesText = null;
            this.translatedLanguage = null;
            if (hadTranslation) {
                this.broadcastStatus();
            }
            return;
        }

        try {
            const translatedText = await this.translateReleaseNotes(originalText, language);

            if (
                requestId !== this.releaseNotesTranslationRequestId ||
                this.originalReleaseNotesText !== originalText ||
                this.releaseVersion !== releaseVersion
            ) {
                return;
            }

            if (!translatedText) {
                const hadTranslation =
                    this.translatedReleaseNotesText !== null || this.translatedLanguage !== null;
                this.translatedReleaseNotesText = null;
                this.translatedLanguage = null;
                if (hadTranslation) {
                    this.broadcastStatus();
                }
                return;
            }

            this.translatedReleaseNotesText = translatedText;
            this.translatedLanguage = language;
            this.broadcastStatus();
        } catch (err) {
            if (requestId !== this.releaseNotesTranslationRequestId) {
                return;
            }

            this.translatedReleaseNotesText = null;
            this.translatedLanguage = null;
            this.desktop.logger.log("error", err, "updater.translateReleaseNotes");
            this.desktop.logger.log("error", err);
            this.broadcastStatus();
        }
    }

    private async translateReleaseNotes(
        originalText: string,
        language: ReleaseNoteTranslationLanguage,
    ): Promise<string | null> {
        const response = await this.desktop.httpService.fetcher(RELEASE_NOTES_TRANSLATION_URL, {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({
                source: "English",
                target: this.getLanguageName(language),
                text: originalText,
            }),
        });

        const responseText = await response.text();

        if (!response.ok) {
            throw new Error(
                `Release notes translation failed with status ${response.status}: ${responseText}`,
            );
        }

        const translatedText = this.extractTranslatedText(responseText);

        if (!translatedText || translatedText === originalText) {
            return null;
        }

        return translatedText;
    }

    private extractTranslatedText(responseText: string): string {
        const trimmedResponseText = responseText.trim();

        if (!trimmedResponseText) {
            return "";
        }

        try {
            const parsed = this.translationResponseSchema.parse(JSON.parse(trimmedResponseText));

            if (typeof parsed.response === "string") {
                return parsed.response.trim();
            }

            return parsed.response.choices[0]?.message.content?.trim() ?? "";
        } catch {
            return trimmedResponseText;
        }
    }

    private getLanguageName(language: ReleaseNoteTranslationLanguage): string {
        switch (language) {
            case "ko":
                return "Korean";
            case "ja":
                return "Japanese";
            case "zh":
                return "Simplified Chinese";
        }
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
