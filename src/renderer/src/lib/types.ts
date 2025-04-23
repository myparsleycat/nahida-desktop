export type Content = {
  id: string;
  name: string;
  isDir: boolean;
  size: number | null;
  mimeType: string | null;
  parentId: string | null;
  createdAt: Date;
  updatedAt: Date;
  preview?: {
    img: {
      default: string;
      cover: string | null
      thumbnail: string | null;
    };
    compAlg: string | null;
  } | null;
  link?: {
    id: string;
    password: boolean;
    expiresAt: Date | null;
    url: string;
  } | null;
  cachedSrc?: string;
}

export type SortType = | "NAME:DESC" | "NAME:ASC" | "SIZE:DESC" | "SIZE:ASC" | "DATE:DESC" | "DATE:ASC";

// export type QueuedUpload = {
//   files: FileInfoWorker[];
//   directories: DirectoryInfo[];
//   processId: string;
// }

export type LayoutType = "grid" | "list";