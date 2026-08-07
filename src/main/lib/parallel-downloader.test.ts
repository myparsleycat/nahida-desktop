import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
    head: vi.fn(),
}));

vi.mock("ky", () => ({
    default: {
        head: mocks.head,
    },
}));

import { ParallelDownloader } from "./parallel-downloader";

describe("ParallelDownloader range capability probe", () => {
    beforeEach(() => {
        mocks.head.mockReset();
        mocks.head.mockResolvedValue(
            new Response(null, {
                status: 200,
                headers: { "Accept-Ranges": "bytes" },
            }),
        );
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
