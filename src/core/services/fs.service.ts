import { BrowserWindow, dialog, shell } from "electron";
import fse, { type MakeDirectoryOptions } from 'fs-extra';
import path from 'node:path';
import type { ReadDirectoryOptions, FileInfo } from "@shared/types/fs.types";
import { bufferToArrayBuffer, bufferToBase64 } from "@core/utils";
import { fileTypeFromBuffer } from "file-type";
import _Watcher from 'watcher';
import { ToastService } from "./toast.service";
import type { ExploreOptions, FInfo } from "./fs.service.types";
import { calculateSHA256, getFileType } from "./fs.service.ut";
// @ts-ignore
const Watcher = _Watcher.default;

class FileSystemServiceClass {
    private folderWatchers: Map<string, _Watcher> = new Map();

    async select(opt: Electron.OpenDialogOptions) {
        const result = await dialog.showOpenDialog(opt);

        if (result.canceled || result.filePaths.length < 1) {
            return null
        }

        return result.filePaths[0];
    }

    async readFile<T extends "buf" | "arrbuf" | "utf8">(
        path: string,
        respType: T
    ): Promise<
        T extends "buf" ? Buffer :
        T extends "arrbuf" ? ArrayBuffer :
        T extends "utf8" ? string :
        never
    > {
        const buf = await fse.readFile(path);
        switch (respType) {
            case "buf":
                return buf as any;
            case "arrbuf":
                return bufferToArrayBuffer(buf) as any;
            case "utf8":
                return buf.toString('utf8') as any;
            default:
                throw new Error("invalid respType");
        }
    }

    async writeFile(path: string, data: any, options?: fse.WriteFileOptions | BufferEncoding | string) {
        try {
            await fse.writeFile(path, data, options);
            return true;
        } catch (e: any) {
            ToastService.error('파일 저장중 오류 발생', {
                description: e.message
            });
            console.error('writeFile Error', e);
            return false;
        }
    }

    async mkdir(path: string, options?: MakeDirectoryOptions) {
        return await fse.mkdir(path, options);
    }

    async getStat(path: string) {
        return await fse.stat(path);
    }

    async exists(path: string) {
        return await fse.pathExists(path);
    }

    async openPath(path: string) {
        shell.openPath(path);
    }

    async readDirectory(dirPath: string, options: ReadDirectoryOptions = {}, currentDepth: number = 0): Promise<FileInfo[]> {
        if (!dirPath) throw new Error("dirPath 는 비어있을 수 없음");

        const { recursive = false, fileFilter } = options;

        try {
            const stats = await fse.stat(dirPath);
            if (!stats.isDirectory()) {
                throw new Error(`${dirPath}는 폴더가 아님`);
            }

            const dirents = await fse.readdir(dirPath, { withFileTypes: true });
            const result: FileInfo[] = [];

            const shouldRecurse = typeof recursive === 'boolean'
                ? recursive
                : (currentDepth < recursive);

            const recursivePromises: Promise<void>[] = [];

            for (const dirent of dirents) {
                if (fileFilter && !fileFilter(dirent.name)) {
                    continue;
                }

                const filePath = path.join(dirPath, dirent.name);
                const isDirectory = dirent.isDirectory();

                const fileInfo: FileInfo = {
                    path: filePath,
                    name: dirent.name,
                    isDirectory
                };

                result.push(fileInfo);

                if (shouldRecurse && isDirectory) {
                    recursivePromises.push(
                        (async () => {
                            fileInfo.children = await this.readDirectory(filePath, options, currentDepth + 1);
                        })()
                    );
                }
            }

            if (recursivePromises.length > 0) {
                await Promise.all(recursivePromises);
            }

            return result;
        } catch (error) {
            console.error(`readDirectory (${dirPath}):`, error);
            throw error;
        }
    }
    async listAllFiles(dirPath: string, options: ReadDirectoryOptions = {}): Promise<string[]> {
        const results: string[] = [];
        const entries = await this.readDirectory(dirPath, options);

        const extractFiles = (items: FileInfo[]) => {
            for (const item of items) {
                if (!item.isDirectory) {
                    results.push(item.path);
                }
                if (item.children) {
                    extractFiles(item.children);
                }
            }
        };

        extractFiles(entries);
        return results;
    }

    async copy(source: string, destination: string) {
        await fse.copy(source, path.join(destination, path.basename(source)));
    }

    async rename(path: string, newPath: string) {
        await fse.rename(path, newPath);
    }

