import { PathMetadata } from "@shared/types";
import { spawn } from "child_process";
import {
    dialog,
    shell,
    type OpenExternalOptions,
    MessageBoxOptions,
    BrowserWindow,
    clipboard,
} from "electron";
import fse from "fs-extra";

export type ShowModalReturnValue = ReturnType<typeof dialog.showMessageBox>;

export async function showModal(options: MessageBoxOptions) {
    return dialog.showMessageBox({
        type: options.type,
        title: options.title,
        message: options.message,
    });
}

export async function openExternal(url: string, opt?: OpenExternalOptions) {
    await shell.openExternal(url, opt);
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
