import { promisify } from "node:util";
import { NahidaDesktop } from "..";
import {
    gzip,
    gunzip,
    zstdCompress,
    zstdDecompress,
    brotliCompress,
    brotliDecompress,
} from "node:zlib";

export const gzipAsync = promisify(gzip);
export const gunzipAsync = promisify(gunzip);
export const zstdCompressAsync = promisify(zstdCompress);
export const zstdDecompressAsync = promisify(zstdDecompress);
export const brotliCompressAsync = promisify(brotliCompress);
export const brotliDecompressAsync = promisify(brotliDecompress);

export class Compressor {
    private readonly desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    zstd = {
        compress: async (data: Buffer) => {
            return await zstdCompressAsync(data);
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
