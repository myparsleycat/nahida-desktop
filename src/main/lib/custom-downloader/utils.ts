import os from "node:os";
import path from "node:path";
import { nanoid } from "nanoid";

export function parseContentLength(contentLength?: string | null) {
    if (!contentLength) {
        return undefined;
    }

    const size = Number(contentLength);
    return Number.isFinite(size) && size > 0 ? size : undefined;
}

export function parseDownloadFileName(
    url: string,
    sanitizeWindowsFilename: (name: string) => string,
    contentDisposition?: string | null,
) {
    const fileNameFromDisposition = contentDisposition
        ?.match(/filename\*\s*=\s*(?:UTF-8''|')?([^;]+)/i)?.[1]
        ?.trim()
        ?.replace(/^"(.*)"$/, "$1");

    if (fileNameFromDisposition) {
        return sanitizeWindowsFilename(decodeURIComponent(fileNameFromDisposition));
    }

    const fileNamePlain = contentDisposition?.match(/filename\s*=\s*("?)([^";]+)\1/i)?.[2]?.trim();

    if (fileNamePlain) {
        return sanitizeWindowsFilename(fileNamePlain);
    }

    try {
        const pathname = new URL(url).pathname;
        const rawFileName = pathname.split("/").pop() || "download";
        return sanitizeWindowsFilename(decodeURIComponent(rawFileName));
    } catch {
        return "download";
    }
}

export function createSiblingTempPath(targetPath: string, suffix: string) {
    const parentPath = path.dirname(targetPath);
    const baseName = path.basename(targetPath);

    return path.join(parentPath, `${baseName}.${suffix}-${nanoid()}`);
}

export function getDownloadTempExtension(fileName: string) {
    const archiveExt = fileName.match(/\.(tar\.gz|tar\.bz2|tar\.xz|tgz|tbz2|txz)$/i)?.[0];
    return archiveExt ?? (path.extname(fileName) || ".download");
}

export function getStagingPaths(
    fileName: string,
    sanitizeWindowsFilename: (name: string) => string,
) {
    const stagingRoot = path.join(os.tmpdir(), "nahida-desktop-downloads");
    const stagingPath = path.join(
        stagingRoot,
        `${sanitizeWindowsFilename(fileName)}.staging-${nanoid()}`,
    );
    const stagedDownloadPath = path.join(stagingPath, fileName);

    return {
        stagingPath,
        stagedDownloadPath,
    };
}

export function getPreviewTargetDir(stagedPath: string) {
    return path.extname(stagedPath) ? path.dirname(stagedPath) : stagedPath;
}

export function getArchiveRootName(
    fileName: string,
    sanitizeWindowsFilename: (name: string) => string,
) {
    const sanitized = sanitizeWindowsFilename(fileName);
    const withoutArchiveExt = sanitized.replace(/\.(zip|7z|rar)$/i, "");
    return withoutArchiveExt || sanitized;
}
