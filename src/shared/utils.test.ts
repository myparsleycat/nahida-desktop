import { describe, expect, it } from "vitest";

import { toErrorMessage } from "./utils";

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
