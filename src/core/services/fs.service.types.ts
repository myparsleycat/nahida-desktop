export interface ExploreOptions {
    recursive?: boolean;
    hash?: boolean;
    mime?: boolean;
}

export interface FInfo {
    name: string;
    isDir: boolean;
    path: string;
    size?: number;
    hash?: string;
    mimeType?: string;
    ext?: string;
    children?: { [key: string]: FInfo };
}