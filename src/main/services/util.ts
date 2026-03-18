import { spawn } from "node:child_process";
import path from "node:path";
import { eden } from "@main/client";
import isDev from "@main/internal/isDev";
import { nahidaLogsPath } from "@main/internal/logger";
import type { AppStatus, PathMetadata } from "@shared/types.gen";
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
import fse from "fs-extra";
import { desktop } from "..";

export function getAppStatus(): AppStatus {
    return {
        version: app.getVersion(),
        isPackaged: app.isPackaged,
        isDev: isDev,
        platform: process.platform,
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
                if (process.platform === "win32" && p.startsWith("/")) {
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

export async function openReportWindow() {
    await desktop.window.report.focus();
}

export async function submitReport({
    title,
    description,
    submitLog,
}: {
    title?: string;
    description: string;
    submitLog: boolean;
}) {
    let log: File | undefined;
    if (submitLog) {
        const logFilePath = path.join(await nahidaLogsPath(), "desktop.log");
        if (await fse.pathExists(logFilePath)) {
            const buffer = await fse.readFile(logFilePath);
            // biome-ignore lint/suspicious/noExplicitAny: _
            log = new File([buffer as any], "desktop.log");
        }
    }

    const { data, error } = await eden.desktop["submit-report"].post({
        title,
        description,
        log,
    });

    if (error) {
        const errStr = error.value.toString();
        throw new Error(errStr);
    }

    return data;
}
