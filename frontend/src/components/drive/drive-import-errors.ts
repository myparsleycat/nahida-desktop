import { toErrorMessage } from "@shared/utils";

export function getErrorCode(error: unknown) {
    if (!error || typeof error !== "object") return undefined;
    const code = (error as { code?: unknown }).code;
    if (typeof code === "string") return code;
    const match = toErrorMessage(error).match(/\b(DRIVE_[A-Z0-9_]+)\b/);
    return match ? match[1] : undefined;
}

export function isNotFoundError(error: unknown) {
    const code = getErrorCode(error) ?? "";
    const msg = toErrorMessage(error).toLowerCase();
    return (
        code.includes("NOT_FOUND") ||
        code.includes("EXPIRED") ||
        msg.includes("not found") ||
        msg.includes("expired") ||
        msg.includes("404")
    );
}
