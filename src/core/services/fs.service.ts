import { dialog, shell } from "electron";
import fs from 'node:fs';
import path from 'node:path';
import type { ReadDirectoryOptions, FileInfo } from "../../types/fs.types";
import { bufferToArrayBuffer, bufferToBase64 } from "../utils";
import { fileTypeFromBuffer } from "file-type";

class FileSystemService {
  async select(opt: Electron.OpenDialogOptions) {
    const result = await dialog.showOpenDialog(opt);

    if (result.canceled || result.filePaths.length < 1) {
      return null
    }

    return result.filePaths[0];
  }

  async readFile<T extends "buf" | "arrbuf">(
    path: string,
    respType: T
  ): Promise<T extends "buf" ? Buffer : ArrayBuffer> {
    const buf = await fs.promises.readFile(path);
    switch (respType) {
      case "buf":
        return buf as any;
      case "arrbuf":
        return bufferToArrayBuffer(buf) as any;
      default:
        throw new Error("invalid respType");
    }
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
}

const fss = new FileSystemService();
export { fss };