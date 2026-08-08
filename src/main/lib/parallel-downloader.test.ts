import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    head: vi.fn(),
    request: vi.fn(),
}));

vi.mock("ky", () => ({
    default: Object.assign(mocks.request, { head: mocks.head }),
}));

import { ParallelDownloader } from "./parallel-downloader";

describe("ParallelDownloader range capability probe", () => {
    beforeEach(() => {
        mocks.head.mockReset();
        mocks.request.mockReset();
        mocks.head.mockResolvedValue(
            new Response(null, {
                status: 200,
                headers: { "Accept-Ranges": "bytes" },
            }),
        );
    });

    it("does not cache negative range probes", async () => {
        const getHeaders = vi.fn().mockResolvedValue({ Authorization: "Bearer token" });
        const downloader = new ParallelDownloader({ getHeaders });
        mocks.head.mockResolvedValue(
            new Response(null, {
                status: 200,
            }),
        );

        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=one"),
        ).resolves.toBe(false);
        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=two"),
        ).resolves.toBe(false);

        expect(mocks.head).toHaveBeenCalledTimes(2);
        expect(getHeaders).toHaveBeenCalledTimes(2);
    });

    it("reuses the probe for signed URLs that share a resource path", async () => {
        const getHeaders = vi.fn().mockResolvedValue({ Authorization: "Bearer token" });
        const downloader = new ParallelDownloader({ getHeaders });

        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=one"),
        ).resolves.toBe(true);
        await expect(
            downloader.checkRangeSupport("https://example.test/files/abc?signature=two"),
        ).resolves.toBe(true);

        expect(mocks.head).toHaveBeenCalledOnce();
        expect(getHeaders).toHaveBeenCalledOnce();
        expect(mocks.head).toHaveBeenCalledWith(
            "https://example.test/files/abc?signature=one",
            expect.objectContaining({ retry: 0 }),
        );
    });
});
