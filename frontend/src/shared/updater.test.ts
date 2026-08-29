import { describe, expect, it } from "vitest";

import { parseUpdaterError, parseUpdaterProgress } from "./updater";

describe("parseUpdaterProgress", () => {
    it("reads written and total from the payload", () => {
        expect(parseUpdaterProgress({ written: 10, total: 40, rate: 2 })).toEqual({
            written: 10,
            total: 40,
            rate: 2,
        });
    });

    it("unwraps a one-element event array", () => {
        expect(parseUpdaterProgress([{ written: 1, total: 2 }])).toEqual({
            written: 1,
            total: 2,
            rate: undefined,
        });
    });
});

describe("parseUpdaterError", () => {
    it("reads stage and message", () => {
        expect(
            parseUpdaterError({ stage: "download", message: "timeout", provider: "github" }),
        ).toEqual({
            stage: "download",
            message: "timeout",
            provider: "github",
        });
    });

    it("rejects payloads without a message", () => {
        expect(parseUpdaterError({ stage: "check" })).toBeNull();
    });
});
