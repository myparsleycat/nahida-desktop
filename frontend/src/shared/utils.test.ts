import { describe, expect, it } from "vitest";

import { getTextureResizeCandidates, toErrorMessage } from "./utils";

describe("toErrorMessage", () => {
    it("returns a fallback for blank errors", () => {
        expect(toErrorMessage(new Error())).toBe("Unknown error");
        expect(toErrorMessage("   ")).toBe("Unknown error");
    });

    it("falls back to an outer message when a nested value is empty", () => {
        expect(toErrorMessage({ value: {}, message: "outer message" })).toBe("outer message");
    });

    it("handles indirect value cycles without overflowing the stack", () => {
        const first: { value?: unknown } = {};
        const second: { value?: unknown } = {};
        first.value = second;
        second.value = first;

        expect(toErrorMessage(first)).toBe("Unknown error");
    });
});

describe("getTextureResizeCandidates", () => {
    it("returns a single candidate for 4096x2048", () => {
        expect(getTextureResizeCandidates(4096, 2048)).toEqual([{ width: 2048, height: 1024 }]);
    });

    it("returns multiple candidates for 8192x4096", () => {
        expect(getTextureResizeCandidates(8192, 4096)).toEqual([
            { width: 2048, height: 1024 },
            { width: 4096, height: 2048 },
            { width: 6144, height: 3072 },
        ]);
    });

    it("returns empty array when dimensions cannot be reduced by 1024 steps", () => {
        expect(getTextureResizeCandidates(1024, 1024)).toEqual([]);
        expect(getTextureResizeCandidates(512, 512)).toEqual([]);
    });
});
