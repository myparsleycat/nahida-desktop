import { spawn } from "child_process";
import {
    dialog,
    shell,
    type OpenExternalOptions,
    MessageBoxOptions,
    BrowserWindow,
    clipboard,
} from "electron";
import trs from "trash";

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
    await trs(path);
    return;
}

export function openCmd(path: string) {
    spawn("cmd.exe", ["/c", "start", "cmd.exe"], {
        cwd: path,
        detached: true,
        stdio: "ignore",
    }).unref();
}
