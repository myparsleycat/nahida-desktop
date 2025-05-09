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

export type DownloadCurrentStatus = 'pending' | 'downloading' | 'completed';
export type GamebananaCurrentStatus = 'pending' | 'pulling' | 'completed';