import { nanoid } from "nanoid";
import { ToastService } from "./toast.service";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import fse from 'fs-extra';
import { FSService } from "./fs.service";
import { ProxyUrl } from "@core/const";

type DownloadStatus = 'pending' | 'downloading';

interface TransferStore {
  download: {
    current: {
      PID: string;
      status: DownloadStatus;
      fileUrl: string;
      fileName: string;
      size: number | null;
      savePath: string;
      speed: number;
      progress: number;
      downloadedBytes: number;
      abortController: AbortController;
    } | null;
    queue: {
      PID: string;
      fileUrl: string;
      fileName: string;
      size: number | null;
      savePath: string;
    }[]
    isProcessing: boolean;
  }
}

class NahidaTransferService {
  transfers: TransferStore = {
    download: {
      current: null,
      queue: [],
      isProcessing: false
    }
  }

  download = {
    enqueue: async (url: string, savePath: string, name?: string) => {
      const PID = nanoid();

      try {
        const headResponse = await fetch(ProxyUrl + url, {
          method: 'HEAD',
          redirect: 'follow'
        });

        if (!headResponse.ok) {
          throw new Error(`HTTP error! status: ${headResponse.status}`);
        }

        const contentLength = headResponse.headers.get('content-length');
        const fileSize = contentLength ? parseInt(contentLength, 10) : null;

        let fileName = name;

        if (!fileName) {
          const contentDisposition = headResponse.headers.get('content-disposition');
          if (contentDisposition) {
            const filenameMatch = contentDisposition.match(/filename[^;=\n]*=((['"]).*?\2|[^;\n]*)/);
            if (filenameMatch && filenameMatch[1]) {
              fileName = filenameMatch[1].replace(/['"]/g, '');
            }
          }

          if (!fileName) {
            const finalUrl = headResponse.url;
            const urlPath = new URL(finalUrl).pathname;
            fileName = urlPath.split('/').pop() || 'unknown';

            fileName = fileName.split('?')[0];
          }
        }

        this.transfers.download.queue.push({ PID, fileUrl: url, fileName, size: fileSize, savePath });

        if (!this.transfers.download.current) {
          await this.download.processNextDownloadInQueue();
        }

        return true;
      } catch (err: any) {
        ToastService.error('대기열 등록 중 오류 발생', {
          description: err.message
        });
        return false;
      }
    },

    processNextDownloadInQueue: async () => {
      const nextDownload = this.transfers.download.queue.shift();
      if (this.transfers.download.isProcessing || !nextDownload) {
        return;
      }

      const { PID, fileUrl, fileName, size, savePath } = nextDownload;
      const abortController = new AbortController();

      this.transfers.download = {
        ...this.transfers.download,
        isProcessing: true,
        current: {
          PID,
          status: 'pending',
          fileUrl,
          fileName,
          size,
          savePath,
          speed: 0,
          progress: 0,
          downloadedBytes: 0,
          abortController
        }
      }

      this.download.downloadProcess();
    },

    completeCurrentDownload: async () => {
      this.transfers.download = {
        ...this.transfers.download,
        isProcessing: false,
        current: null
      }

      if (this.transfers.download.queue.length > 0) {
        await this.download.processNextDownloadInQueue();
      }
    },

    downloadFileWithProgress: async (
      url: string,
      path: string,
      onProgress?: (progress: number, downloadedSize: number, speed: number) => void
    ) => {
      try {
        const resp = await fetch(ProxyUrl + url);

        if (!resp.ok) {
          throw new Error(`HTTP error! status: ${resp.status}`);
        } else if (!resp.body) {
          throw new Error('body is empty');
        }

        const totalSize = parseInt(resp.headers.get('content-length') || '0');
        let downloadedSize = 0;

        // 속도 계산을 위한 변수들
        let lastTime = Date.now();
        let lastDownloadedSize = 0;
        let speed = 0;

        const readableStream = new Readable({
          read() { }
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

                // 1초마다 또는 처음 호출시 속도 계산
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
        try {
          if (await FSService.exists(path)) {
            await FSService.deletePath(path);
          }
        } catch (cleanupError: any) {
          console.error('파일 정리 실패:', cleanupError.message);
        }
      }
    },

    downloadProcess: async () => {
      if (!this.transfers.download.current) {
        throw new Error('대기열에서 반출된 전송이 없음');
      }

      const { fileUrl, fileName, savePath } = this.transfers.download.current;

      const onProgress = (
        progress: number,
        downloadedBytes: number,
        speed: number
      ) => {
        if (!this.transfers.download.current) return;

        console.log(progress, downloadedBytes, speed);

        this.transfers.download.current = {
          ...this.transfers.download.current,
          progress,
          downloadedBytes,
          speed
        }
      }

      const path = savePath + '/' + fileName;

      this.transfers.download = {
        ...this.transfers.download,
        current: {
          ...this.transfers.download.current,
          status: 'downloading'
        },
        isProcessing: true
      }

      // 다운 시작
      await this.download.downloadFileWithProgress(fileUrl, path, onProgress);

      await this.download.completeCurrentDownload();
    },
  }
}

export const NTS = new NahidaTransferService();