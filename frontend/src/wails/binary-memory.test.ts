import { afterEach, describe, expect, it, vi } from "vitest";

import {
    BinaryTransportError,
    binaryUploadChunkBytes,
    fetchFloat32,
    fetchUint32,
    uploadTypedArray,
} from "./binary-memory";

afterEach(() => vi.unstubAllGlobals());

describe("binary memory transport", () => {
    it("decodes aligned float32 and uint32 buffers with exact counts", async () => {
        const payloads = [new Float32Array([1, 2]).buffer, new Uint32Array([3, 4]).buffer];
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => new Response(payloads.shift(), { status: 200 })),
        );

        await expect(fetchFloat32("/float", 2)).resolves.toEqual(new Float32Array([1, 2]));
        await expect(fetchUint32("/uint", 2)).resolves.toEqual(new Uint32Array([3, 4]));
    });

    it("rejects non-2xx, misaligned, and unexpected-size responses", async () => {
        const responses = [
            new Response(null, { status: 404 }),
            new Response(new Uint8Array([1, 2, 3])),
            new Response(new Float32Array([1]).buffer),
        ];
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => responses.shift()!),
        );

        await expect(fetchFloat32("/missing")).rejects.toMatchObject({
            status: 404,
        } satisfies Partial<BinaryTransportError>);
        await expect(fetchFloat32("/misaligned")).rejects.toThrow("not 4-byte aligned");
        await expect(fetchFloat32("/short", 2)).rejects.toThrow("expected 8, received 4");
    });

    it("uploads only the selected typed-array byte view", async () => {
        const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
            const body = init?.body as Uint8Array;
            expect([...body]).toEqual([2, 3]);
            expect(init?.headers).toEqual({ "Content-Type": "application/octet-stream" });
            return new Response(null, { status: 204 });
        });
        vi.stubGlobal("fetch", fetchMock);
        const bytes = new Uint8Array([1, 2, 3, 4]);

        await uploadTypedArray("/upload", bytes.subarray(1, 3));
        expect(fetchMock).toHaveBeenCalledOnce();
    });

    it("splits large uploads into WebView-safe chunks", async () => {
        const bodies: number[] = [];
        const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => {
            bodies.push((init?.body as Uint8Array).byteLength);
            return new Response(null, { status: 204 });
        });
        vi.stubGlobal("fetch", fetchMock);
        const payload = new Uint8Array(binaryUploadChunkBytes + 16);

        await uploadTypedArray("/upload", payload);
        expect(bodies).toEqual([binaryUploadChunkBytes, 16]);
    });

    it("rejects a failed chunk and does not start later chunks", async () => {
        const fetchMock = vi.fn(async () => {
            if (fetchMock.mock.calls.length === 1) {
                return new Response(null, { status: 204 });
            }
            return new Response(null, { status: 400 });
        });
        vi.stubGlobal("fetch", fetchMock);
        const payload = new Uint8Array(binaryUploadChunkBytes * 2 + 16);

        await expect(uploadTypedArray("/upload", payload)).rejects.toMatchObject({
            status: 400,
        } satisfies Partial<BinaryTransportError>);
        expect(fetchMock).toHaveBeenCalledTimes(2);
    });

    it("rejects an aborted chunk and does not start later chunks", async () => {
        const abortError = new DOMException("Aborted", "AbortError");
        const fetchMock = vi.fn(async () => {
            if (fetchMock.mock.calls.length === 1) {
                return new Response(null, { status: 204 });
            }
            throw abortError;
        });
        vi.stubGlobal("fetch", fetchMock);
        const payload = new Uint8Array(binaryUploadChunkBytes * 2 + 16);

        await expect(uploadTypedArray("/upload", payload)).rejects.toBe(abortError);
        expect(fetchMock).toHaveBeenCalledTimes(2);
    });
});
