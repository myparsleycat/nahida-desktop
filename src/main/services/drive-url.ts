import { DriveApiError } from "./drive-errors";

export type DriveSource = {
    type: "link" | "mod";
    id: string;
};

export function parseDriveSourceUrl(value: string): DriveSource {
    try {
        const url = new URL(value.trim());
        if (!["http:", "https:"].includes(url.protocol)) throw new Error("unsupported protocol");
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
