import { join } from "node:path";
import crypto from 'node:crypto';
import { nanoid } from 'nanoid';
import { FSService } from "@core/services";
import { ProxyUrl } from "@core/const";

export interface ModProfileResponse {
  _idRow: number;
  _aPreviewMedia: {
    _aImages: {
      _sBaseUrl: string; // baseUrl (_sBaseUrl + / + _sFile 처럼 조합해서 사용)
      _sFile: string; // 이미지 파일 이름
    }[]
  };
  _sName: string; // 모드 이름
  _aFiles: {
    _idRow: number;
    _sFile: string; // 파일 이름
    _sDownloadUrl: string; // 다운로드 링크
    _sMd5Checksum: string; // MD5 해시
  }[];
  _aSubmitter: {
    _sName: string; // 업로더 이름
  }
  __aGame: {
    _sName: string; // 게임 이름
    _sAbbreviation: 'GI' | 'HSR' | 'ZZZ' | 'WuWa'; // 게임 약어
  }
}

export interface FileData {
  name: string;
  url: string;
  md5: string;
}

class GameBananaClass {
  private static baseApi = 'https://gamebanana.com/apiv11/Mod/';
  private modId?: number;
  private modData: ModProfileResponse | null = null;
  private isLoaded: boolean = false;

  constructor(modId?: number) {
    this.modId = modId;
  }

  private async fetcher({
    url, method = "GET", body, response = 'json', useProxy = false
  }: {
    url: string; method?: "GET" | "POST"; body?: any; response: 'json' | 'raw'; useProxy?: boolean;
  }) {
    try {
      const aurl = useProxy ? ProxyUrl + url : url;
      const res = await fetch(aurl, {
        method,
        headers: {
          "referer": `${url}`,
          "user-agent":
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
        },
        body: body ? JSON.stringify(body) : undefined,
      });

      if (!res.ok) {
        throw new Error(`HTTP Error: ${res.status} ${res.statusText}`);
      }

      if (response === 'json') return await res.json();
      else if (response === 'raw') return res;
      else throw new Error("Invalid response");
    } catch (err: any) {
      console.error("Gamebanana fetcher Error:", err.message);
      throw err;
    }
  }

  private async ensureModDataLoaded(): Promise<void> {
    if (!this.isLoaded && this.modId) {
      await this.GetModProfile(this.modId);
    }
  }

  async GetModProfile(id?: number): Promise<ModProfileResponse | null> {
    const modId = id || this.modId;
    if (!modId) throw new Error("Mod ID is required");

    const url = `${GameBananaClass.baseApi}${modId}/ProfilePage`;
    try {
      const data = await this.fetcher({ url, response: 'json', useProxy: true });

      if (!id && this.modId) {
        this.modData = data;
        this.isLoaded = true;
      }

      return data;
    } catch (err: any) {
      console.error(`Error fetching mod profile for ID ${modId}:`, err);
      return null;
    }
  }

  GetFilesData(mod?: ModProfileResponse): FileData[] {
    const modData = mod || this.modData;
    if (!modData) throw new Error("Mod data is required");

    return modData._aFiles.map((file) => ({
      name: file._sFile,
      url: file._sDownloadUrl.replace(/\\/g, ""),
      md5: file._sMd5Checksum
    }));
  }

  GetFileData(modOrFileId: ModProfileResponse | number, fileId?: number): FileData {
    let mod: ModProfileResponse;
    let targetFileId: number;

    if (typeof modOrFileId === 'number') {
      mod = this.modData!;
      targetFileId = modOrFileId;
    } else {
      mod = modOrFileId;
      targetFileId = fileId!;
    }

    const file = mod._aFiles.find(f => f._idRow === targetFileId);
    if (!file) {
      throw new Error(`File with ID ${targetFileId} not found in mod ${mod._sName}`);
    }

    return {
      name: file._sFile,
      url: file._sDownloadUrl.replace(/\\/g, ""),
      md5: file._sMd5Checksum
    };
  }

  GetImagesData(mod?: ModProfileResponse): Array<{ name: string; url: string }> {
    const modData = mod || this.modData;
    if (!modData) throw new Error("Mod data is required");

    return modData._aPreviewMedia._aImages.map((img) => ({
      name: img._sFile,
      url: img._sBaseUrl.replace(/\\/g, "") + '/' + img._sFile
    }));
  }

  async DownloadModFiles(modOrPath: ModProfileResponse | string, currentCharPath?: string, retryCount = 3) {
    let mod: ModProfileResponse;
    let path: string;

    if (typeof modOrPath === 'string') {
      await this.ensureModDataLoaded();
      mod = this.modData!;
      path = modOrPath;
    } else {
      mod = modOrPath;
      path = currentCharPath!;
    }

    const files = this.GetFilesData(mod);
    if (!files || files.length === 0) {
      throw new Error('파일 데이터를 가져오는데 실패했거나 배열이 비어있음');
    }

    const downloadPromises = files.map(async (file) => {
      const id = nanoid();
      const extensionWithDot = file.name.substring(file.name.lastIndexOf('.'));
      const filePath = join(path, mod._sName + extensionWithDot);
      let downloadAttempts = 0;
      let checksumAttempts = 0;

      while (downloadAttempts < retryCount) {
        try {
          const response = await this.fetcher({ url: file.url, response: 'raw', useProxy: false });
          if (!response.ok) {
            downloadAttempts++;
            console.error('resp is not ok');
            throw new Error(`GamebananaHelpers: Failed to fetch ${file.url}, status: ${response.status}`);
          }
          const data = await response.arrayBuffer();
          await FSService.writeFile(filePath, Buffer.from(data));

          while (checksumAttempts < retryCount) {
            const filebuf = await FSService.readFile(filePath, 'buf');
            const hash = crypto.createHash('md5').update(filebuf).digest('hex');

            if (hash === file.md5) {
              return { id, name: file.name, path: filePath, status: 'success' as const };
            }
            checksumAttempts++;
          }

          console.error('invalid_checksum');
          return { id, name: file.name, path: filePath, status: 'invalid_checksum' as const };
        } catch (e: any) {
          console.error('에러', e.message);
        }
        downloadAttempts++;
      }

      console.error('download fail');
      return { id, name: file.name, path: filePath, status: 'download_fail' as const };
    });

    return Promise.all(downloadPromises);
  }

  async getModName(): Promise<string> {
    await this.ensureModDataLoaded();
    return this.modData!._sName;
  }

  async getSubmitterName(): Promise<string> {
    await this.ensureModDataLoaded();
    return this.modData!._aSubmitter._sName;
  }
}

export const GameBanana = new GameBananaClass();
export { GameBananaClass };