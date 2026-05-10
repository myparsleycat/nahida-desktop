import { open } from "node:fs/promises";
import { fileTypeFromFile } from "file-type";

const archiveExts = new Set(["zip", "7z", "rar"]);

const archiveMimes = new Set([
    "application/zip",
    "application/x-zip-compressed",
    "application/x-7z-compressed",
    "application/vnd.rar",
    "application/x-rar-compressed",
]);

export function isArchiveFileName(fileName: string) {
    return /\.(zip|7z|rar)$/i.test(fileName);
}

export function isHtmlContentType(headers?: Headers) {
    const contentType = headers?.get("Content-Type")?.split(";")[0]?.trim().toLowerCase();
    return contentType === "text/html" || contentType === "application/xhtml+xml";
}

export async function isArchiveByResponseOrContent(props: {
    headers?: Headers;
    originalFileName?: string;
    filePath: string;
}) {
    const { headers, originalFileName, filePath } = props;

    if (originalFileName && isArchiveFileName(originalFileName)) {
        return true;
    }

    const contentDisposition = headers?.get("Content-Disposition");
    if (contentDisposition && isArchiveFileName(contentDisposition)) {
        return true;
    }

    const contentType = headers?.get("Content-Type")?.split(";")[0]?.trim().toLowerCase();
    if (contentType && archiveMimes.has(contentType)) {
        return true;
    }

    const fileType = await fileTypeFromFile(filePath);
    return !!fileType && archiveExts.has(fileType.ext);
}

export async function isHtmlResponseOrContent(props: { headers?: Headers; filePath: string }) {
    const { headers, filePath } = props;

    if (isHtmlContentType(headers)) {
        return true;
    }

    const fd = await open(filePath, "r");

    try {
        const buffer = Buffer.alloc(4096);
        const { bytesRead } = await fd.read(buffer, 0, buffer.length, 0);
        const snippet = buffer.subarray(0, bytesRead).toString("utf8").trimStart();
        return /^(<!doctype\s+html\b|<html\b)/i.test(snippet);
    } finally {
        await fd.close();
    }
}
