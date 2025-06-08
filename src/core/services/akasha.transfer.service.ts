import { nanoid } from "nanoid";
import PQueue from "p-queue";
import { ADS } from "./drive.service";
import { GetDirDownloadWithStream } from "./drive.service.ut";
import { fss } from "./fs.service";
import { createDirectoryStructure, createDirMap, removeDuplicateDirs } from "./fs.service.ut";
import { downloadFiles } from "./akasha.transfer.service.ut";

export type FileInfoComponent = {
  FID: string;
  path: string;
  name: string;
  size: number;
  parentPath: string;
  file: File;
}

export type FileInfoWorker = {
  FID: string;
  path: string;
  name: string;
  size: number;
  parentPath: string;
  parentId: string;
  file: File;
}

export type DirectoryInfo = {
  path: string;
  name: string;
  parentPath: string;
}

export type ProcessStatus = 'pending'
  | 'creating-directory'
  | 'hash-calculation'
  | 'uploading'
  | 'paused'
  | 'completed'
  | 'failed';

export interface FileProgress {
  path: string;
  name: string;
  size: number;
  status: 'pending' | 'uploading' | 'completed' | 'failed';
  uploadedBytes?: number;
  uploadSpeed?: number;
}

export interface CurrentProcess {
  pid: string;
  name: string;
  status: ProcessStatus;
  totalItems: number;
  processedItems: number;
  uploadedBytes: number;
  totalBytes: number;
  uploadBytesPerSec: number;
  files?: FileProgress[];
  size: number;
  directories?: {
    path: string;
    name: string;
    status: 'pending' | 'created' | 'failed';
  }[];
  error?: any;

  parentUUID?: string;
  rawFiles?: FileInfoComponent[];
  rawDirectories?: DirectoryInfo[];
}

export interface QueuedProcess {
  files: FileInfoComponent[];
  directories: DirectoryInfo[];
  pid: string;
  parentUUID?: string;
  name: string;
  size: number;
  totalItems: number;
  settings?: {
    removeDISABLED?: boolean;
  }
}

export interface CompleteProcess {
  pid: string;
  name: string;
  size: number;
}

type DownloadCurrentStatus = 'pending' | 'downloading' | 'completed';

interface dlinf {
  totalBytes: number;
  files: {
    uuid: string;
    fileId: string;
    parentId: string | null;
    name: string;
    size: number;
    compAlg: "gzip" | "zstd" | null;
    url: string;
  }[];
  dirs: {
    uuid: string;
    parentId: string | null;
    name: string;
  }[];
}

interface DownloadTask {
  PID: string;
  id: string;
  name: string;
  save_path: string;
  linkId?: string;
  password?: string;
  priority?: number;
}

interface DownloadProgress {
  PID: string;
  id: string;
  status: 'pending' | 'downloading' | 'completed' | 'failed';
  name: string;
  downloadedSize: number;
  totalSize: number;
  currentFile: number;
  totalFiles: number;
  progress: number;
  downloadBytesPerSec: number;
  abortController: AbortController;
  currentFileName?: string;
  phase?: 'downloading' | 'decompressing';
}

interface TransferStore {
  upload: {
    current: CurrentProcess | null;
    queue: QueuedProcess[];
    completed: CompleteProcess[];
    isProcessing: boolean;
  },
  download: {
    active: Map<string, DownloadProgress>;
    completed: CompleteProcess[];
  }
}

class TransferService {
  transfers: TransferStore = {
    upload: {
      current: null,
      queue: [],
      completed: [],
      isProcessing: false
    },
    download: {
      active: new Map(),
      completed: []
    }
  }

  private downloadQueue = new PQueue({ concurrency: 1 });

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

  akasha = {
    download: {
      enqueue: async (id: string, save_path: string, options?: {
        linkId?: string;
        password?: string;
        priority?: number;
      }) => {
        const item = await ADS.item.get(id);
        if (!item.content) {
          throw new Error('아이템 정보를 가져올 수 없음');
        }

        const PID = nanoid();
        const task: DownloadTask = {
          PID,
          id,
          name: item.content.name,
          save_path,
          linkId: options?.linkId,
          password: options?.password,
          priority: options?.priority || 0
        };

        return this.downloadQueue.add(
          () => this.akasha.download.processDownload(task),
          {
            priority: options?.priority || 0
          }
        );
      },

      processDownload: async (task: DownloadTask) => {
        const { PID, id, name, save_path, linkId, password } = task;

        try {
          const abortController = new AbortController();

          const progress: DownloadProgress = {
            PID,
            id,
            name,
            status: 'downloading',
            downloadedSize: 0,
            totalSize: 0,
            currentFile: 0,
            totalFiles: 0,
            progress: 0,
            downloadBytesPerSec: 0,
            abortController
          };

          this.transfers.download.active.set(PID, progress);

          const data = await GetDirDownloadWithStream({
            id,
            linkId,
            password
          });

          progress.totalSize = data.totalBytes;
          progress.totalFiles = data.files.length;

          await this.akasha.download.startDownload({
            id,
            name,
            save_path,
            linkId,
            password,
            download: data,
            abortSignal: abortController.signal,
            progress
          });

          progress.status = 'completed';
          progress.progress = 100;

          this.transfers.download.completed.push({
            pid: PID,
            name,
            size: data.totalBytes
          });

          this.transfers.download.active.delete(PID);

        } catch (error) {
          const progress = this.transfers.download.active.get(PID);
          if (progress) {
            progress.status = 'failed';
            this.transfers.download.active.delete(PID);
          }

          console.error(`다운로드 실패 [${name}]:`, error);
          throw error;
        }
      },

      startDownload: async (params: {
        id: string;
        name: string;
        save_path: string;
        linkId?: string;
        password?: string;
        download: any;
        abortSignal: AbortSignal;
        progress: DownloadProgress;
      }) => {
        const { id, name, save_path, download, progress, abortSignal } = params;

        const rootDirPath = `${save_path}/${name}`;
        await fss.mkdir(rootDirPath, { recursive: true });

        const uniqueDirs = removeDuplicateDirs(download.dirs);
        const dirMap = createDirMap(uniqueDirs);
        const dirPaths = await createDirectoryStructure(rootDirPath, dirMap, id);

        await downloadFiles(
          download.files,
          dirPaths,
          dirMap,
          id,
          {
            progressCallback: (progressData) => {
              progress.downloadedSize = progressData.downloadedBytes;
              progress.downloadBytesPerSec = progressData.downloadSpeed;
              progress.currentFile = progressData.downloadedFiles;
              progress.totalFiles = progressData.totalFiles;
              progress.progress = (progressData.downloadedBytes / progress.totalSize) * 100;

              if (progressData.currentFile) {
                progress.currentFileName = progressData.currentFile;
              }
            },
            abortSignal,
            concurrencyLimit: 50,
            retryAttempts: 3,
            timeout: 30000
          }
        );
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
    }
  }
}

const TFS = new TransferService();
export { TFS };