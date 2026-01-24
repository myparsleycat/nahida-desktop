import { MessageBoxOptions, OpenExternalOptions } from "electron";
import { Session } from "./schemas/auth";
import { ShowModalReturnValue } from "@main/services/util";
import { eden } from "@main/client";
import { Treaty } from "@elysiajs/eden";
import { desktop } from "@main/index";

interface ToastData {
    description?: string;
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
    name: string;
    path: string;
    isEnabled: boolean;
    toggleKeys: ToggleKey[];
    preview?: string;
    inis: {
        name: string;
        path: string;
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
    mods: string[];
}

export type IpcHandlers = {
    ping: () => string;

    "setting:general:getRunOnStartup": () => boolean;
    "setting:general:setRunOnStartup": (enabled: boolean) => void;
    "setting:general:getMoveTransferPageWhenStartTransfer": () => boolean;
    "setting:general:setMoveTransferPageWhenStartTransfer": (enabled: boolean) => void;
    "setting:general:getPowerSaveBlockInTransfer": () => boolean;
    "setting:general:setPowerSaveBlockInTransfer": (enabled: boolean) => void;
    "setting:general:getDefaultStartPage": () => string | null;
    "setting:general:setDefaultStartPage": (page: string | null) => void;
    "setting:general:checkUpdate": () => void;
    "setting:general:getCheckBackgroundUpdates": () => boolean;
    "setting:general:setCheckBackgroundUpdates": (enabled: boolean) => void;

    "setting:mod:getDeleteArchiveAfterExtract": () => boolean;
    "setting:mod:setDeleteArchiveAfterExtract": (enabled: boolean) => void;
    "setting:mod:getMoveFolderInsteadOfCopy": () => boolean;
    "setting:mod:setMoveFolderInsteadOfCopy": (enabled: boolean) => void;

    "window:closeWindow": (window: string) => void;
    "window:openReport": () => void;
    "window:openSetting": () => void;

    "auth:isLoggedIn": () => boolean;
    "auth:startLogin": () => void;
    "auth:startLogout": () => void;
    "auth:getSession": () => Session | null;

    "util:showModal": (opt: MessageBoxOptions) => ShowModalReturnValue;
    "util:openExternal": (url: string, opt?: OpenExternalOptions) => void;
    "util:copyStr": (str: string) => void;
    "util:get:language": () => "en" | "ko" | "zh";
    "util:openPath": (path: string) => void;
    "util:fs:trash": (path: string) => void;
    "util:openCmd": (path: string) => void;
    "util:getClipboardFiles": () => string[];

    "drive:get:item": (itemId: string) => ModIdGetResp;
    "drive:patch:rename": (
        itemId: string,
        name: string,
    ) => ReturnType<typeof desktop.service.drive.patch.rename>;
    "drive:post:dir": (
        parentId: string,
        name: string,
    ) => ReturnType<typeof desktop.service.drive.post.dir>;
    "drive:delete:items": (ids: string[]) => void;
    "drive:fn:startDownload": (id: string) => void;
    "drive:fn:startUpload": (destId: string, paths: string[]) => void;

    "transfer:list": () => TransferWithoutData[];
    "transfer:cancel": (pid: string) => void;
    "transfer:pause": (pid: string) => void;
    "transfer:resume": (pid: string) => void;
    "transfer:retry": (pid: string) => void;
    "transfer:pause-all": () => void;
    "transfer:resume-all": () => void;
    "transfer:clear": () => void;

    "mod:selectFolder": (game: string) => string | null;
    "mod:getGamePath": (game: string) => string | null;
    "mod:getGames": () => GameConfig[];
    "mod:addGame": (game: string, path: string) => void;
    "mod:removeGame": (game: string) => void;
    "mod:pickFolder": () => string | null;

    "mod:getCharacters": (game: string) => FolderGroup[];
    "mod:getMods": (groupPath: string) => FolderGroup;
    "mod:toggle": (modPath: string) => string;
    "mod:updateToggleKey": (
        modPath: string,
        iniFileName: string,
        sectionName: string,
        variable: string,
        value: string,
    ) => void;
    "mod:getPresets": (game: string) => Preset[];
    "mod:createPreset": (game: string, name: string) => Preset;
    "mod:applyPreset": (presetId: string) => void;
    "mod:deletePreset": (presetId: string) => void;
    "mod:updatePresetName": (presetId: string, newName: string) => void;
    "mod:getLastGame": () => string | null;
    "mod:setLastGame": (game: string) => void;
    "mod:extractArchive": (archivePath: string, groupPath: string) => void;
    "mod:copyFolder": (folderPath: string, groupPath: string) => void;
    "mod:enableAll": (groupPath: string) => void;
    "mod:disableAll": (groupPath: string) => void;
    "mod:pastePreview": (modPath: string, data: string, type: "url" | "base64" | "path") => void;
    "mod:watchGame": (game: string) => void;
    "mod:watchCharacter": (characterPath: string) => void;

    "pathSelector:selectFolderPath": (selectionId: string) => void;
    "pathSelector:selectModManagerPath": (
        selectionId: string,
        path: string,
        fileName?: string,
    ) => void;
    "pathSelector:cancel": (selectionId: string) => void;

    "logger:log": (
        level: "info" | "debug" | "warn" | "error" | "trace" | "fatal",
        object: unknown,
        where?: string,
    ) => void;
};

export interface GameConfig {
    game: string;
    modFolderPath: string;
}

export type IpcEvents = {
    "window:blur": () => void;
    "window:focus": () => void;

    "transfer:update": (transfers: Transfer[]) => void;

    "fn:toast": (message: string, data?: ToastData) => void;
    "fn:navi": (path: string) => void;
    "download:completed": (data: { path: string; name: string; disableToast?: boolean }) => void;
    "pathSelector:modeSelect": (data: { selectionId: string; suggestedName?: string }) => void;

    "mod:update-game": () => void;
    "mod:update-mods": () => void;
};

const akashaModIdGet = eden.akasha.content({ id: "" }).get;
export type ModIdGetResp = Treaty.Data<typeof akashaModIdGet>;
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
            // stream: string;
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
    status: TransferStatus;
    totalSize: number;
    transferedSize: number;
    progress: number;
    speed: number; // bytes per second
    eta: number; // seconds
    startTime: number;
    data: TransferData;
    name: string;
    totalFiles: number;
    transferedFiles: number;
    path?: string;
}
export type TransferWithoutData = Omit<Transfer, "data">;
