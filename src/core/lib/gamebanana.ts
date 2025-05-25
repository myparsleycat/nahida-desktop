import path from "node:path";
import crypto from 'node:crypto';
import { nanoid } from 'nanoid';
import { fss } from "@core/services";

interface ModProfileResponse {
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
}

export interface FileData {
  name: string;
  url: string;
  md5: string;
}

class GameBananaClass {
  private baseApi: string;

  constructor() {
    this.baseApi = 'https://gamebanana.com/apiv11/Mod/';
  }

  async fetcher({
    url, method = "GET", body, response = 'json', useProxy = false
  }: {
    url: string; method?: "GET" | "POST"; body?: any; response: 'json' | 'raw'; useProxy?: boolean;
  }) {
    try {
      const aurl = useProxy ? `https://proxy.nahida.live/?url=${url}` : url;
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

      if (response === 'json') {
        return await res.json();
      } else if (response === 'raw') {
        return res;
      } else {
        throw new Error("잘못된 response 파라미터");
      }
    } catch (err: any) {
      console.error("Gamebanana fetcher Error:", err);
      throw new Error("Failed to fetch data from GameBanana API.");
    }
  }

  async GetModProfile(id: number): Promise<ModProfileResponse | null> {
    const url = `${this.baseApi}${id}/ProfilePage`;
    try {
      return await this.fetcher({ url, response: 'json', useProxy: true });
    } catch (err: any) {
      console.error(`Error fetching mod profile for ID ${id}:`, err);
      return null;
    }
  }

  GetFilesData(mod: ModProfileResponse) {
    return mod._aFiles.map((file) => ({
      name: file._sFile,
      url: file._sDownloadUrl.replace(/\\/g, ""),
      md5: file._sMd5Checksum
    }))
  }

  GetImagesData(mod: ModProfileResponse) {
    return mod._aPreviewMedia._aImages.map((img) => ({
      name: img._sFile,
      url: img._sBaseUrl.replace(/\\/g, "") + '/' + img._sFile
    }))
  }

  async DownloadModFiles(mod: ModProfileResponse, currentCharPath: string, retryCount = 3) {
    const files = this.GetFilesData(mod);
    if (!files || files.length === 0) throw new Error('파일 데이터를 가져오는데 실패했거나 배열이 비어있음');

    const downloadPromises = files.map(async (file) => {
      const id = nanoid();
      const extensionWithDot = file.name.substring(file.name.lastIndexOf('.'));
      // const filePath = path.join(TEMP_DIR, id + extensionWithDot);
      const filePath = path.join(currentCharPath, mod._sName + extensionWithDot);
      let downloadAttempts = 0;
      let checksumAttempts = 0;

      while (downloadAttempts < retryCount) {
        try {
          console.log('url', file.url);
          const response = await this.fetcher({ url: file.url, response: 'raw', useProxy: false });
          if (!response.ok) {
            downloadAttempts++;
            console.error('resp is not ok');
            throw new Error(`GamebananaHelpers: Failed to fetch ${file.url}, status: ${response.status}`);
          }
          const data = await response.arrayBuffer();
          await fss.writeFile(filePath, Buffer.from(data));

          while (checksumAttempts < retryCount) {
            const filebuf = await fss.readFile(filePath, 'buf');
            const hash = crypto.createHash('md5').update(filebuf).digest('hex');

            if (hash === file.md5) {
              return { id, name: file.name, path: filePath, status: 'success' };
            }
            checksumAttempts++;
          }

          console.error('invalid_checksum');
          return { id, name: file.name, path: filePath, status: 'invalid_checksum' };
        } catch (e: any) {
          console.error('에러', e.message);
        }
      }

      console.error('download fail');
      return { id, name: file.name, path: filePath, status: 'download_fail' };
    });

    return Promise.all(downloadPromises);
  }
}

export const GameBanana = new GameBananaClass();