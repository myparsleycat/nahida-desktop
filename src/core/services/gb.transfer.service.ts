import { FileData, GameBanana, ModProfileResponse } from "@core/lib/gamebanana";
import { nanoid } from "nanoid";
import path from "node:path";
import PQueue from "p-queue";
import { FSService } from "./fs.service";
import { pipeline } from "node:stream/promises";
import fse from "fs-extra";
import { ProxyUrl } from "@core/const";
import { Readable } from "node:stream";
import { extractFile } from "@core/lib/extractor";
import { ToastService } from "./toast.service";
import { SettingService } from "./setting.service";
import { fixGenshinMod } from "@core/lib/mod/fix/genshin.hash.fix";
import { fixHSRMod } from "@core/lib/mod/fix/hsr.hash.fix";
import { processFolder } from "@core/lib/mod/fix/zzz.hash.fix";
import { wuwaModFix } from "@core/lib/mod/fix/wuwa.fix";
import { fileTypeFromBuffer } from "file-type";

type DownloadStatus = 'pending' | 'downloading' | 'extracting' | 'completed' | 'failed';

interface DownloadTask {
    PID: string;
    modData: ModProfileResponse;
    fileData: FileData;
    url: string;
    status: DownloadStatus;
    progress: number; // 0 to 100
    downloadedBytes: number;
    speed?: number;
    savePath: string;
    priority?: number;
    abortController?: AbortController;
}

interface TransferStore {
    download: {
        active: Map<string, DownloadTask>;
    }
}

class GBTransferServiceClass {
    transfers: TransferStore = {
        download: {
            active: new Map()
        }
    }

    private downloadQueue = new PQueue({ concurrency: 3 });

    private queueStats = {
        running: 0,
        pending: 0,
        completed: 0
    };

    download = {
        enqueue: async (path: string, modData: ModProfileResponse, fileData: FileData, url: string, options?: {
            priority?: number;
        }) => {
            try {
                const PID = nanoid();
                const task: DownloadTask = {
                    PID,
                    modData,
                    fileData,
                    url,
                    status: 'pending',
                    progress: 0,
                    savePath: path,
                    priority: options?.priority ?? 0,
                    downloadedBytes: 0
                };

                this.downloadQueue.add(
                    () => this.download.processDownload(task),
                    { priority: options?.priority || 0 }
                ).then();

                return true;
            } catch (err: any) {
                console.error(`Error during GBTransfer.download.enqueue`, err);
                return false;
            }
        },

        processDownload: async (task: DownloadTask) => {
            const { PID, modData, fileData, url, savePath } = task;
            const abortController = new AbortController();

            try {
                const imageData = GameBanana.GetImagesData(modData);
                const previewPromise = fetch(ProxyUrl + imageData[0].url).then(resp => resp.arrayBuffer());

                this.transfers.download.active.set(PID, task);
                task.status = 'downloading';

                const filePath = path.join(savePath, fileData.name);
                await FSService.mkdir(savePath, { recursive: true });

                await this.download.downloadFileWithProgress(
                    fileData.url,
                    filePath,
                    abortController.signal,
                    (progress, downloadedSize, speed) => {
                        task.progress = progress;
                        task.downloadedBytes = downloadedSize;
                        task.speed = speed;
                    }
                );

                task.progress = 100;
                task.status = 'extracting';

                const extractedPath = await extractFile({ filePath, delAfter: true });

                task.status = 'completed';
                ToastService.success(`${modData._sName} 다운로드 완료`);

                const previewData = await previewPromise;
                const previewMetadata = await fileTypeFromBuffer(previewData);
                const previewPath = path.join(extractedPath, `preview.${previewMetadata?.ext}`);
                await FSService.writeFile(previewPath, Buffer.from(previewData));

                const autofix = await SettingService.autofix.nahida.get();
                if (autofix) {
                    switch (modData.__aGame._sAbbreviation) {
                        case 'GI':
                            fixGenshinMod(extractedPath).then(() => { })
                            break;
                        case 'HSR':
                            fixHSRMod(extractedPath).then(() => { })
                            break;
                        case 'ZZZ':
                            processFolder(extractedPath).then(() => { })
                            break;
                        case 'WuWa':
                            wuwaModFix(extractedPath).then(() => { })
                            break;
                    }
                }

                this.transfers.download.active.delete(PID);
                return true;
            } catch (err: any) {
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

                const readableStream = new Readable({ read() { } });

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
            const task = this.transfers.download.active.get(PID);
            if (task) {
                task.abortController?.abort();
                task.status = 'failed';
                this.transfers.download.active.delete(PID);
            }
        },
    }
}

export const GBTransferService = new GBTransferServiceClass();