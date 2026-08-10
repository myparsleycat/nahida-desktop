import { describe, expect, it } from "vitest";

import { DriveApiError } from "./drive-errors";
import { encodeNahidaPassword, NAHIDA_SOURCE_HOSTNAMES, parseDriveSourceUrl } from "./drive-url";

const sourceUrl = (hostname: string, pathname: string, protocol = "https:") =>
    new URL(pathname, `${protocol}//${hostname}`).toString();

const testSourceUrls = [
    {
        label: "public folder",
        url: "https://nahida.live/akasha/link/qjsEdvLpcAxr",
        expected: { type: "link", id: "qjsEdvLpcAxr" },
    },
    {
        label: "private folder",
        url: "https://nahida.live/akasha/link/ZwgSTtFUXZGu",
        expected: { type: "link", id: "ZwgSTtFUXZGu" },
    },
    {
        label: "public collection",
        url: "https://nahida.live/akasha/mod/WmVWMjAzthuFpKZiE-AKj",
        expected: { type: "mod", id: "WmVWMjAzthuFpKZiE-AKj" },
    },
    {
        label: "private collection",
        url: "https://nahida.live/akasha/mod/-fpnEyi_nPNB-Mf97p5_k",
        expected: { type: "mod", id: "-fpnEyi_nPNB-Mf97p5_k" },
    },
    {
        label: "Base64-encoded public folder",
        url: "aHR0cHM6Ly9uYWhpZGEubGl2ZS9ha2FzaGEvbGluay9xanNFZHZMcGNBeHI=",
        expected: { type: "link", id: "qjsEdvLpcAxr" },
    },
] as const;

describe("encodeNahidaPassword", () => {
    it("matches the web client's URL-safe Base64 encoding", () => {
        expect(encodeNahidaPassword("gayshin")).toBe("Z2F5c2hpbg");
        expect(encodeNahidaPassword("비밀번호")).toBe("67mE67CA67KI7Zi4");
    });
});

describe("parseDriveSourceUrl", () => {
    it.each(testSourceUrls)("parses $label fixture", ({ url, expected }) => {
        expect(parseDriveSourceUrl(url)).toEqual(expected);
    });

    it.each([
        [
            sourceUrl(NAHIDA_SOURCE_HOSTNAMES[0], "/akasha/link/link_123"),
            { type: "link", id: "link_123" },
        ],
        [
            sourceUrl(NAHIDA_SOURCE_HOSTNAMES[1], "/akasha/mod/mod-456/"),
            { type: "mod", id: "mod-456" },
        ],
    ])("parses %s", (value, expected) => {
        expect(parseDriveSourceUrl(value)).toEqual(expected);
    });

    it.each([
        "",
        "nahida://link/abc",
        sourceUrl(NAHIDA_SOURCE_HOSTNAMES[0], "/akasha/link/abc", "http:"),
        sourceUrl("example.invalid", "/akasha/link/abc"),
        sourceUrl(NAHIDA_SOURCE_HOSTNAMES[0], "/akasha/link/"),
        sourceUrl(NAHIDA_SOURCE_HOSTNAMES[0], "/akasha/unknown/abc"),
        sourceUrl(NAHIDA_SOURCE_HOSTNAMES[0], "/akasha/mod/abc/extra"),
    ])("rejects unsupported source %s", (value) => {
        expect(() => parseDriveSourceUrl(value)).toThrowError(DriveApiError);
        expect(() => parseDriveSourceUrl(value)).toThrow("DRIVE_INVALID_SOURCE_URL");
    });
});