    async deletePath(path: string) {
        let isDirectory = false;

        try {
            const stat = await this.getStat(path);

            if (stat.isDirectory()) {
                isDirectory = true;
                await fse.rm(path, { recursive: true, force: true });
            } else {
                await fse.unlink(path);
            }

            return true;
        } catch (e: any) {
            ToastService.error(`${isDirectory ? '폴더' : '파일'} 삭제중 오류가 발생했습니다`, {
                description: e.message
            });
            return false;
        }
    }

    async generateUniqueFileName(basePath: string, fileName: string) {
        let uniquePath = path.join(basePath, fileName);
        let counter = 1;

        while (true) {
            try {
                await fse.access(uniquePath);
                counter++;
                const ext = path.extname(fileName);
                const nameWithoutExt = path.basename(fileName, ext);
                const newFileName = ext ? `${nameWithoutExt} (${counter})${ext}` : `${fileName} (${counter})`;
                uniquePath = path.join(basePath, newFileName);
            } catch (error) {
                break;
            }
        }

        return uniquePath;
    }

    async exploreFolderStructure(
        dirPath: string,
        options: ExploreOptions = {}
    ) {
        const { recursive = true, hash = false, mime = true } = options;

        try {
            const items = await fse.readdir(dirPath);
            const result: { [key: string]: FInfo } = {};

            for (const item of items) {
                const itemPath = path.join(dirPath, item);
                const stats = await fse.stat(itemPath);

                const fileInfo: FInfo = {
                    name: item,
                    isDir: stats.isDirectory(),
                    path: itemPath,
                };

                if (stats.isFile()) {
                    fileInfo.size = stats.size;

                    if (hash) {
                        fileInfo.hash = await calculateSHA256(itemPath);
                    }

                    if (mime) {
                        const mimeInfo = await getFileType(itemPath);
                        if (mimeInfo) {
                            fileInfo.mimeType = mimeInfo.mime;
                            fileInfo.ext = mimeInfo.ext;
                        }
                    }
                }

                else if (stats.isDirectory() && recursive) {
                    fileInfo.children = await this.exploreFolderStructure(itemPath, options);
                }

                result[item] = fileInfo;
            }

            return result;
        } catch (err: any) {
            throw new Error(`폴더 탐색 중 오류 발생: ${err}`);
        }
    }

    watchFolderChanges(folderPath: string, options: {
        recursive?: boolean,
        depth?: number
    } = {}): boolean {
        try {
            if (this.folderWatchers.has(folderPath)) {
                return false;
            }

            if (!fse.existsSync(folderPath)) {
                throw new Error(`폴더가 존재하지 않음: ${folderPath}`);
            }

            const watcherOptions = {
                recursive: options.recursive ?? true,
                renameDetection: true,
                ignoreInitial: true,
                depth: options.depth ?? 20,
            };

            const watcher = new Watcher(folderPath, watcherOptions) as _Watcher;

            watcher.on('addDir', (dirPath: string) => {
                const windows = BrowserWindow.getAllWindows();
                for (const window of windows) {
                    if (!window.isDestroyed()) {
                        window.webContents.send('fss-folder-event', {
                            type: 'added',
                            path: dirPath
                        });
                    }
                }
            });

            watcher.on('renameDir', (oldPath: string, newPath: string) => {
                const windows = BrowserWindow.getAllWindows();
                for (const window of windows) {
                    if (!window.isDestroyed()) {
                        window.webContents.send('fss-folder-event', {
                            type: 'renamed',
                            oldPath: oldPath,
                            newPath: newPath
                        });
                    }
                }
            });

            watcher.on('unlinkDir', (dirPath: string) => {
                const windows = BrowserWindow.getAllWindows();
                for (const window of windows) {
                    if (!window.isDestroyed()) {
                        window.webContents.send('fss-folder-event', {
                            type: 'removed',
                            path: dirPath
                        });
                    }
                }
            });

            this.folderWatchers.set(folderPath, watcher);
            return true;
        } catch (error) {
            console.error('폴더 감시 설정 오류:', error);
            return false;
        }
    }

    unwatchFolder(folderPath: string): boolean {
        const watcher = this.folderWatchers.get(folderPath);
        if (watcher) {
            watcher.close();
            this.folderWatchers.delete(folderPath);
            return true;
        }
        return false;
    }

    unwatchAllFolders(): void {
        for (const [path, watcher] of this.folderWatchers.entries()) {
            watcher.close();
            this.folderWatchers.delete(path);
        }
    }

    getWatchedFolders(): string[] {
        return Array.from(this.folderWatchers.keys());
    }
}

const FSService = new FileSystemServiceClass();
export { FSService };