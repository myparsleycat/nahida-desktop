import { describe, expect, it } from "vitest";

import { getNahidaDeepLinkRoute, parseNahidaDeepLink } from "./deep-link";

describe("parseNahidaDeepLink", () => {
    it.each([
        ["nahida://gamebanana/mods/123", "/gamebanana?mod=123"],
        ["nahida://gamebanana/mod/456", "/gamebanana?mod=456"],
        ["nahida://gamebanana?id=789", "/gamebanana?mod=789"],
        [
            "nahida://gamebanana/open?url=https%3A%2F%2Fgamebanana.com%2Fmods%2F321",
            "/gamebanana?mod=321",
        ],
    ])("maps %s to the GameBanana route", (value, expected) => {
        expect(parseNahidaDeepLink(value)).toBe(expected);
    });

    it.each([
        "https://gamebanana.com/mods/123",
        "nahida://gamebanana/mods/not-a-number",
        "nahida://gamebanana?id=0",
        "nahida://gamebanana/open?url=https%3A%2F%2Fexample.com%2Fmods%2F123",
        "nahida://gamebanana/open?url=file%3A%2F%2Fgamebanana.com%2Fmods%2F123",
    ])("rejects unsupported deep links", (value) => {
        expect(parseNahidaDeepLink(value)).toBeNull();
    });
});

describe("getNahidaDeepLinkRoute", () => {
    it("finds a deep link among process arguments", () => {
        expect(
            getNahidaDeepLinkRoute(["Nahida Desktop.exe", "--flag", "nahida://gamebanana/mods/42"]),
        ).toBe("/gamebanana?mod=42");
    });
});
