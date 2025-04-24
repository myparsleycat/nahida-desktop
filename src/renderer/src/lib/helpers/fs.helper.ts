// src/renderer/src/lib/helpers/fs.helper.ts

import type { ReadDirectoryOptions } from "../../../../types/fs.types";

class FileSystemHelper {
  async readDir(path: string, options: ReadDirectoryOptions) {
    return window.api.fss.readDir(path, options);
  }

  async readFile(path: string) {
    return window.api.fss.readFile(path);
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
}

const FSH = new FileSystemHelper();
export { FSH };