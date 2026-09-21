import { describe, expect, it } from "vitest";

import { isElevatedHelperRequiredError } from "./elevated-helper";

describe("isElevatedHelperRequiredError", () => {
    it("matches the stable classification prefix", () => {
        expect(
            isElevatedHelperRequiredError(
                "ELEVATED_HELPER_REQUIRED: elevated helper is not running",
            ),
        ).toBe(true);
    });

    it("rejects unrelated errors", () => {
        expect(isElevatedHelperRequiredError("WINDOW_NOT_FOUND")).toBe(false);
        expect(isElevatedHelperRequiredError(null)).toBe(false);
    });
});
