// oxlint-disable typescript/no-explicit-any
import type { Session } from "./schemas/auth";

export type { IpcHandlers } from "./types.gen";

export interface AppStatus {
    version: string;
    isPackaged: boolean;
    isDev: boolean;
    platform: NodeJS.Platform;
}

export interface ProxySettings {
    type: "disabled" | "https" | "socks5";
    host?: string;
    port?: string;
    requiresAuth?: boolean;
    username?: string;
    password?: string;
}

interface ToastData {
    description?: string;
}

export interface FixToolLogEvent {
    message: string;
    replaceLast?: boolean;
}

export interface ToggleKey {
    sectionName: string;
    iniFileName: string;
    key?: string;
    back?: string;
    type?: string;
    variable: string;
    values: string[];
    currentValue?: string;
}

export interface ModInfo {
    id: string;
    name: string;
    path: string;
    isEnabled: boolean;
    preview?: string;
    mtime: number;
    size: number;
    inis: {
        name: string;
        path: string;
        toggleKeys: ToggleKey[];
    }[];
}

export interface FolderGroup {
    name: string;
    path: string;
    mods: ModInfo[];
    preview?: string;
    modCount?: number;
}

export interface Preset {
    id: string;
    game: string;
    name: string;
    description: string | null;
    createdAt: string;
    updatedAt: string;
    version: number;
    isLegacy: boolean;
}

export interface ApplyPresetResult {
    presetId: string;
    applied: string[];
    skipped: string[];
    missing: {
        modKey: string;
        expectedFolderName: string;
        expectedRelativePath: string;
    }[];
}

export interface PresetCreateConflictCandidate {
    actualPath: string;
    relativePath: string;
    folderName: string;
    isEnabled: boolean;
}

export interface PresetCreateConflict {
    modKey: string;
    candidates: PresetCreateConflictCandidate[];
}

export interface PathMetadata {
    isDirectory: boolean;
    isFile: boolean;
    size: number;
    mtime: Date;
    ctime: Date;
    birthtime: Date;
}

export interface GameConfig {
    game: string;
    modFolderPath: string;
    importer: string | null;
}

export type IpcEvents = {
    "window:blur": () => void;
    "window:focus": () => void;

    "transfer:update": (transfers: TransferWithoutData[]) => void;

    "fn:toast": (message: string, data?: ToastData) => void;
    "fn:navi": (path: string) => void;
    "download:completed": (data: { path: string; name: string; disableToast?: boolean }) => void;
    "mod:archiveExtractPrompt": (data: { requestId: string; fileName: string }) => void;
    "pathSelector:modeSelect": (data: { selectionId: string; suggestedName?: string }) => void;
    "language:update": (language: string) => void;

    "mod:update-game": () => void;
    "mod:update-mods": () => void;
    "mod:update-settings": () => void;
    "drive:update-settings": () => void;

    "auth:update": (session: Session | null) => void;
    "setting:update": (data: { key: string; value: any }) => void;
    "compact:log": (message: string) => void;
    "compact:progress": (payload: {
        message: string;
        processedFiles: number;
        skippedFiles: number;
        errorFiles: number;
    }) => void;
    "setting:xxmi:persistLogs": (logs: string[]) => void;
    "setting:xxmi:toggleViewerLogs": (logs: string[]) => void;
    "tools:progress": (message: string) => void;
    "ftm:log": (event: FixToolLogEvent) => void;
    "updater:status-changed": (
        status: Awaited<
            ReturnType<(typeof import("@main/index"))["desktop"]["updater"]["getStatus"]>
        >,
    ) => void;
    "updater:update-available": () => void;
    "updater:update-downloaded": () => void;

    "renderer:reload": () => void;
};

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
        img?: {
            default: string;
            cover: string | null;
            thumbnail: string | null;
        };
        video?: {
            default: string;
        };
    } | null;
    link?: {
        id: string;
        password: boolean;
        expiresAt: Date | null;
        url: string;
    } | null;
};

export type TransferStatus =
    | "pending"
    | "preparing"
    | "progress"
    | "completed"
    | "paused"
    | "canceled"
    | "error";

export interface TransferData {
    root?: {
        id: string;
        parentId: string | null;
        name: string;
    };
    files: {
        id: string;
        fileId: string;
        parentId: string | null;
        name: string;
        size: number;
        compAlg: "gzip" | "zstd" | null;
        url: string;
    }[];
    dirs: {
        id: string;
        parentId: string | null;
        name: string;
    }[];
}

export interface Transfer {
    pid: string;
    type: "upload" | "download";
    queueGroupId?: number;
    currentId?: string;
    status: TransferStatus;
    totalSize: number;
    transferedSize: number;
    progress: number;
    speed: number;
    eta: number;
    startTime: number;
    data: TransferData;
    name: string;
    totalFiles: number;
    transferedFiles: number;
    failedFiles: number;
    path?: string;
}

export type TransferWithoutData = Omit<Transfer, "data">;
