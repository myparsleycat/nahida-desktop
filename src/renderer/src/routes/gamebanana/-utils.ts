import type { PreviewImage, PreviewMedia, SubmissionListItem } from "./-types";

export function formatEpoch(epochSeconds: number, language: string) {
    return new Intl.DateTimeFormat(language, {
        year: "numeric",
        month: "short",
        day: "numeric",
    }).format(new Date(epochSeconds * 1000));
}

export function formatNumber(value: number, language: string) {
    return new Intl.NumberFormat(language).format(value);
}

export function getSubmissionTimestamp(
    submission: Pick<SubmissionListItem, "_tsDateAdded" | "_tsDateModified" | "_tsDateUpdated">,
) {
    return submission._tsDateAdded ?? submission._tsDateModified ?? submission._tsDateUpdated;
}

export function getSubmissionDateKey(
    submission: Pick<
        SubmissionListItem,
        "_idRow" | "_tsDateAdded" | "_tsDateModified" | "_tsDateUpdated"
    >,
) {
    return getSubmissionTimestamp(submission) ?? submission._idRow;
}

export function getSubmissionPreviewUrl(submission: { _aPreviewMedia?: PreviewMedia }) {
    const preview = submission._aPreviewMedia?._aImages?.[0];
    return preview ? resolvePreviewImageUrl(preview, "preview") : undefined;
}

export function getSubmissionFullPreviewUrl(submission: { _aPreviewMedia?: PreviewMedia }) {
    const preview = submission._aPreviewMedia?._aImages?.[0];
    return preview ? resolvePreviewImageUrl(preview) : undefined;
}

export function getSubmissionPreviewImages(submission: { _aPreviewMedia?: PreviewMedia }) {
    const previews = submission._aPreviewMedia?._aImages ?? [];

    return previews
        .map((preview, index) => {
            const fullUrl = resolvePreviewImageUrl(preview);
            const previewUrl = fullUrl;
            const thumbnailUrl = fullUrl;
            const url = previewUrl ?? thumbnailUrl ?? fullUrl;
            if (!url) return null;

            return {
                id: `${index}-${url}`,
                previewUrl: url,
                fullUrl: fullUrl ?? url,
                thumbnailUrl: thumbnailUrl ?? url,
                alt: preview._sCaption?.trim() || `Preview image ${index + 1}`,
            };
        })
        .filter((preview): preview is NonNullable<typeof preview> => Boolean(preview));
}

function resolvePreviewImageUrl(preview: PreviewImage, variant: "full" | "preview" = "full") {
    const candidates =
        variant === "preview"
            ? [preview._sFile800, preview._sFile530, preview._sFile, preview._sUrl]
            : [preview._sFile, preview._sFile800, preview._sFile530, preview._sUrl];
    const absoluteCandidate = candidates.find((value) => isAbsoluteUrl(value));
    if (absoluteCandidate) return absoluteCandidate;

    const relativeCandidate = candidates.find((value) => Boolean(value));
    if (relativeCandidate && preview._sBaseUrl) {
        try {
            return new URL(relativeCandidate, ensureTrailingSlash(preview._sBaseUrl)).toString();
        } catch {
            return undefined;
        }
    }

    return isAbsoluteUrl(preview._sBaseUrl) ? preview._sBaseUrl : undefined;
}

function isAbsoluteUrl(value?: string) {
    return Boolean(value && /^https?:\/\//i.test(value));
}

function ensureTrailingSlash(value: string) {
    return value.endsWith("/") ? value : `${value}/`;
}
