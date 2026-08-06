import { describe, expect, it } from "vitest";

import { createDriveApiError, DriveApiError } from "./drive-errors";

describe("createDriveApiError", () => {
    it("preserves an existing DriveApiError", () => {
        const original = new DriveApiError("DRIVE_CUSTOM", "already normalized");

        expect(createDriveApiError(original, "copy")).toBe(original);
    });

    it("extracts nested API codes and messages", () => {
        const error = createDriveApiError(
            { value: { code: "MISSING_PASSWORD", message: "Password required" } },
            "linkAccess",
            401,
        );

        expect(error).toMatchObject({
            code: "MISSING_PASSWORD",
            status: 401,
            message: "MISSING_PASSWORD: Password required",
        });
    });

    it("uses an operation-specific fallback for unknown errors", () => {
        const error = createDriveApiError({}, "copyFromUrl");

        expect(error).toMatchObject({
            code: "DRIVE_COPYFROMURL_FAILED",
            message: "DRIVE_COPYFROMURL_FAILED: {}",
        });
    });
});
