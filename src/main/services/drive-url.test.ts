import { describe, expect, it } from "vitest";

import { DriveApiError } from "./drive-errors";
import { encodeNahidaPassword, parseDriveSourceUrl } from "./drive-url";

describe("encodeNahidaPassword", () => {
    it("matches the web client's URL-safe Base64 encoding", () => {
        expect(encodeNahidaPassword("gayshin")).toBe("Z2F5c2hpbg");
        expect(encodeNahidaPassword("비밀번호")).toBe("67mE67CA67KI7Zi4");
    });
});

describe("parseDriveSourceUrl", () => {
    it.each([
        ["https://nahida.live/akasha/link/link_123", { type: "link", id: "link_123" }],
        ["https://www.nahida.live/akasha/mod/mod-456/", { type: "mod", id: "mod-456" }],
    ])("parses %s", (value, expected) => {
        expect(parseDriveSourceUrl(value)).toEqual(expected);
    });

    it.each([
        "",
        "nahida://link/abc",
        "http://nahida.live/akasha/link/abc",
        "https://example.com/akasha/link/abc",
        "https://nahida.live/akasha/link/",
        "https://nahida.live/akasha/unknown/abc",
        "https://nahida.live/akasha/mod/abc/extra",
    ])("rejects unsupported source %s", (value) => {
        expect(() => parseDriveSourceUrl(value)).toThrowError(DriveApiError);
        expect(() => parseDriveSourceUrl(value)).toThrow("DRIVE_INVALID_SOURCE_URL");
    });
});
