import { describe, expect, it, vi } from "vitest";

import type { ParallelDownloader } from "../parallel-downloader";

import { downloadFile } from "./download-file";

describe("downloadFile", () => {
    it("reuses the caller's range probe result", async () => {
        const checkRangeSupport = vi.fn();
        const download = vi.fn().mockResolvedValue(undefined);
        const downloader = { checkRangeSupport, download } as unknown as ParallelDownloader;

        await downloadFile({
            url: "https://example.test/file.bin",
            savePath: "file.bin",
            fileSize: 1024,
            supportsRange: true,
            signal: new AbortController().signal,
            downloader,
            httpService: { getHeaders: vi.fn() },
        });

        expect(checkRangeSupport).not.toHaveBeenCalled();
        expect(download).toHaveBeenCalledWith(
            expect.objectContaining({
                url: "https://example.test/file.bin",
                fileSize: 1024,
            }),
        );
    });
});
