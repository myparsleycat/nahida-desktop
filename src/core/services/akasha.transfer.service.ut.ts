import { gunzipAsync, zstdDecompress } from "@core/utils";
import { fss } from "./fs.service";
import { DirInfo, FileInfo } from "./fs.service.ut";

export async function downloadFiles(
  files: FileInfo[],
  dirPaths: Record<string, string>,
  _dirMap: Record<string, DirInfo>,
  rootId: string
): Promise<void> {
  let downloadedCount = 0;
  let failedCount = 0;
  const totalFiles = files.length;
  const failedFiles: { file: FileInfo, error: string }[] = [];
  const concurrencyLimit = 80;
  const downloadedFiles: { file: FileInfo, compressedPath: string, destPath: string }[] = [];

  const queue = [...files];
  const activePromises = new Map();

  try {
    while (queue.length > 0 || activePromises.size > 0) {
      while (activePromises.size < concurrencyLimit && queue.length > 0) {
        const file = queue.shift()!;

        let saveDir: string;
        if (file.parentId && file.parentId === rootId) {
          saveDir = `${dirPaths[rootId] || ''}`;
        } else if (file.parentId && dirPaths[file.parentId]) {
          saveDir = dirPaths[file.parentId];
        } else {
          saveDir = `${dirPaths[rootId] || ''}`;
        }

        const filePath = `${saveDir}/${file.name}`;
        const compressedPath = `${filePath}.nhdtmp`;
        const downloadId = `${file.uuid}-${Date.now()}`;

        const downloadPromise = (async () => {
          try {
            await downloadCompressedFile(file, compressedPath);
            downloadedCount++;
            downloadedFiles.push({ file, compressedPath, destPath: filePath });
            return { success: true, file };
          } catch (error: any) {
            console.error(`파일 다운로드 실패: ${file.name}`, error);
            failedCount++;
            failedFiles.push({ file, error: error.message || String(error) });
            return { success: false, file, error };
          } finally {
            activePromises.delete(downloadId);
          }
        })();

        activePromises.set(downloadId, downloadPromise);
      }

      if (activePromises.size > 0) {
        await Promise.race(Array.from(activePromises.values()));
      }
    }

    console.log(`모든 파일 다운로드 완료 - 총 파일: ${totalFiles}, 성공: ${downloadedCount}, 실패: ${failedCount}`);
    console.log('압축 해제 시작...');
    await decompressAllFiles(downloadedFiles);

  } catch (error) {
    console.error("전체 다운로드 프로세스 오류:", error);
  }

  console.log(`전체 작업 완료 - 총 파일: ${totalFiles}, 성공: ${downloadedCount}, 실패: ${failedCount}`);
  if (failedFiles.length > 0) {
    console.log('실패한 파일 목록:');
    failedFiles.forEach(({ file, error }) => {
      console.log(`- ${file.name}: ${error}`);
    });
  }
}

async function downloadCompressedFile(file: FileInfo, compressedPath: string): Promise<void> {
  let retries = 0;
  const maxRetries = 3;
  let lastError: Error | null = null;

  while (retries <= maxRetries) {
    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 30000);

      try {
        const resp = await fetch(file.url, {
          signal: controller.signal
        });

        clearTimeout(timeoutId);

        if (!resp.ok) {
          throw new Error(`HTTP 오류: ${resp.status}`);
        }

        const respBuf = await resp.arrayBuffer();
        const buffer = Buffer.from(respBuf);

        await fss.writeFile(compressedPath, buffer);
        return;
      } catch (e) {
        clearTimeout(timeoutId);
        throw e;
      }
    } catch (error: any) {
      lastError = error as Error;
      retries++;

      if (retries > maxRetries) {
        break;
      }

      const delay = Math.pow(2, retries - 1) * 100;
      await new Promise(resolve => setTimeout(resolve, delay));
    }
  }

  throw new Error(`최대 재시도 횟수 초과: ${lastError?.message || '알 수 없는 오류'}`);
}

async function decompressAllFiles(
  files: { file: FileInfo, compressedPath: string, destPath: string }[]
): Promise<void> {
  let decompressedCount = 0;
  let failedCount = 0;
  const totalFiles = files.length;
  const batchSize = 100;

  for (let i = 0; i < files.length; i += batchSize) {
    const batch = files.slice(i, i + batchSize);
    const promises = batch.map(async ({ file, compressedPath, destPath }) => {
      try {
        await decompressFile(file, compressedPath, destPath);
        decompressedCount++;
        return { success: true, file };
      } catch (error: any) {
        console.error(`파일 압축 해제 실패: ${file.name}`, error);
        failedCount++;
        return { success: false, file, error };
      } finally {
        try {
          await fss.deletePath(compressedPath);
        } catch (error) {
          console.warn(`임시 파일 삭제 실패: ${compressedPath}`, error);
        }
      }
    });

    await Promise.all(promises);
    console.log(`압축 해제 진행 중: ${Math.min(i + batchSize, totalFiles)}/${totalFiles}`);
  }

  console.log(`압축 해제 완료 - 총 파일: ${totalFiles}, 성공: ${decompressedCount}, 실패: ${failedCount}`);
}

async function decompressFile(file: FileInfo, compressedPath: string, destPath: string): Promise<void> {
  try {
    const compressedData = await fss.readFile(compressedPath, "buf");
    let buffer: Buffer;

    switch (file.compAlg) {
      case "gzip":
        buffer = await gunzipAsync(compressedData);
        break;
      case "zstd":
        const uncomp = await zstdDecompress(new Uint8Array(compressedData));
        buffer = Buffer.from(uncomp);
        break;
      default:
        buffer = compressedData;
    }

    await fss.writeFile(destPath, buffer);
  } catch (error) {
    throw new Error(`압축 해제 실패: ${error}`);
  }
}