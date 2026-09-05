const sensitiveKey =
    /^(authorization|proxy-authorization|cookie|set-cookie|rmc|token|access[_-]?token|refresh[_-]?token|password|secret|credentials|api[_-]?key|signature|state|stateResponse)$/i;

export function serializeDiagnostic(input: unknown): unknown {
    const seen = new WeakSet<object>();
    let remaining = 32;
    const visit = (value: unknown, key = ""): unknown => {
        if (sensitiveKey.test(key)) return "[redacted]";
        if (typeof value === "string") {
            const limit = /stack/i.test(key) ? 16 * 1024 : 4 * 1024;
            const sanitized = value
                .replace(/https?:\/\/[^\s"<>]+/g, (url) => {
                    try {
                        const parsed = new URL(url);
                        parsed.username = "";
                        parsed.password = "";
                        parsed.search = "";
                        parsed.hash = "";
                        return parsed.toString();
                    } catch {
                        return "[invalid URL]";
                    }
                })
                .replace(/\bBearer\s+[^\s,;]+/gi, "Bearer [redacted]")
                .replace(
                    /\b(token|access[_-]?token|refresh[_-]?token|password|secret|cookie|authorization|state|stateResponse)(\s*[=:]\s*)[^&\s,;}"']+/gi,
                    "$1$2[redacted]",
                );
            return sanitized.length > limit
                ? `${sanitized.slice(0, limit)} [truncated]`
                : sanitized;
        }
        if (value === null || typeof value === "boolean" || typeof value === "number") return value;
        if (typeof value === "bigint") return String(value);
        if (typeof value !== "object") return `[${typeof value}]`;
        if (seen.has(value)) return "[circular]";
        if (remaining-- <= 0) return "[truncated]";
        seen.add(value);
        try {
            if (Array.isArray(value)) {
                const result = value.slice(0, 32).map((entry) => visit(entry));
                if (value.length > 32) result.push(`[${value.length - 32} omitted]`);
                return result;
            }
            const keys = Object.keys(value);
            const entries: [string, unknown][] = keys
                .slice(0, 32)
                .map((key) => [key, Reflect.get(value, key)]);
            if (keys.length > 32) entries.push(["diagnosticOmittedFields", keys.length - 32]);
            if (
                value instanceof Error ||
                (typeof DOMException !== "undefined" && value instanceof DOMException)
            ) {
                entries.push(
                    ["name", value.name],
                    ["message", value.message],
                    ["stack", value.stack],
                );
                if ("cause" in value) entries.push(["cause", value.cause]);
                if ("errors" in value) entries.push(["errors", value.errors]);
            }
            return Object.fromEntries(entries.map(([name, entry]) => [name, visit(entry, name)]));
        } catch {
            return { error: "Unable to inspect diagnostic value" };
        } finally {
            seen.delete(value);
        }
    };
    return visit(input);
}

export function isSilentDiagnostic(error: unknown): boolean {
    if (!(error instanceof Error)) return false;
    // RuntimeError with a cause is the installed Wails runtime's service-error
    // envelope; the Go service marshaler already records that failure.
    if (error.name === "RuntimeError" && error.cause !== undefined) return true;
    return (
        error.name === "AbortError" ||
        /^(GAMEBANANA_LOGIN_CANCELLED|DRIVE_COPY_CANCELED|CUSTOM_DOWNLOAD_(?:ABORTED|CANCELED))(?::|$)/.test(
            error.message,
        )
    );
}
