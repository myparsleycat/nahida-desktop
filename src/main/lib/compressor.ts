import { promisify } from "node:util";
import {
    brotliCompress,
    brotliDecompress,
    gunzip,
    gzip,
    zstdCompress,
    zstdDecompress,
    constants,
} from "node:zlib";
import type { NahidaDesktop } from "..";

export const gzipAsync = promisify(gzip);
export const gunzipAsync = promisify(gunzip);
export const zstdCompressAsync = promisify(zstdCompress);
export const zstdDecompressAsync = promisify(zstdDecompress);
export const brotliCompressAsync = promisify(brotliCompress);
export const brotliDecompressAsync = promisify(brotliDecompress);

interface ZstdCompressOption {
    chunkSize?: number; // default 16 * 1024
    level?: number; // default 3
}

export class Compressor {
    private readonly desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    zstd = {
        compress: async (data: Buffer, options?: ZstdCompressOption) => {
            return await zstdCompressAsync(data, {
                chunkSize: options?.chunkSize || 16 * 1024,
                params: {
                    [constants.ZSTD_c_compressionLevel]: options?.level || 3,
                },
            });
        },
        decompress: async (data: Buffer) => {
            return await zstdDecompressAsync(data);
        },
    };

    brotli = {
        compress: async (data: Buffer) => {
            return await brotliCompressAsync(data);
        },
        decompress: async (data: Buffer) => {
            return await brotliDecompressAsync(data);
        },
    };

    gzip = {
        compress: async (data: Buffer) => {
            return await gzipAsync(data);
        },
        decompress: async (data: Buffer) => {
            return await gunzipAsync(data);
        },
    };
}

export default Compressor;
