import type { ReadDirectoryOptions } from "../../../../types/fs.types";

class FileSystemHelper {
  async readDir(path: string, options: ReadDirectoryOptions) {
    return window.api.fs.readDir(path, options);
  }

  async readFile(path: string) {
    return window.api.fs.readFile(path);
  }

  async openPath(path: string) {
    return window.api.fs.openPath(path);
  }
}

const FSH = new FileSystemHelper();
export { FSH };