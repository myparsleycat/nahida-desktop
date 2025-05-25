import { promisify } from "node:util";
import { gunzip, gzip } from "node:zlib";
import ZstdWasm from "./lib/zstd";

export function bufferToBase64(buf: Buffer) {
  return buf.toString('base64');
}

export function bufferToArrayBuffer(buf: Buffer | Uint8Array): ArrayBufferLike {
  if (buf instanceof Uint8Array) {
    if (buf.byteOffset === 0 && buf.byteLength === buf.buffer.byteLength) {
      return buf.buffer;
    }
    return buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength);
  }

  if (Buffer.isBuffer(buf)) {
    // @ts-ignore
    const arrayCopy = new Uint8Array(buf.length);
    // @ts-ignore
    for (let i = 0; i < buf.length; i++) {
      arrayCopy[i] = buf[i];
    }
    return arrayCopy.buffer;
  }

  throw new Error('입력은 Buffer 또는 Uint8Array여야 함');
}

export const gzipAsync = promisify(gzip);
export const gunzipAsync = promisify(gunzip);
// export const zstdCompressAsync = promisify(zstdCompress);
// export const zstdDecompressAsync = promisify(zstdDecompress);

export async function zstdCompress(data: Buffer, level?: number) {
  const zstd = await ZstdWasm.getInstance();
  return zstd.compress(data, level);
}

export async function zstdDecompress(data: Uint8Array) {
  const zstd = await ZstdWasm.getInstance();
  return zstd.decompress(new Uint8Array(data));
}