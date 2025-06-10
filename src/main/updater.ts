import { dialog } from 'electron';
import ProgressBar from 'electron-progressbar';
import electronUpdater, { type AppUpdater } from 'electron-updater';
import log from 'electron-log';

class AutoUpdaterManager {
    private static instance: AutoUpdaterManager | null = null;
    private autoUpdater: AppUpdater;
    private progressBar: ProgressBar | null = null;

    private constructor() {
        // Using destructuring to access autoUpdater due to the CommonJS module of 'electron-updater'.
        // It is a workaround for ESM compatibility issues, see https://github.com/electron-userland/electron-builder/issues/7976.
        const { autoUpdater } = electronUpdater;
        this.autoUpdater = autoUpdater;
        this.setupAutoUpdater();
    }

    public static getInstance(): AutoUpdaterManager {
        if (!AutoUpdaterManager.instance) {
            AutoUpdaterManager.instance = new AutoUpdaterManager();
        }
        return AutoUpdaterManager.instance;
    }

    private setupAutoUpdater(): void {
        this.autoUpdater.autoDownload = false;

        this.autoUpdater.on("checking-for-update", () => {
            log.info("Checking for updates");
        });

        this.autoUpdater.on("update-available", (au) => {
            log.info("Update version detected");

            dialog
                .showMessageBox({
                    type: "info",
                    title: `New Update Available: v${au.version}`,
                    message:
                        "새로운 버전으로 업데이트 할 수 있습니다. 지금 진행할까요?",
                    buttons: ["확인", "나중에 진행"]
                })
                .then(result => {
                    const { response } = result;

                    if (response === 0) {
                        this.progressBar = new ProgressBar({
                            detail: 'Wait...',
                            text: "Download Files...",
                            initialValue: 0,
                            maxValue: 100
                        });

                        this.progressBar
                            .on("completed", () => {
                                log.info(`completed...`);
                                if (this.progressBar) this.progressBar.detail = 'Update completed. Closing...';
                            })
                            .on("aborted", () => {
                                log.info("aborted");
                            })
                            .on("progress", (percent: number) => {
                                if (this.progressBar) this.progressBar.text = `Download Files... ${percent}%`;
                            });

                        this.autoUpdater.downloadUpdate();
                    }
                });
        });

        this.autoUpdater.on("update-not-available", () => {
            log.info("Update not available");
        });

        this.autoUpdater.on("download-progress", (pg) => {
            if (!this.progressBar) return;

            const percent = Math.floor(pg.percent);
            this.progressBar.value = percent;
            this.progressBar.text = `Download Files... ${percent}%`;
        });

        this.autoUpdater.on("update-downloaded", () => {
            if (this.progressBar) {
                this.progressBar.setCompleted();
            }

            dialog
                .showMessageBox({
                    type: "info",
                    title: "Update",
                    message: "새로운 버전이 다운로드 되었습니다. 다시 시작할까요?",
                    buttons: ["Yes", "No"]
                })
                .then(result => {
                    const { response } = result;
                    if (response === 0) this.autoUpdater.quitAndInstall(true, true);
                });
        });
    }

    public getAutoUpdater(): AppUpdater {
        return this.autoUpdater;
    }

    public checkForUpdates(): void {
        this.autoUpdater.checkForUpdatesAndNotify();
    }

    public checkForUpdatesAndNotify(): void {
        this.autoUpdater.checkForUpdatesAndNotify();
    }
}

export function getAutoUpdater(): AppUpdater {
    return AutoUpdaterManager.getInstance().getAutoUpdater();
}