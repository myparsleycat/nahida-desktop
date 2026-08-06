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
            code: "DRIVE_LINK_PASSWORD_REQUIRED",
            status: 401,
            message: "DRIVE_LINK_PASSWORD_REQUIRED: Password required",
        });
    });

    it("normalizes invalid password codes", () => {
        const error = createDriveApiError({ code: "INVALID_PASSWORD" }, "linkAccess");

        expect(error.code).toBe("DRIVE_LINK_INVALID_PASSWORD");
    });

    it("uses an operation-specific fallback for unknown errors", () => {
        const error = createDriveApiError({}, "copyFromUrl");

        expect(error.code).toBe("DRIVE_COPYFROMURL_FAILED");
        expect(error.message).toBe("DRIVE_COPYFROMURL_FAILED: Unknown error");
    });

    it("normalizes multiword operation names in fallback codes", () => {
        const error = createDriveApiError({}, "shared link access");

        expect(error.code).toBe("DRIVE_SHARED_LINK_ACCESS_FAILED");
    });
});
