import { toErrorMessage } from "@shared/utils";

export class DriveApiError extends Error {
    public readonly code: string;
    public readonly status?: number;

    public constructor(code: string, message: string, status?: number, cause?: unknown) {
        super(`${code}: ${message}`, { cause });
        this.name = "DriveApiError";
        this.code = code;
        this.status = status;
    }
}

export function createDriveApiError(
    error: unknown,
    operation: string,
    status?: number,
    fallback = "Drive request failed",
) {
    if (error instanceof DriveApiError) return error;

    const message = toErrorMessage(error);
    const normalizedMessage = message === "[object Object]" ? fallback : message || fallback;
    const code = toDriveErrorCode(error) ?? `DRIVE_${operation.toUpperCase()}_FAILED`;
    return new DriveApiError(code, normalizedMessage, status, error);
}

function toDriveErrorCode(error: unknown) {
    if (typeof error === "string" && /^[A-Z][A-Z0-9_]+$/.test(error)) return error;
    if (typeof error !== "object" || error === null) return undefined;

    const record = error as Record<string, unknown>;
    const code = record.code;
    if (typeof code === "string" && code.trim()) return code;

    const value = record.value;
    if (typeof value === "string" && /^[A-Z][A-Z0-9_]+$/.test(value)) return value;
    if (typeof value === "object" && value !== null) {
        const nestedCode = (value as Record<string, unknown>).code;
        if (typeof nestedCode === "string" && nestedCode.trim()) return nestedCode;
    }

    return undefined;
}
