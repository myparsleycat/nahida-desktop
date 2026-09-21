import { describe, expect, it } from "vitest";

import { isElevatedHelperRequiredError } from "./elevated-helper";

describe("isElevatedHelperRequiredError", () => {
    it("matches the exact sentinel", () => {
        expect(isElevatedHelperRequiredError("ELEVATED_HELPER_REQUIRED")).toBe(true);
    });

    it("matches the stable classification prefix", () => {
        expect(
            isElevatedHelperRequiredError(
                "ELEVATED_HELPER_REQUIRED: elevated helper is not running",
            ),
        ).toBe(true);
    });

    it("rejects messages that only contain the sentinel", () => {
        expect(isElevatedHelperRequiredError("failed ELEVATED_HELPER_REQUIRED later")).toBe(false);
        expect(isElevatedHelperRequiredError("NOT_ELEVATED_HELPER_REQUIRED")).toBe(false);
    });

    it("rejects unrelated errors", () => {
        expect(isElevatedHelperRequiredError("WINDOW_NOT_FOUND")).toBe(false);
        expect(isElevatedHelperRequiredError(null)).toBe(false);
    });
});
