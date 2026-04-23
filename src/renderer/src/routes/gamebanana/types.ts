import type {
  GameBananaGameKey,
  useGameBananaGameSubfeed,
  useGameBananaModCategoryOverview,
  useGameBananaModOverview,
} from "@renderer/hooks/use-gamebanana-data";

export interface GameOption {
  key: GameBananaGameKey;
  id: number;
}

export interface GameBananaBreadcrumbItem {
  id: number;
  name: string;
}

export interface PreviewImage {
  _sUrl?: string;
  _sFile?: string;
  _sFile530?: string;
  _sFile800?: string;
  _sFile220?: string;
  _sFile100?: string;
  _sBaseUrl?: string;
  _sCaption?: string;
}

export interface PreviewMedia {
  _aImages?: PreviewImage[];
}

export interface SubmissionListItem {
  _idRow: number;
  _sName: string;
  _sDescription?: string;
  _aPreviewMedia?: PreviewMedia;
  _tsDateAdded?: number;
  _tsDateModified?: number;
  _tsDateUpdated?: number;
  _nLikeCount?: number;
  _nPostCount?: number;
  _nViewCount?: number;
  _aSubmitter: {
    _sName: string;
  };
  _aRootCategory?: {
    _sName: string;
  };
  _aSubCategory?: {
    _sName: string;
  };
}

export interface RootCategoryItem {
  _idRow?: number;
  _sName: string;
  _nItemCount?: number;
}

export interface CategoryChildItem {
  _idRow: number;
  _sName: string;
}

export interface ModFileItem {
  _idRow: number;
  _sFile: string;
  _sDownloadUrl: string;
  _nDownloadCount: number;
  _tsDateAdded: number;
}

export type GameSubfeedQuery = ReturnType<typeof useGameBananaGameSubfeed>;
export type CategoryOverviewQuery = ReturnType<typeof useGameBananaModCategoryOverview>;
export type ModOverviewQuery = ReturnType<typeof useGameBananaModOverview>;
