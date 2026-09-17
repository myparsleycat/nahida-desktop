export const PREVIEW_IMAGE_EXTENSIONS = [
    ".png",
    ".jpg",
    ".jpeg",
    ".webp",
    ".avif",
    ".avifs",
    ".gif",
    ".bmp",
] as const;

export const PREVIEW_VIDEO_EXTENSIONS = [".mp4", ".webm", ".ogg", ".avi", ".mkv", ".mov"] as const;

export const PREVIEW_MEDIA_EXTENSIONS = [
    ...PREVIEW_IMAGE_EXTENSIONS,
    ...PREVIEW_VIDEO_EXTENSIONS,
] as const;

function getLowerCaseExtension(path: string) {
    const normalizedPath = path.replace(/\\/g, "/");
    const queryOrFragmentIndex = normalizedPath.search(/[?#]/);
    const cleanedPath =
        queryOrFragmentIndex === -1
            ? normalizedPath
            : normalizedPath.slice(0, queryOrFragmentIndex);
    const dotIndex = cleanedPath.lastIndexOf(".");

    if (dotIndex === -1) {
        return "";
    }

    return cleanedPath.slice(dotIndex).toLowerCase();
}

export function isPreviewImagePath(path: string) {
    return PREVIEW_IMAGE_EXTENSIONS.includes(
        getLowerCaseExtension(path) as (typeof PREVIEW_IMAGE_EXTENSIONS)[number],
    );
}

export function isPreviewMediaPath(path: string) {
    return PREVIEW_MEDIA_EXTENSIONS.includes(
        getLowerCaseExtension(path) as (typeof PREVIEW_MEDIA_EXTENSIONS)[number],
    );
}

export function isPreviewMediaFile(file: File) {
    return isPreviewMediaPath(file.name);
}

export function hasPreviewFile(basePath: string, previewPath?: string) {
    if (!previewPath) return false;

    const normalizedBasePath = basePath.replace(/\\/g, "/").replace(/\/+$/, "");
    const normalizedPreviewPath = previewPath.replace(/\\/g, "/");

    if (!normalizedPreviewPath.startsWith(`${normalizedBasePath}/`)) {
        return false;
    }

    const relativePreviewPath = normalizedPreviewPath.slice(normalizedBasePath.length + 1);
    return /^preview\.[^/]+$/i.test(relativePreviewPath);
}

export const hasModPreviewFile = hasPreviewFile;
