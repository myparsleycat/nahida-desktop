import { BrowserWindow, dialog, shell } from "electron";
import fs from 'node:fs';
import path from 'node:path';
import type { ReadDirectoryOptions, FileInfo } from "../../types/fs.types";
import { bufferToArrayBuffer, bufferToBase64 } from "../utils";
import { fileTypeFromBuffer } from "file-type";
import _Watcher from 'watcher';
// @ts-ignore
const Watcher = _Watcher.default;

class FileSystemService {
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
    const buf = await fs.promises.readFile(path);
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

  async saveFile(path: string, data: ArrayBuffer) {
    const buffer = Buffer.from(data);
    await fs.promises.writeFile(path, buffer);
  }

  async getStat(path: string) {
    return await fs.promises.stat(path);
  }

  async openPath(path: string) {
    shell.openPath(path);
  }

  async getImgBase64(path: string) {
    const buf = await this.readFile(path, "buf");
    const [filetype, base64] = await Promise.all([
      fileTypeFromBuffer(buf),
      bufferToBase64(buf)
    ]);
    return `data:${filetype?.mime};base64,${base64}`;
  }

  async readDirectory(dirPath: string, options: ReadDirectoryOptions = {}, currentDepth: number = 0): Promise<FileInfo[]> {
    if (!dirPath) throw new Error("dirPath 는 비어있을 수 없음");

    const { recursive = false, fileFilter } = options;

    try {
      const stats = await fs.promises.stat(dirPath);
      if (!stats.isDirectory()) {
        throw new Error(`${dirPath}는 폴더가 아님`);
      }

      const dirents = await fs.promises.readdir(dirPath, { withFileTypes: true });
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
      console.error(`폴더 읽기 오류 (${dirPath}):`, error);
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

  async rename(path: string, newPath: string) {
    await fs.promises.rename(path, newPath);
  }

  async deletePath(path: string) {
    const stat = await this.getStat(path);

    if (stat.isDirectory()) {
      await fs.promises.rm(path, { recursive: true, force: true });
    } else {
      await fs.promises.unlink(path);
    }
  }
  // 폴더 변경 감지 시작
  watchFolderChanges(folderPath: string, options: {
    recursive?: boolean,
    depth?: number
  } = {}): boolean {
    try {
      // 이미 감시 중인 폴더인지 확인
      if (this.folderWatchers.has(folderPath)) {
        return false;
      }

      // 폴더가 존재하는지 확인
      if (!fs.existsSync(folderPath)) {
        throw new Error(`폴더가 존재하지 않음: ${folderPath}`);
      }

      const watcherOptions = {
        recursive: options.recursive ?? true,
        renameDetection: true,
        ignoreInitial: true,
        depth: options.depth ?? 20,
      };

      const watcher = new Watcher(folderPath, watcherOptions) as _Watcher;

      // 폴더 추가 감지
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

      // 폴더 이름 변경 감지
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

      // 폴더 삭제 감지
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

      // 감시 인스턴스 저장
      this.folderWatchers.set(folderPath, watcher);
      return true;
    } catch (error) {
      console.error('폴더 감시 설정 오류:', error);
      return false;
    }
  }

  // 폴더 감시 중지
  unwatchFolder(folderPath: string): boolean {
    const watcher = this.folderWatchers.get(folderPath);
    if (watcher) {
      watcher.close();
      this.folderWatchers.delete(folderPath);
      return true;
    }
    return false;
  }

  // 모든 폴더 감시 중지
  unwatchAllFolders(): void {
    for (const [path, watcher] of this.folderWatchers.entries()) {
      watcher.close();
      this.folderWatchers.delete(path);
    }
  }

  // 감시 중인 모든 폴더 경로 반환
  getWatchedFolders(): string[] {
    return Array.from(this.folderWatchers.keys());
  }
}

const fss = new FileSystemService();
export { fss };