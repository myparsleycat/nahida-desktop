import { afterEach, describe, expect, it, vi } from "vitest";

import { BinaryTransportError, fetchFloat32, fetchUint32, uploadTypedArray } from "./binary-memory";

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
});
