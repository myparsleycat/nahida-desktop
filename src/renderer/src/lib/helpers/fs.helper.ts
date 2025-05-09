// src/renderer/src/lib/helpers/fs.helper.ts

import type { OpenDialogOptions } from "electron";
import type { ReadDirectoryOptions } from "../../../../types/fs.types";

interface FolderEventData {
  type: 'added' | 'renamed' | 'removed';
  path?: string;
  oldPath?: string;
  newPath?: string;
}

class FileSystemHelper {
  async select(opt: OpenDialogOptions) {
    return window.api.fss.select(opt);
  }

  async readDir(path: string, options: ReadDirectoryOptions) {
    return window.api.fss.readDir(path, options);
  }

  async readFile(path: string) {
    return window.api.fss.readFile(path);
  }

  async saveFile(path: string, data: ArrayBuffer) {
    return window.api.fss.saveFile(path, data);
  }

  async getStat(path: string) {
    return window.api.fss.getStat(path);
  }

  async openPath(path: string) {
    return window.api.fss.openPath(path);
  }

  async deletePath(path: string) {
    return window.api.fss.deletePath(path);
  }

  async watchFolder(path: string, options: { recursive?: boolean, depth?: number } = {}) {
    return window.api.fss.watchFolder(path, options);
  }

  async unwatchFolder(path: string) {
    return window.api.fss.unwatchFolder(path);
  }

  async getWatchedFolders() {
    return window.api.fss.getWatchedFolders();
  }

  onFolderEvent(callback: (data: FolderEventData) => void) {
    return window.api.fss.folderEvent(callback);
  }
}

const FSH = new FileSystemHelper();
export { FSH };