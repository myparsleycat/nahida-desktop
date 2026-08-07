import { DriveApiError } from "./drive-errors";

export type DriveSource = {
    type: "link" | "mod";
    id: string;
};

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
    try {
        const url = new URL(value.trim());
        if (url.protocol !== "https:") throw new Error("unsupported protocol");
        if (!["nahida.live", "www.nahida.live"].includes(url.hostname.toLowerCase())) {
            throw new Error("unsupported host");
        }

        const linkMatch = /^\/akasha\/link\/([A-Za-z0-9_-]+)\/?$/i.exec(url.pathname);
        if (linkMatch) return { type: "link", id: linkMatch[1] };

        const modMatch = /^\/akasha\/mod\/([A-Za-z0-9_-]+)\/?$/i.exec(url.pathname);
        if (modMatch) return { type: "mod", id: modMatch[1] };
    } catch {
        // Normalize malformed external input to the same user-facing error below.
    }

    throw new DriveApiError(
        "DRIVE_INVALID_SOURCE_URL",
        "Enter a Nahida shared link or collection URL.",
    );
}
