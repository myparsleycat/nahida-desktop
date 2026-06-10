import { spawn } from "node:child_process";
import fsp from "node:fs/promises";
import path from "node:path";
import isDev from "@main/internal/isDev";
import type { AppStatus, PathMetadata } from "@shared/types";
import {
    BrowserWindow,
    clipboard,
    dialog,
    type MessageBoxOptions,
    type OpenDialogOptions,
    type OpenExternalOptions,
    shell,
} from "electron";
import { app } from "electron/main";
import { trim } from "es-toolkit";
import fse from "fs-extra";
import { desktop } from "..";

export function getAppStatus(): AppStatus {
    return {
        version: app.getVersion(),
        isPackaged: app.isPackaged,
        isDev: isDev,
        platform: "win32",
    };
}

export type ShowModalReturnValue = ReturnType<typeof dialog.showMessageBox>;

export async function showModal(options: MessageBoxOptions) {
    return dialog.showMessageBox({
        type: options.type,
        title: options.title,
        message: options.message,
    });
}

export async function openExternal(str: string, opt?: OpenExternalOptions) {
    try {
        try {
            const parsedUrl = new URL(str);
            await shell.openExternal(parsedUrl.toString(), opt);
        } catch {
            await shell.openPath(str);
        }
    } catch (error) {
        desktop.logger.error(error, `util:openExternal`);
        throw error;
    }
}

export function closeAllWindows() {
    const windows = BrowserWindow.getAllWindows();
    windows.forEach((window) => {
        window.close();
    });
}

export function copyStr(str: string) {
    clipboard.writeText(str);
}

export function openPath(path: string) {
    shell.openPath(path);
}

export async function trash(path: string) {
    await shell.trashItem(path);
    return;
}

export async function mkdir(parentPath: string, name: string): Promise<string> {
    const trimmedName = trim(name);
    if (!trimmedName) {
        throw new Error("INVALID_GROUP_NAME");
    }

    desktop.lib.fs.assertValidWindowsFilename(trimmedName);

    const nextPath = path.join(parentPath, trimmedName);
    try {
        await fsp.mkdir(nextPath);
    } catch (error) {
        const code = (error as NodeJS.ErrnoException | undefined)?.code;
        const name = (error as NodeJS.ErrnoException | undefined)?.name;
        if (code === "EEXIST" || name === "AlreadyExists") {
            throw new Error(`ALREADY_EXISTS:${trimmedName}`);
        }
        throw error;
    }

    return nextPath;
}

export function openCmd(path: string) {
    spawn("cmd.exe", ["/c", "start", "cmd.exe"], {
        cwd: path,
        detached: true,
        stdio: "ignore",
    }).unref();
}

export function getClipboardFiles(): string[] {
    const buffer = clipboard.readBuffer("FileNameW");
    if (buffer && buffer.length > 0) {
        const path = buffer.toString("ucs2").replace(/\0+$/, "");
        if (path) return [path];
    }

    const text = clipboard.read("text/uri-list");
    if (text) {
        return text
            .split(/\r?\n/)
            .filter((line) => line.trim().startsWith("file://"))
            .map((line) => {
                const url = new URL(line.trim());
                let p = decodeURIComponent(url.pathname);
                if (p.startsWith("/")) {
                    p = p.slice(1);
                }
                return p;
            });
    }

    return [];
}

export async function getPathMetadata(path: string): Promise<PathMetadata> {
    const stat = await fse.stat(path);
    return {
        isDirectory: stat.isDirectory(),
        isFile: stat.isFile(),
        size: stat.size,
        mtime: stat.mtime,
        ctime: stat.ctime,
        birthtime: stat.birthtime,
    };
}

export async function showOpenDialog(options: OpenDialogOptions) {
    return dialog.showOpenDialog(options);
}

export async function processChunked<T>(
    items: T[],
    processor: (item: T) => void,
    size = 1000,
    signal?: AbortSignal,
) {
    const CHUNK_SIZE = size;
    for (let i = 0; i < items.length; i += CHUNK_SIZE) {
        if (signal?.aborted) return;
        const end = Math.min(i + CHUNK_SIZE, items.length);
        for (let j = i; j < end; j++) {
            processor(items[j]);
        }
        if (i + CHUNK_SIZE < items.length) {
            await new Promise((resolve) => setImmediate(resolve));
        }
    }
}


