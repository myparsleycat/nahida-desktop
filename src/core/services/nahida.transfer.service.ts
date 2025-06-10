// src/core/services/nahida.transfer.service.ts

import { nanoid } from "nanoid";
import PQueue from "p-queue";
import { ToastService } from "./toast.service";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import fse from 'fs-extra';
import { FSService } from "./fs.service";
import { ProxyUrl } from "@core/const";
import { extractFile } from "@core/lib/extractor";
import { Notification } from "electron";
import { Mod } from "@shared/types/nahida.types";
import { SettingService } from "./setting.service";
import { fixGenshinMod } from "@core/lib/mod/fix/genshin.hash.fix";
import { fixHSRMod } from "@core/lib/mod/fix/hsr.hash.fix";
import { processFolder } from "@core/lib/mod/fix/zzz.hash.fix";
import wuwaModFix from "@core/lib/mod/fix/wuwa.fix";

type DownloadStatus = 'pending' | 'downloading' | 'extracting' | 'completed' | 'failed';

interface DownloadTask {
    PID: string;
    fileUrl: string;
    fileName: string;
    mod: Mod;
    savePath: string;
    priority?: number;
}

interface DownloadProgress {
    PID: string;
    status: DownloadStatus;
    fileUrl: string;
    mod: Mod;
    savePath: string;
    speed: number;
    progress: number;
    downloadedBytes: number;
    abortController: AbortController;
}

interface CompleteProcess {
    pid: string;
    name: string;
    size: number;
}

interface TransferStore {
    download: {
        active: Map<string, DownloadProgress>;
        completed: CompleteProcess[];
    }
}

class NahidaTransferServiceClass {
    transfers: TransferStore = {
        download: {
            active: new Map(),
            completed: []
        }
    }

    private downloadQueue = new PQueue({ concurrency: 3 });

    private queueStats = {
        running: 0,
        pending: 0,
        completed: 0
    };

    constructor() {
        this.downloadQueue.on('active', () => {
            this.queueStats.running++;
            this.queueStats.pending = Math.max(0, this.queueStats.pending - 1);
            console.log(`큐 활성: ${this.queueStats.pending} 대기, ${this.queueStats.running} 실행중`);
        });

        this.downloadQueue.on('completed', () => {
            this.queueStats.running = Math.max(0, this.queueStats.running - 1);
            this.queueStats.completed++;
        });

        this.downloadQueue.on('add', () => {
            this.queueStats.pending++;
        });

        this.downloadQueue.on('idle', () => {
            console.log('모든 다운로드 완료');
            this.queueStats.running = 0;
            this.queueStats.pending = 0;
        });

        this.downloadQueue.on('error', (error) => {
            console.error('다운로드 큐 에러:', error);
        });
    }

