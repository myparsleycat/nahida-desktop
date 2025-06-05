// @ts-nocheck

import path from "node:path";
import fse from 'fs-extra';
import AdmZip from 'adm-zip';
import { fss } from "../services";

class Archive {
  supportedFormats = ['zip', 'rar', '7z'];

  constructor() { }

  zip = {
    create: async (path: string, outputPath?: string, options = {}) => {
      return await this._createZip(path, outputPath, options);
    },

    unzip: async (archivePath: string, outputDir?: string, options = {}) => {
      return await this._extractZip(archivePath, outputDir, options);
    },

    list: async (archivePath: string) => {
      return await this._listZip(archivePath);
    },

    add: async (archivePath: string, filePaths: string) => {
      return await this._addToZip(archivePath, filePaths);
    },

    remove: async (archivePath: string, fileNames: string) => {
      return await this._removeFromZip(archivePath, fileNames);
    }
  }

  rar = {
    unrar: async (archivePath: string, outputDir?: string, options = {}) => {
      return await this._extractRar(archivePath, outputDir, options);
    },

    list: async (archivePath: string) => {
      return await this._listRar(archivePath);
    }
  }

  seven = {
    create: async (files, outputPath: string, options = {}) => {
      return await this._create7z(files, outputPath, options);
    },

    extract: async (archivePath: string, outputDir?: string, options = {}) => {
      return await this._extract7z(archivePath, outputDir, options);
    },

    list: async (archivePath: string) => {
      return await this._list7z(archivePath);
    },

    add: async (archivePath: string, filePaths: string[]) => {
      return await this._addTo7z(archivePath, filePaths);
    },

    remove: async (archivePath, fileNames) => {
      return await this._removeFrom7z(archivePath, fileNames);
    }
  }

  async create(format: 'zip', path: string, outputPath?: string, options?: any) {
    const normalizedFormat = format.toLowerCase();

    switch (normalizedFormat) {
      case 'zip':
        return await this.zip.create(path, outputPath, options);
      // case '7z':
      //   return await this.seven.create(files, outputPath, options);
      default:
        throw new Error(`${format} 형식의 생성은 지원되지 않음`);
    }
  }

  async extract(archivePath: string, outputDir?: string, options = {}) {
    const format = this._detectFormat(archivePath);

    switch (format) {
      case 'zip':
        return await this.zip.unzip(archivePath, outputDir, options);
      case 'rar':
        return await this.rar.unrar(archivePath, outputDir, options);
      case '7z':
        return await this.seven.extract(archivePath, outputDir, options);
      default:
        throw new Error(`${format} 형식은 지원되지 않음`);
    }
  }

  async list(archivePath: string) {
    const format = this._detectFormat(archivePath);

    switch (format) {
      case 'zip':
        return await this.zip.list(archivePath);
      // case 'rar':
      //   return await this.rar.list(archivePath);
      // case '7z':
      //   return await this.seven.list(archivePath);
      default:
        throw new Error(`${format} 형식은 지원되지 않음`);
    }
  }

  // async add(archivePath, filePaths) {
  //   const format = this._detectFormat(archivePath);

  //   switch (format) {
  //     case 'zip':
  //       return await this.zip.add(archivePath, filePaths);
  //     case '7z':
  //       return await this.seven.add(archivePath, filePaths);
  //     default:
  //       throw new Error(`${format} 형식에 파일 추가는 지원되지 않음`);
  //   }
  // }

  // async remove(archivePath, fileNames) {
  //   const format = this._detectFormat(archivePath);

  //   switch (format) {
  //     case 'zip':
  //       return await this.zip.remove(archivePath, fileNames);
  //     case '7z':
  //       return await this.seven.remove(archivePath, fileNames);
  //     default:
  //       throw new Error(`${format} 형식에서 파일 제거는 지원되지 않음`);
  //   }
  // }

  _detectFormat(filePath: string) {
    const ext = filePath.split('.').pop()?.toLowerCase();

    switch (ext) {
      case 'zip':
        return 'zip';
      case 'rar':
        return 'rar';
      case '7z':
        return '7z';
      default:
        throw new Error(`지원하지 않는 파일 형식: ${ext}`);
    }
  }

  getSupportedFormats() {
    return [...this.supportedFormats];
  }

  isFormatSupported(format: string) {
    return this.supportedFormats.includes(format.toLowerCase());
  }

  async _createZip(sourcePath: string, outputPath?: string, options?: createZipOptions) {
    return new Promise(async (resolve, reject) => {
      try {
        if (!outputPath) {
          const parsedPath = path.parse(sourcePath);
          outputPath = path.join(parsedPath.dir, `${parsedPath.name}.zip`);
        }

        const zip = new AdmZip();
        const stats = await fss.getStat(sourcePath);

        if (stats.isFile()) {
          zip.addLocalFile(sourcePath);
        } else if (stats.isDirectory()) {
          zip.addLocalFolder(sourcePath, options?.folderName || '');
        } else {
          throw new Error('지원하지 않는 파일 타입');
        }

        zip.writeZip(outputPath);
        resolve(outputPath);

      } catch (err: any) {
        reject(err);
      }
    });
  }

  async _extractZip(archivePath: string, outputDir?: string, options?: extractZipOptions) {
    return new Promise(async (resolve, reject) => {
      try {
        if (!outputDir) {
          const parsedPath = path.parse(archivePath);
          outputDir = path.join(parsedPath.dir, parsedPath.name);
        }

        if (!await fss.exists(outputDir)) {
          fss.mkdir(outputDir, { recursive: true });
        }

        const zip = new AdmZip(archivePath);

        if (options?.overwrite === false) {
          // 덮어쓰기 방지 옵션
          const entries = zip.getEntries();
          for (const entry of entries) {
            const entryPath = path.join(outputDir, entry.entryName);
            if (await fss.exists(entryPath)) {
              throw new Error(`파일이 이미 존재합니다: ${entryPath}`);
            }
          }
        }

        zip.extractAllTo(outputDir, options?.overwrite !== false);

        // const entryCount = zip.getEntries().length;
        resolve(outputDir);
      } catch (error) {
        reject(error);
      }
    });
  }

  async _listZip(archivePath: string) {
    try {
      const zip = new AdmZip(archivePath);
      const entries = zip.getEntries();

      return entries.map(entry => ({
        name: entry.entryName,
        size: entry.header.size,
        compressedSize: entry.header.compressedSize,
        isDirectory: entry.isDirectory,
        date: entry.header.time
      }));
    } catch (err: any) {
      throw new Error(`ZIP 파일을 읽을 수 없음: ${err.message}`);
    }
  }

  async _addToZip(archivePath, filePaths) { }

  async _removeFromZip(archivePath, fileNames) { }

  async _extractRar(archivePath, outputDir, options) { }

  async _listRar(archivePath) { }

  async _create7z(files, outputPath, options) { }

  async _extract7z(archivePath, outputDir, options) { }

  async _list7z(archivePath) { }

  async _addTo7z(archivePath: string, filePaths: string[]) { }

  async _removeFrom7z(archivePath, fileNames) { }
}

export const ArchiveManager = new Archive();