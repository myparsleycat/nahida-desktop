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