import { dialog } from "electron";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";

export type PathSelectorMode = "folder" | "modManager";

export interface PathSelectorResult {
    mode: PathSelectorMode;
    path: string | null;
    fileName?: string;
}

export interface PendingPathSelection {
    id: string;
    suggestedName?: string;
    resolve: (result: PathSelectorResult) => void;
    reject: (error: Error) => void;
}

export class PathSelector {
    private desktop: NahidaDesktop;
    private pendingSelections: Map<string, PendingPathSelection> = new Map();

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async getSelectedPathWithModeModal(suggestedName?: string): Promise<PathSelectorResult> {
        return new Promise((resolve, reject) => {
            const selectionId = nanoid();

            this.pendingSelections.set(selectionId, {
                id: selectionId,
                suggestedName,
                resolve,
                reject,
            });

            const mainWindow = this.desktop.window.main.window;
            if (!mainWindow) {
                this.desktop.window.main.createMainWindow().then((window) => {
                    if (window?.webContents.isLoading()) {
                        window.webContents.once("did-finish-load", () => {
                            this.desktop.window.main.focus();
                            setTimeout(() => {
                                this.showSelectionModal(selectionId, suggestedName);
                            }, 500);
                        });
                    } else {
                        this.desktop.window.main.focus();
                        this.showSelectionModal(selectionId, suggestedName);
                    }
                });
            } else {
                this.desktop.window.main.focus();
                this.showSelectionModal(selectionId, suggestedName);
            }
        });
    }

    private showSelectionModal(selectionId: string, suggestedName?: string) {
        const mainWindow = this.desktop.window.main.window;
        if (!mainWindow) {
            const pending = this.pendingSelections.get(selectionId);
            if (pending) {
                pending.reject(new Error("Main window not found"));
                this.pendingSelections.delete(selectionId);
            }
            return;
        }

        this.desktop.ipc.postMessageToWindow(mainWindow, "pathSelector:modeSelect", {
            selectionId,
            suggestedName,
        });
    }

    public async selectFolderPath(selectionId: string): Promise<void> {
        const pending = this.pendingSelections.get(selectionId);
        if (!pending) {
            throw new Error("Pending selection not found");
        }

        const path = await this.selectFolderDialog();

        if (path) {
            pending.resolve({ mode: "folder", path });
        } else {
            pending.resolve({ mode: "folder", path: null });
        }

        this.pendingSelections.delete(selectionId);
    }

    public async selectModManagerPath(
        selectionId: string,
        path: string,
        fileName?: string,
    ): Promise<void> {
        const pending = this.pendingSelections.get(selectionId);
        if (!pending) {
            throw new Error("Pending selection not found");
        }

        pending.resolve({ mode: "modManager", path, fileName });
        this.pendingSelections.delete(selectionId);
    }

    public cancelSelection(selectionId: string): void {
        const pending = this.pendingSelections.get(selectionId);
        if (!pending) {
            return;
        }

        pending.resolve({ mode: "folder", path: null });
        this.pendingSelections.delete(selectionId);
    }

    private async selectFolderDialog(): Promise<string | null> {
        const window = this.desktop.window.main.window;
        if (!window) {
            throw new Error("Main window not found");
        }

        const dialogResult = await dialog.showOpenDialog(window, {
            properties: ["openDirectory"],
        });

        if (!dialogResult || dialogResult.canceled) {
            return null;
        }

        const savePath = dialogResult.filePaths[0];
        const isWritable = this.desktop.lib.fs.isPathWritable(savePath);
        if (!isWritable) {
            throw new Error("Path is not writable");
        }

        return savePath;
    }
}
