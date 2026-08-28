export function localFileSrc(
    path: string,
    options?: { orig?: boolean; cacheKey?: string | number },
): string {
    if (isSafeMediaUrl(path)) {
        return path;
    }

    if (path.startsWith("model-viewer-memory://")) {
        return `/protocol/model-viewer-memory/${path.slice("model-viewer-memory://".length)}`;
    }

    const filePath = filesystemPathFromMediaSource(path);
    const params = new URLSearchParams({ path: filePath });
    if (options?.orig) {
        params.set("orig", "true");
    }
    if (options?.cacheKey !== undefined) {
        params.set("v", String(options.cacheKey));
    }
    return `/protocol/local?${params}`;
}

function isSafeMediaUrl(value: string): boolean {
    return (
        value.startsWith("/protocol/") ||
        value.startsWith("blob:") ||
        value.startsWith("http://") ||
        value.startsWith("https://")
    );
}

function filesystemPathFromMediaSource(value: string): string {
    if (value.startsWith("local://")) {
        const withoutScheme = decodeURI(value.slice("local://".length));
        const queryIndex = withoutScheme.search(/[?#]/);
        return queryIndex >= 0 ? withoutScheme.slice(0, queryIndex) : withoutScheme;
    }

    if (value.startsWith("file:")) {
        return fileUrlToPath(value);
    }

    return value;
}

function fileUrlToPath(value: string): string {
    try {
        const parsed = new URL(value);
        let pathname = decodeURIComponent(parsed.pathname);
        if (/^\/[A-Za-z]:/.test(pathname)) {
            pathname = pathname.slice(1);
        }
        return pathname;
    } catch {
        return decodeURI(value.replace(/^file:\/\//, ""));
    }
}
