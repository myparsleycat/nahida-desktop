import { nanoid } from "nanoid";
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
type GamebananaCurrentStatus = 'pending' | 'pulling' | 'completed';
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

interface TransferStore {
  upload: {
    current: CurrentProcess | null;
    queue: QueuedProcess[];
    completed: CompleteProcess[];
    isProcessing: boolean;
  },
  download: {
    current: {
      PID: string;
      id: string;
      status: DownloadCurrentStatus
      name: string;
      downloadedSize: number
      totalSize: number;
      currentFile: number;
      totalFiles: number;
      progress: number;
      downloadBytesPerSec: number;
      download: dlinf;
      abortController: AbortController;
    } | null;
    queue: {
      PID: string;
      id: string;
      name: string;
      save_path: string;
      linkId?: string;
      password?: string;
    }[];
    completed: {
      pid: string;
      name: string;
      size: number;
    }[];
    isProcessing: boolean;
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
      current: null,
      queue: [],
      completed: [],
      isProcessing: false
    }
  }

  akasha = {
    download: {
      enqueue: async (id: string, save_path: string) => {
        const item = await ADS.item.get(id);
        if (!item.content) {
          throw new Error('아이템 정보를 가져올 수 없음');
        }

        const PID = nanoid();
        this.transfers.download.queue.push({ PID, id, name: item.content.name, save_path });

        if (!this.transfers.download.current) {
          await this.akasha.download.processNextDownloadInQueue();
        }
      },

      processNextDownloadInQueue: async () => {
        const nextDownload = this.transfers.download.queue.shift();
        if (this.transfers.download.isProcessing || !nextDownload) {
          return;
        }

        const { PID, id, name, save_path, linkId, password } = nextDownload;

        const abortController = new AbortController();
        const data = await GetDirDownloadWithStream({
          id,
          linkId,
          password
        });

        this.transfers.download = {
          ...this.transfers.download,
          isProcessing: true,
          current: {
            PID,
            id,
            name,
            status: 'downloading',
            downloadedSize: 0,
            totalSize: data.totalBytes,
            currentFile: 0,
            totalFiles: 0,
            progress: 0,
            downloadBytesPerSec: 0,
            download: data,
            abortController
          }
        }

        await this.akasha.download.startDownload({
          id,
          name,
          save_path,
          linkId,
          password,
          download: data,
          abortSignal: abortController.signal
        });
      },

      completeCurrentDownload: async () => {
        this.transfers.download = {
          ...this.transfers.download,
          isProcessing: false,
          current: null
        }

        if (this.transfers.download.queue.length > 0) {
          await this.akasha.download.processNextDownloadInQueue();
        }
      },

      startDownload: async (params: {
        id: string;
        name: string;
        save_path: string;
        linkId?: string;
        password?: string;
        download: dlinf;
        abortSignal: AbortSignal;
      }) => {
        const { id, name, save_path, linkId, password, abortSignal } = params;
        let { download } = params;

        if (!download) {
          download = await GetDirDownloadWithStream({
            id,
            linkId,
            password
          });
        }

        const rootDirPath = `${save_path}/${name}`;
        await fss.mkdir(rootDirPath, { recursive: true });

        const uniqueDirs = removeDuplicateDirs(download.dirs);
        const dirMap = createDirMap(uniqueDirs);
        const dirPaths = await createDirectoryStructure(rootDirPath, dirMap, id);

        downloadFiles(download.files, dirPaths, dirMap, id)
          .then(async () => {
            await this.akasha.download.completeCurrentDownload();
          })
          .catch((e: any) => {
            throw e;
          })
      }
    },
  }
}

const TFS = new TransferService();
export { TFS };