    download = {
        enqueue: async (url: string, savePath: string, mod: Mod, options?: {
            priority?: number;
        }) => {
            try {
                const headresp = await fetch(url, {
                    method: 'HEAD',
                    redirect: 'follow'
                });

                if (!headresp.ok) {
                    const er = `HTTP error status: ${headresp.status}`;
                    ToastService.error('나히다 모드 다운로드 실패', {
                        description: er
                    })
                    return false;
                }

                let fileName = mod.title + '.zip';

                const PID = nanoid();
                const task: DownloadTask = {
                    PID,
                    fileUrl: url,
                    fileName,
                    mod,
                    savePath,
                    priority: options?.priority || 0
                };

                this.downloadQueue.add(
                    () => this.download.processDownload(task),
                    { priority: options?.priority || 0 }
                ).then();

                return true;
            } catch (err: any) {
                ToastService.error('대기열 등록 중 오류 발생', {
                    description: err.message
                });
                return false;
            }
        },

        processDownload: async (task: DownloadTask) => {
            const { PID, fileUrl, fileName, mod, savePath } = task;
            const abortController = new AbortController();

            try {
                const progress: DownloadProgress = {
                    PID,
                    status: 'pending',
                    fileUrl,
                    mod,
                    savePath,
                    speed: 0,
                    progress: 0,
                    downloadedBytes: 0,
                    abortController
                };

                this.transfers.download.active.set(PID, progress);

                progress.status = 'downloading';

                const filePath = `${savePath}/${fileName}`;

                await FSService.mkdir(savePath, { recursive: true });

                await this.download.downloadFileWithProgress(
                    fileUrl,
                    filePath,
                    abortController.signal,
                    (progressPercent, downloadedBytes, speed) => {
                        progress.progress = progressPercent;
                        progress.downloadedBytes = downloadedBytes;
                        progress.speed = speed;
                    }
                );

                progress.progress = 100;
                progress.status = 'extracting';

                const extractedPath = await extractFile({ filePath, delAfter: true });

                progress.status = 'completed';
                ToastService.success(`${mod.title}의 다운로드가 완료되었습니다`);
                new Notification({ title: '다운로드 완료', body: `${fileName} 의 다운로드가 완료되었습니다` }).show();

                const autofix = await SettingService.autofix.nahida.get();
                if (autofix) {
                    switch (mod.game) {
                        case 'genshin':
                            fixGenshinMod(extractedPath).then(() => { })
                            break;
                        case 'starrail':
                            fixHSRMod(extractedPath).then(() => { })
                            break;
                        case 'zzz':
                            processFolder(extractedPath).then(() => { })
                            break;
                        case 'wuwa':
                            wuwaModFix(extractedPath).then(() => { })
                            break;
                    }
                }

                this.transfers.download.completed.push({
                    pid: PID,
                    name: fileName,
                    size: mod.size || 0
                });

                this.transfers.download.active.delete(PID);
                return true;
            } catch (error: any) {
                console.error(`다운로드 실패 [${fileName}]:`, error);

                const progress = this.transfers.download.active.get(PID);
                if (progress) {
                    progress.status = 'failed';
                    this.transfers.download.active.delete(PID);
                }

                try {
                    const filePath = `${savePath}/${fileName}`;
                    if (await FSService.exists(filePath)) {
                        await FSService.deletePath(filePath);
                    }
                } catch (cleanupError: any) {
                    console.error('파일 정리 실패:', cleanupError.message);
                }

                throw error;
            }
        },

        downloadFileWithProgress: async (
            url: string,
            path: string,
            abortSignal: AbortSignal,
            onProgress?: (progress: number, downloadedSize: number, speed: number) => void
        ) => {
            const resp = await fetch(ProxyUrl + url, { signal: abortSignal });

            if (!resp.ok) {
                throw new Error(`HTTP error! status: ${resp.status}`);
            } else if (!resp.body) {
                throw new Error('body is empty');
            }

            try {
                const totalSize = parseInt(resp.headers.get('content-length') || '0');
                let downloadedSize = 0;

                let lastTime = Date.now();
                let lastDownloadedSize = 0;
                let speed = 0;

                const readableStream = new Readable({
                    read() {
                    }
                });

                const reader = resp.body.getReader();

                const pump = async () => {
                    try {
                        while (true) {
                            const { done, value } = await reader.read();

                            if (done) {
                                readableStream.push(null);
                                break;
                            }

                            downloadedSize += value.length;

                            if (onProgress && totalSize > 0) {
                                const currentTime = Date.now();
                                const timeDiff = currentTime - lastTime;

                                if (timeDiff >= 1000 || lastTime === currentTime) {
                                    const sizeDiff = downloadedSize - lastDownloadedSize;
                                    speed = timeDiff > 0 ? Math.round((sizeDiff * 1000) / timeDiff) : 0;

                                    lastTime = currentTime;
                                    lastDownloadedSize = downloadedSize;
                                }

                                const progress = (downloadedSize / totalSize) * 100;
                                onProgress(progress, downloadedSize, speed);
                            }

                            readableStream.push(value);
                        }
                    } catch (err: any) {
                        readableStream.destroy(err);
                    }
                };

                pump();

                const writeStream = fse.createWriteStream(path);
                await pipeline(readableStream, writeStream);
            } catch (e: any) {
                if (e.name === 'AbortError') {
                    console.log('다운로드가 취소됨');
                }
                throw e;
            }
        },

        pause: () => {
            this.downloadQueue.pause();
        },

        resume: () => {
            this.downloadQueue.start();
        },

        clear: () => {
            this.downloadQueue.clear();
            this.queueStats.pending = 0;
            this.queueStats.running = 0;
        },

        cancel: (PID: string) => {
            const progress = this.transfers.download.active.get(PID);
            if (progress) {
                progress.abortController.abort();
                progress.status = 'failed';
                this.transfers.download.active.delete(PID);
            }
        },

        getStatus: () => ({
            active: Array.from(this.transfers.download.active.values()),
            pending: this.downloadQueue.pending,
            completed: this.transfers.download.completed
        })
    };
}

export const NahidaTransferService = new NahidaTransferServiceClass();