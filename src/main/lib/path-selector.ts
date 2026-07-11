import path from "node:path";

import type { DownloadSource } from "@shared/mod";
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
    suggestedNames?: string[];
    downloadTargetName?: string;
    downloadImporterKey?: string;
    downloadSource: DownloadSource;
    selectFile: boolean;
    resolve: (result: PathSelectorResult) => void;
    reject: (error: Error) => void;
}

export class PathSelector {
    private desktop: NahidaDesktop;
    private pendingSelections: Map<string, PendingPathSelection> = new Map();

    public constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async getSelectedPathWithModeModal(
        suggestedName?: string,
        downloadTargetName?: string,
        downloadImporterKey?: string,
        downloadSource: DownloadSource = "nahidaLive",
        suggestedNames?: string[],
        selectFile = false,
    ): Promise<PathSelectorResult> {
        return new Promise((resolve, reject) => {
            const selectionId = nanoid();

            this.pendingSelections.set(selectionId, {
                id: selectionId,
                suggestedName,
                suggestedNames,
                downloadTargetName,
                downloadImporterKey,
                downloadSource,
                selectFile,
                resolve,
                reject,
            });

            const mainWindow = this.desktop.window.main.window;
            if (!mainWindow) {
                void this.desktop.window.main.createMainWindow().then((window) => {
                    if (window?.webContents.isLoading()) {
                        window.webContents.once("did-finish-load", () => {
                            this.desktop.window.main.focus();
                            setTimeout(() => {
                                this.showSelectionModal(
                                    selectionId,
                                    suggestedName,
                                    downloadTargetName,
                                    downloadImporterKey,
                                    downloadSource,
                                    suggestedNames,
                                );
                            }, 500);
                        });
                    } else {
                        this.desktop.window.main.focus();
                        this.showSelectionModal(
                            selectionId,
                            suggestedName,
                            downloadTargetName,
                            downloadImporterKey,
                            downloadSource,
                            suggestedNames,
                        );
                    }
                });
            } else {
                this.desktop.window.main.focus();
                this.showSelectionModal(
                    selectionId,
                    suggestedName,
                    downloadTargetName,
                    downloadImporterKey,
                    downloadSource,
                    suggestedNames,
                );
            }
        });
    }

    private showSelectionModal(
        selectionId: string,
        suggestedName?: string,
        downloadTargetName?: string,
        downloadImporterKey?: string,
        downloadSource: DownloadSource = "nahidaLive",
        suggestedNames?: string[],
    ) {
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
            suggestedNames,
            downloadTargetName,
            downloadImporterKey,
            downloadSource,
        });
    }

    public async selectFolderPath(selectionId: string): Promise<void> {
        const pending = this.pendingSelections.get(selectionId);
        if (!pending) {
            throw new Error("Pending selection not found");
        }

        try {
            if (pending.selectFile) {
                const result = await this.selectFileDialog(pending.suggestedName);
                pending.resolve({
                    mode: "folder",
                    path: result ? path.dirname(result) : null,
                    fileName: result ? path.basename(result) : undefined,
                });
                return;
            }

            const selectedPath = await this.selectFolderDialog();
            pending.resolve({ mode: "folder", path: selectedPath });
        } catch (error) {
            pending.reject(error instanceof Error ? error : new Error(String(error)));
            throw error;
        } finally {
            this.pendingSelections.delete(selectionId);
        }
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
        const isWritable = await this.desktop.lib.fs.isPathWritable(savePath);
        if (!isWritable) {
            throw new Error("Path is not writable");
        }

        return savePath;
    }

    private async selectFileDialog(suggestedName?: string): Promise<string | null> {
        const window = this.desktop.window.main.window;
        if (!window) throw new Error("Main window not found");

        const dialogResult = await dialog.showSaveDialog(window, {
            defaultPath: suggestedName,
        });
        if (dialogResult.canceled || !dialogResult.filePath) return null;

        const isWritable = await this.desktop.lib.fs.isPathWritable(
            path.dirname(dialogResult.filePath),
        );
        if (!isWritable) throw new Error("Path is not writable");
        return dialogResult.filePath;
    }
}
