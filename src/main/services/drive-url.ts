import { DriveApiError } from "./drive-errors";

export type DriveSource = {
    type: "link" | "mod";
    id: string;
};

export const NAHIDA_SOURCE_HOSTNAMES = ["nahida.live", "www.nahida.live"] as const;
// Bound nested decoding so malformed input cannot trigger unbounded work.
const MAX_SOURCE_URL_DECODING_DEPTH = 10;

/** Match the web client's URL-safe Base64 password encoding. */
export function encodeNahidaPassword(value: string) {
    const binary = encodeURIComponent(value).replace(/%([0-9A-F]{2})/g, (_, hex: string) =>
        String.fromCharCode(Number.parseInt(hex, 16)),
    );
    return Buffer.from(binary, "latin1")
        .toString("base64")
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=+$/, "");
}

export function parseDriveSourceUrl(value: string): DriveSource {
    const sourceUrl = resolveSourceUrl(value);
    const source = sourceUrl ? parseNahidaSourceUrl(sourceUrl) : undefined;
    if (source) return source;

    throw new DriveApiError(
        "DRIVE_INVALID_SOURCE_URL",
        "Enter a Nahida shared link or collection URL.",
    );
}

function resolveSourceUrl(value: string, depth = 0): string | undefined {
    const normalized = value.trim();
    if (/^http/i.test(normalized)) return normalized;
    if (/^nahida/i.test(normalized)) return `https://${normalized}`;
    if (depth >= MAX_SOURCE_URL_DECODING_DEPTH) return;

    const decoded = decodeBase64(normalized);
    if (!decoded || decoded === normalized) return;

    return resolveSourceUrl(decoded, depth + 1);
}

function parseNahidaSourceUrl(value: string): DriveSource | undefined {
    try {
        const url = new URL(value.trim());
        if (url.protocol !== "https:") throw new Error("unsupported protocol");
        if (!NAHIDA_SOURCE_HOSTNAMES.some((hostname) => hostname === url.hostname.toLowerCase())) {
            throw new Error("unsupported host");
        }

        const linkMatch = /^\/akasha\/link\/([A-Za-z0-9_-]+)\/?$/i.exec(url.pathname);
        if (linkMatch) return { type: "link", id: linkMatch[1] };

        const modMatch = /^\/akasha\/mod\/([A-Za-z0-9_-]+)\/?$/i.exec(url.pathname);
        if (modMatch) return { type: "mod", id: modMatch[1] };
    } catch {
        // Normalize malformed external input to the same user-facing error below.
    }

    return;
}

function decodeBase64(value: string) {
    const normalized = value.trim().replace(/\s+/g, "").replace(/-/g, "+").replace(/_/g, "/");
    if (
        normalized.length === 0 ||
        normalized.length % 4 === 1 ||
        !/^[A-Za-z0-9+/]*={0,2}$/.test(normalized)
    ) {
        return;
    }

    return Buffer.from(normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "="), "base64")
        .toString("utf8")
        .trim();
}
