import { describe, expect, it } from "vitest";

import { DriveApiError } from "./drive-errors";
import { parseDriveSourceUrl } from "./drive-url";

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
