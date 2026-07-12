import fse from "fs-extra";
import ky from "ky";
import { Readable, Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import type { ReadableStream } from "node:stream/web";
import type { ParallelDownloader } from "../parallel-downloader";

interface HttpServiceLike {
    getHeaders: (url: string) => Promise<Record<string, string>>;
}

export async function downloadFile(props: {
    url: string;
    savePath: string;
    fileSize?: number;
    signal?: AbortSignal;
    onProgress?: (bytes: number) => void;
    downloader: ParallelDownloader;
    httpService: HttpServiceLike;
}) {
    const { url, savePath, fileSize, signal, onProgress, downloader, httpService } = props;
    const supportsRange = await downloader.checkRangeSupport(url);

    if (supportsRange && fileSize) {
        await downloader.download({
            url,
            savePath,
            fileSize,
            signal,
            onProgress(bytes) {
                onProgress?.(bytes);
            },
            maxChunks: 8,
        });
    } else {
        const fileStream = fse.createWriteStream(savePath);

        const resp = await ky.get(url, {
            signal,
            headers: await httpService.getHeaders(url),
        });
        if (!resp.ok) {
            throw new Error(`Failed to download file: ${resp.statusText}`);
        }

        try {
            if (!resp.body) {
                throw new Error("No response body");
            }
            const source = Readable.fromWeb(resp.body as unknown as ReadableStream);
            const progressStream = new Transform({
                transform(chunk: Buffer, _encoding, callback) {
                    onProgress?.(chunk.byteLength);
                    callback(null, chunk);
                },
            });
            await pipeline(source, progressStream, fileStream, { signal });
        } catch (err) {
            fileStream.destroy();
            await fse.remove(savePath).catch(() => {});
            throw err;
        }
    }
}
