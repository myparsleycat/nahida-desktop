export interface ModFolders {
  id: string;
  path: string;
  name: string;
  parentId: string | null;
  createdAt: string;
  seq: number;
}

export interface ReadDirectoryOptions {
  recursive?: boolean | number;
  fileFilter?: (filename: string) => boolean;
}

export interface FileInfo {
  path: string;
  name: string;
  isDirectory: boolean;
  children?: FileInfo[];
}