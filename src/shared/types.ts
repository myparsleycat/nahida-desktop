// oxlint-disable typescript/no-explicit-any
import type { BackendStatus } from "./backend";
import type { Session } from "./schemas/auth";

export type { IpcHandlers } from "./types.gen";
export type { BackendStatus } from "./backend";

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

export interface GitHubRateState {
    limit: number;
    remaining: number;
    reset: number;
    used: number;
    resource: string;
    updatedAt: string;
}

export interface WuwaFixerOptions {
    derivedHashes: boolean;
    stableTexture: boolean;
    aemeathMech: boolean;
    aeroFix: "none" | "1" | "2";
    rollback: boolean;
}

export interface WuwaFixerStatus {
    supported: boolean;
    installed: boolean;
    installedVersion: string | null;
    latestVersion: string | null;
    binaryPath: string | null;
    updateAvailable: boolean;
    rateState: GitHubRateState | null;
    rateLimited: boolean;
    nextCheckAt: string | null;
}

export type TouchProfileProgressStage =
    | "scan"
    | "preview"
    | "vision"
    | "assets"
    | "ini"
    | "validate"
    | "complete";

export interface TouchProfileProgressEvent {
    sessionId?: string;
    stage: TouchProfileProgressStage;
    progress: number;
    message: string;
    componentId?: string;
}

export interface FourThousandOneFixerProgressEvent {
    task: "build-dll" | "diversify-dll" | "restore-dll" | null;
    code: string;
    errorMessage?: string;
}

export interface WuwaFixerPrepareResult extends WuwaFixerStatus {
    needsInstall: boolean;
    checkedRemotely: boolean;
}

export type TextureResizeMode = "percent" | "custom";
export type TextureResizeOperation = "resize" | "resize_and_convert" | "convert";
export type TextureColorSpace = "srgb" | "linear" | "unknown";

export interface TextureResizeSettings {
    mode: TextureResizeMode;
    operation: TextureResizeOperation;
    percent: number;
    customWidth: number;
    customHeight: number;
    outputFormat: string;
    backup: boolean;
}

export interface TextureResizeRunInput {
    targetPath: string;
    settings: TextureResizeSettings;
}

export interface TextureResizeListItem {
    filePath: string;
    relativePath: string;
    fileName: string;
    fileSize: number;
    format: string;
    colorSpace: TextureColorSpace;
    layerCount: number;
    mipLevelCount: number;
    originalWidth: number;
    originalHeight: number;
    targetWidth: number;
    targetHeight: number;
    canResize: boolean;
    canConvertFormat: boolean;
    canProcess: boolean;
    availableOutputFormats: string[];
    outputFormatDefault: string;
    formatConversionMessage?: string | null;
    message?: string | null;
}

export interface TextureResizeFileRunInput {
    filePath: string;
    settings: TextureResizeSettings;
}

export interface TextureResizeFileResult {
    filePath: string;
    status: "updated" | "skipped" | "failed";
    originalWidth: number;
    originalHeight: number;
    outputWidth: number;
    outputHeight: number;
    originalFormat: string;
    outputFormat: string;
    backupCreated: boolean;
    message?: string | null;
}

export interface TextureResizeResult {
    targetPath: string;
    processed: number;
    updated: number;
    skipped: number;
    failed: number;
    files: TextureResizeFileResult[];
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
    enabledModCount?: number;
    isManualSubGroup?: boolean;
    hasSubGroups?: boolean;
    hasManualSubGroups?: boolean;
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
    linkedModFolderPath: string | null;
    gameInstallPath: string | null;
    gameExecutablePath: string | null;
    nteLauncherPath: string | null;
    order: number;
}

export type NteBootstrapProgressPhase =
    | "checking"
    | "fetching-release"
    | "downloading"
    | "extracting"
    | "installing"
    | "completed"
    | "failed";

export interface NteBootstrapProgress {
    phase: NteBootstrapProgressPhase;
    archiveName?: string;
    progress: number | null;
    message?: string;
}

export type BisectStatus = "idle" | "scanning" | "round" | "done" | "reverting" | "cancelled";

export interface BisectSnapshot {
    status: BisectStatus;
    game: string;
    modRootPath: string | null;
    round: number;
    batchSize: number;
    candidates: string[];
    currentBatch: string[];
    undoStackDepth: number;
    finalBadPath: string | null;
    error: string | null;
}

export type IpcEvents = {
    "window:blur": () => void;
    "window:focus": () => void;

    "transfer:update": (transfers: TransferWithoutData[]) => void;

    "fn:toast": (message: string, data?: ToastData) => void;
    "fn:navi": (path: string) => void;
    "download:completed": (data: { path: string; name: string; disableToast?: boolean }) => void;
    "mod:archiveExtractPrompt": (data: { requestId: string; fileName: string }) => void;
    "pathSelector:modeSelect": (data: {
        selectionId: string;
        suggestedName?: string;
        suggestedNames?: string[];
        downloadTargetName?: string;
        downloadImporterKey?: string;
        downloadSource: import("./mod").DownloadSource;
    }) => void;
    "language:update": (language: string) => void;

    "mod:update-game": () => void;
    "mod:update-mods": () => void;
    "mod:update-settings": () => void;
    "mod:nte-bootstrap-progress": (payload: NteBootstrapProgress) => void;
    "drive:update-settings": () => void;

    "auth:update": (session: Session | null) => void;
    "backend:status": (status: BackendStatus) => void;
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
    "tools:4001FixerProgress": (event: FourThousandOneFixerProgressEvent) => void;
    "tools:touchProfileProgress": (event: TouchProfileProgressEvent) => void;
    "tools:bisectState": (snapshot: BisectSnapshot) => void;
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
    error?: string;
}

export type TransferWithoutData = Omit<Transfer, "data">;
