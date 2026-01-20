import updaterPkg from "electron-updater";
const { autoUpdater } = updaterPkg; // esm support
import { serializeError } from "./utils";
import { BrowserWindow, app, dialog, Notification } from "electron";
import ProgressBar from "electron-progressbar";
import { convert } from "html-to-text";
import isDev from "./isDev";
import { NahidaDesktop } from "..";

autoUpdater.allowDowngrade = false;
autoUpdater.autoDownload = false;
autoUpdater.autoInstallOnAppQuit = false;
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
    private interval: ReturnType<typeof setInterval> | undefined = undefined;
    private progressBar: ProgressBar | null = null;
    private isManualCheck: boolean = false;

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public initialize(): void {
        autoUpdater.on("checking-for-update", () => {
            this.desktop.logger.log("info", "Checking for update");
            if (this.isManualCheck) {
                new Notification({
                    title: "Nahida Desktop",
                    body: "업데이트 확인중...",
                }).show();
            }
        });

        autoUpdater.on("download-progress", (progress) => {
            if (!this.progressBar) return;

            const percent = Math.floor(progress.percent);
            this.progressBar.value = percent;
            this.progressBar.text = `Download Files... ${percent}%`;
        });

        autoUpdater.on("error", (err) => {
            this.updateDownloaded = false;
            this.updateAvailable = false;

            this.desktop.logger.log("error", err, "updater");
        });

        autoUpdater.on("update-available", (updateInfo) => {
            this.updateAvailable = true;

            this.desktop.logger.log("info", "Update available");

            dialog
                .showMessageBox({
                    type: "info",
                    title: `New Update Available: v${updateInfo.version}`,
                    message: "새로운 버전으로 업데이트 할 수 있습니다. 지금 진행할까요?",
                    detail: convert(String(updateInfo.releaseNotes)),
                    buttons: ["확인", "나중에 진행"],
                })
                .then((result) => {
                    const { response } = result;

                    if (response === 0) {
                        this.progressBar = new ProgressBar({
                            detail: "Wait...",
                            text: "Download Files...",
                            initialValue: 0,
                            maxValue: 100,
                        });

                        this.progressBar
                            .on("completed", () => {
                                this.desktop.logger.log("info", `completed...`);
                                if (this.progressBar)
                                    this.progressBar.detail = "Update completed. Closing...";
                            })
                            .on("aborted", () => {
                                this.desktop.logger.log("info", "aborted");
                            })
                            .on("progress", (percent: number) => {
                                if (this.progressBar)
                                    this.progressBar.text = `Download Files... ${percent}%`;
                            });

                        autoUpdater.downloadUpdate();
                    }
                });
        });

        autoUpdater.on("update-not-available", () => {
            this.updateDownloaded = false;
            this.updateAvailable = false;

            this.desktop.logger.log("info", "No update available");

            if (this.isManualCheck) {
                new Notification({
                    title: "Nahida Desktop",
                    body: "지금은 업데이트가 없습니다",
                }).show();
            }
        });

        autoUpdater.on("update-downloaded", (info) => {
            this.updateDownloaded = true;

            this.desktop.logger.log("info", `Update downloaded: ${JSON.stringify(info)}`);

            if (this.progressBar) {
                this.progressBar.setCompleted();
            }

            dialog
                .showMessageBox({
                    type: "info",
                    title: "Update",
                    message: "새로운 버전이 다운로드 되었습니다. 다시 시작할까요?",
                    buttons: ["Yes", "No"],
                })
                .then((result) => {
                    const { response } = result;
                    if (response === 0) {
                        this.installUpdate();
                    }
                });
        });

        autoUpdater.on("update-cancelled", () => {
            this.desktop.logger.log("info", "Update cancelled");
        });

        autoUpdater.checkForUpdates().catch((err) => {
            this.desktop.logger.log("error", err, "updater.interval");
            this.desktop.logger.log("error", err);
        });

        clearInterval(this.interval);

        this.interval = setInterval(() => {
            autoUpdater.checkForUpdates().catch((err) => {
                this.desktop.logger.log("error", err, "updater.interval");
                this.desktop.logger.log("error", err);
            });
        }, 3600000);
    }

    public async checkForUpdates(manual: boolean = false): Promise<void> {
        this.isManualCheck = manual;
        await autoUpdater.checkForUpdates();
    }

    public async installUpdate(): Promise<void> {
        if (!this.updateDownloaded || !this.updateAvailable) {
            throw new Error("No update available to install.");
        }

        this.desktop.shouldExitOnQuit = true;

        this.desktop.logger.log("info", "Installing update");

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
