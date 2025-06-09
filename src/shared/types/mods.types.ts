import type { IniParseResult } from '@core/lib/InIUtil';

export interface DirectChildren {
  path: string;
  name: string;
  ini: {
    path: string;
    data: IniParseResult[]
  } | null;
  preview: {
    path: string;
    base64: string | null;
  } | null
}

export interface getDirectChildrenOptions {
  recursive?: number;
  dirOnly?: boolean;
}

export type Games = 'genshin' | 'starrail' | 'zzz';