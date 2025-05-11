// src/types/drive.types.ts
export type SuccessResponse = {
  success: true;
};

export type ErrorResponse = {
  success: false;
  error: {
    code: string;
    message: string;
  };
};

export type BaseResponse = SuccessResponse | ErrorResponse;

export type GetContentsLinkType = {
  id: string;
  password: boolean;
  expiresAt: Date | null;
  url: string;
};

export type ContentType = {
  id: string;
  name: string;
  isDir: boolean;
  size: number | null;
  mimeType: string | null;
  parentId: string | null;
  createdAt: Date;
  updatedAt: Date;
  link?: GetContentsLinkType | null;
};

export type ParentType = {
  id: string;
  name: string;
};

export type AncestorType = {
  id: string;
  parentId: string | null;
  name: string;
  depth: number;
};

export type ChildType = ContentType & {
  preview?: {
    img: {
      default: string;
      cover: string | null;
      thumbnail: string | null;
    };
    compAlg: string | null;
  };
};

// 성공 시 응답
export type GetContentsSuccessResp = SuccessResponse & {
  content: ContentType | null;
  parent: ParentType | null;
  ancestors: AncestorType[];
  children: ChildType[];
};

// 실패 시 응답
export type GetContentsErrorResp = ErrorResponse;

// 통합 응답 타입
export type GetContentsResp = GetContentsSuccessResp | GetContentsErrorResp;

// 이름 변경 응답
export type RenameSuccessResp = SuccessResponse & {
  rename: {
    before: string;
    after: string;
  };
};

export type RenameErrorResp = ErrorResponse;

export type RenameResp = RenameSuccessResp | RenameErrorResp;

// 디렉토리 생성 응답
export type DirCreateManySuccessResp = SuccessResponse & {
  parent: {
    uuid: string;
    name: string;
  } | null;
  directories: {
    uuid: string;
    path: string;
    name: string;
  }[];
};

export type DirCreateManyErrorResp = ErrorResponse;

export type DirCreateManyResp = DirCreateManySuccessResp | DirCreateManyErrorResp;

// 휴지통 응답
export type TrashManySuccessResp = SuccessResponse & {
  trash_many: string[];
};

export type TrashManyErrorResp = ErrorResponse;

export type TrashManyResp = TrashManySuccessResp | TrashManyErrorResp;