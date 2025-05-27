import { gunzipAsync, zstdDecompress } from "@core/utils";
import { fss } from "./fs.service";
import { DirInfo, FileInfo } from "./fs.service.ut";

// 진행상황 콜백 타입 정의
export interface DownloadProgressCallback {
  (progress: {
    downloadedBytes: number;
    totalBytes: number;
    downloadedFiles: number;
    totalFiles: number;
    currentFile: string;
    downloadSpeed: number; // bytes per second
    phase: 'downloading' | 'decompressing';
    decompressedFiles?: number;
  }): void;
}

// AbortSignal 지원을 위한 옵션
export interface DownloadOptions {
  progressCallback?: DownloadProgressCallback;
  abortSignal?: AbortSignal;
  concurrencyLimit?: number;
  retryAttempts?: number;
  timeout?: number;
}

export async function downloadFiles(
  files: FileInfo[],
  dirPaths: Record<string, string>,
  _dirMap: Record<string, DirInfo>,
  rootId: string,
  options: DownloadOptions = {}
): Promise<void> {
  const {
    progressCallback,
    abortSignal,
    concurrencyLimit = 80,
    retryAttempts = 3,
    timeout = 30000
  } = options;

  let downloadedCount = 0;
  let failedCount = 0;
  let downloadedBytes = 0;
  let decompressedCount = 0;
  const totalFiles = files.length;
  const totalBytes = files.reduce((sum, file) => sum + file.size, 0);
  const failedFiles: { file: FileInfo, error: string }[] = [];
  const downloadedFiles: { file: FileInfo, compressedPath: string, destPath: string }[] = [];

  // 속도 계산을 위한 변수들
  let startTime = Date.now();
  let lastProgressTime = Date.now();
  let lastDownloadedBytes = 0;

  const queue = [...files];
  const activePromises = new Map();

  // 진행상황 업데이트 함수
  const updateProgress = (currentFileName: string = '', phase: 'downloading' | 'decompressing' = 'downloading') => {
    if (!progressCallback) return;
    
    const now = Date.now();
    const timeDiff = (now - lastProgressTime) / 1000; // seconds
    const bytesDiff = downloadedBytes - lastDownloadedBytes;
    const downloadSpeed = timeDiff > 0 ? bytesDiff / timeDiff : 0;
    
    progressCallback({
      downloadedBytes,
      totalBytes,
      downloadedFiles: downloadedCount,
      totalFiles,
      currentFile: currentFileName,
      downloadSpeed,
      phase,
      decompressedFiles: decompressedCount
    });
    
    lastProgressTime = now;
    lastDownloadedBytes = downloadedBytes;
  };

  try {
    // 다운로드 단계
    while (queue.length > 0 || activePromises.size > 0) {
      // Abort 체크
      if (abortSignal?.aborted) {
        throw new Error('Download aborted by user');
      }

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
            updateProgress(file.name, 'downloading');
            
            await downloadCompressedFile(file, compressedPath, {
              abortSignal,
              timeout,
              retryAttempts
            });
            
            downloadedCount++;
            downloadedBytes += file.size;
            downloadedFiles.push({ file, compressedPath, destPath: filePath });
            
            updateProgress(file.name, 'downloading');
            
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
    
    // 압축 해제 단계
    if (downloadedFiles.length > 0) {
      console.log('압축 해제 시작...');
      updateProgress('', 'decompressing');
      
      await decompressAllFiles(downloadedFiles, {
        progressCallback: (current, _total) => {
          decompressedCount = current;
          updateProgress('', 'decompressing');
        },
        abortSignal
      });
    }

  } catch (error) {
    console.error("전체 다운로드 프로세스 오류:", error);
    throw error; // p-queue에서 에러 처리할 수 있도록 다시 throw
  }

  console.log(`전체 작업 완료 - 총 파일: ${totalFiles}, 성공: ${downloadedCount}, 실패: ${failedCount}`);
  if (failedFiles.length > 0) {
    console.log('실패한 파일 목록:');
    failedFiles.forEach(({ file, error }) => {
      console.log(`- ${file.name}: ${error}`);
    });
  }
}

interface DownloadFileOptions {
  abortSignal?: AbortSignal;
  timeout?: number;
  retryAttempts?: number;
}

async function downloadCompressedFile(
  file: FileInfo, 
  compressedPath: string,
  options: DownloadFileOptions = {}
): Promise<void> {
  const { abortSignal, timeout = 30000, retryAttempts = 3 } = options;
  
  let retries = 0;
  let lastError: Error | null = null;

  while (retries <= retryAttempts) {
    // Abort 체크
    if (abortSignal?.aborted) {
      throw new Error('Download aborted by user');
    }

    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), timeout);

      // 기존 abortSignal과 새로운 timeout controller 연결
      if (abortSignal) {
        abortSignal.addEventListener('abort', () => controller.abort());
      }

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

      if (retries > retryAttempts) {
        break;
      }

      // Exponential backoff
      const delay = Math.pow(2, retries - 1) * 100;
      await new Promise(resolve => setTimeout(resolve, delay));
    }
  }

  throw new Error(`최대 재시도 횟수 초과: ${lastError?.message || '알 수 없는 오류'}`);
}

interface DecompressOptions {
  progressCallback?: (current: number, total: number) => void;
  abortSignal?: AbortSignal;
  batchSize?: number;
}

async function decompressAllFiles(
  files: { file: FileInfo, compressedPath: string, destPath: string }[],
  options: DecompressOptions = {}
): Promise<void> {
  const { progressCallback, abortSignal, batchSize = 100 } = options;
  
  let decompressedCount = 0;
  let failedCount = 0;
  const totalFiles = files.length;

  for (let i = 0; i < files.length; i += batchSize) {
    // Abort 체크
    if (abortSignal?.aborted) {
      throw new Error('Decompression aborted by user');
    }

    const batch = files.slice(i, i + batchSize);
    const promises = batch.map(async ({ file, compressedPath, destPath }) => {
      try {
        await decompressFile(file, compressedPath, destPath);
        decompressedCount++;
        progressCallback?.(decompressedCount, totalFiles);
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