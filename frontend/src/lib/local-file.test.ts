import { describe, expect, it } from "vitest";

import { localFileSrc } from "./local-file";

function protocolParams(src: string): URLSearchParams {
    const query = src.startsWith("/protocol/local?") ? src.slice("/protocol/local?".length) : "";
    return new URLSearchParams(query);
}

describe("localFileSrc", () => {
    it("encodes a Windows path with spaces as a protocol query", () => {
        const path = String.raw`E:\GIMI\Mods\Character\CharG\CharG Nude toggle\preview.png`;
        const src = localFileSrc(path);
        const params = protocolParams(src);

        expect(src.startsWith("/protocol/local?")).toBe(true);
        expect(params.get("path")).toBe(path);
        expect(params.has("orig")).toBe(false);
        expect(params.has("v")).toBe(false);
    });

    it("adds orig and cache-busting query params", () => {
        const path = String.raw`E:\GIMI\Mods\preview.png`;
        const params = protocolParams(
            localFileSrc(path, { orig: true, cacheKey: 1786779048238.0342 }),
        );

        expect(params.get("path")).toBe(path);
        expect(params.get("orig")).toBe("true");
        expect(params.get("v")).toBe("1786779048238.0342");
    });

    it("passes through protocol, blob, and remote URLs", () => {
        expect(localFileSrc("/protocol/local?path=E%3A%5Cpreview.png")).toBe(
            "/protocol/local?path=E%3A%5Cpreview.png",
        );
        expect(localFileSrc("/protocol/model-viewer-memory/session/buffer")).toBe(
            "/protocol/model-viewer-memory/session/buffer",
        );
        expect(localFileSrc("blob:http://127.0.0.1/abc")).toBe("blob:http://127.0.0.1/abc");
        expect(localFileSrc("https://example.com/preview.png")).toBe(
            "https://example.com/preview.png",
        );
    });

    it("rewrites leftover Electron local:// and file:// URLs", () => {
        const windowsPath = String.raw`E:\GIMI\Mods\CharG Nude toggle\preview.png`;
        expect(protocolParams(localFileSrc(`local://${windowsPath}`)).get("path")).toBe(
            windowsPath,
        );
        expect(protocolParams(localFileSrc("file:///E:/GIMI/Mods/preview.png")).get("path")).toBe(
            "E:/GIMI/Mods/preview.png",
        );
        expect(localFileSrc("model-viewer-memory://session/buffer.bin")).toBe(
            "/protocol/model-viewer-memory/session/buffer.bin",
        );
    });
});
