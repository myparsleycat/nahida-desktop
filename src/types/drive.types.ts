// src/types/drive.types.ts
export interface BaseResponse {
  success: boolean;
  error?: {
    code: string;
    message: string;
  }
}

export interface GetContentsLinkType {
  id: string;
  password: boolean;
  expiresAt: Date | null;
  url: string;
}

export interface GetContentsResp extends BaseResponse {
  content: {
    id: string;
    name: string;
    isDir: boolean;
    size: number | null;
    mimeType: string | null;
    parentId: string | null;
    createdAt: Date;
    updatedAt: Date;
    link?: GetContentsLinkType | null
  } | null;
  parent: {
    id: string;
    name: string;
  } | null;
  ancestors: {
    id: string;
    parentId: string | null;
    name: string;
    depth: number;
  }[];
  children: {
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
        cover: string | null;
        thumbnail: string | null;
      };
      compAlg: string | null;
    };
    link?: GetContentsLinkType | null;
  }[]
}

export interface RenameResp extends BaseResponse {
  rename: {
    before: string;
    after: string;
  }
}

export interface DirCreateManyResp extends BaseResponse {
  parent: {
    uuid: string;
    name: string;
  } | null;
  directories: {
    uuid: string;
    path: string;
    name: string;
  }[]
}