import { ErrorResponse, SuccessResponse } from "./fetcher.types";

export interface NahidaIPCHelloModsParams {
    size?: number;
    page?: number;
}

export type Mod = {
    uuid: string;
    version: string;
    password: boolean;
    game: 'genshin' | 'starrail' | 'zzz' | 'wuwa' | 'hk3rd';
    title: string;
    description: string | null;
    tags: string[];
    merged: boolean;
    preview_url: string;
    sha256: string;
    size: number;
    unzip_size: number;
    uploaded_at: number;
    expires_at: number | null;
    status: string;
}

export type HelloModsRespSuccessResp = SuccessResponse & {
    data: {
        ps: number;
        cp: number;
        tp: number;
        ti: number;
        st: number;
        r: Mod[];
    }
};
export type HelloModsRespErrorResp = ErrorResponse;
export type HelloModsResp = HelloModsRespSuccessResp | HelloModsRespErrorResp;