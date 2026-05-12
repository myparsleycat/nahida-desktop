import i18n from "@renderer/lib/i18n";
import type { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

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

interface PasteModPreviewOptions {
    modPath: string;
    selectedGroupPath?: string;
    queryClient: QueryClient;
}

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

export async function pasteModPreview({
    modPath,
    selectedGroupPath,
    queryClient,
}: PasteModPreviewOptions) {
    try {
        const files = await window.api.invoke("util:getClipboardFiles");
        if (files.length > 0) {
            const filePath = files[0];
            if (isPreviewImagePath(filePath)) {
                const promise = window.api.invoke("mod:pastePreview", modPath, filePath, "path");
                toast.promise(promise, {
                    loading: i18n.t("page.mod.toast.paste-preview.copying"),
                    success: i18n.t("page.mod.toast.paste-preview.success"),
                    error: i18n.t("page.mod.toast.paste-preview.copy-error"),
                });
                promise
                    .then(() => {
                        queryClient.invalidateQueries({
                            queryKey: ["modGroup", selectedGroupPath],
                        });
                    })
                    .catch((error) => {
                        console.error(error);
                    });
                return;
            }
        }

        const text = await navigator.clipboard.readText();
        if (text?.startsWith("http") && isPreviewImagePath(text)) {
            const promise = window.api.invoke("mod:pastePreview", modPath, text, "url");
            toast.promise(promise, {
                loading: i18n.t("page.mod.toast.paste-preview.downloading"),
                success: i18n.t("page.mod.toast.paste-preview.success"),
                error: i18n.t("page.mod.toast.paste-preview.download-error"),
            });
            promise
                .then(() => {
                    queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
                })
                .catch((error) => {
                    console.error(error);
                });
            return;
        }

        const items = await navigator.clipboard.read();
        for (const item of items) {
            if (item.types.includes("image/png") || item.types.includes("image/jpeg")) {
                const type = item.types.find((t) => t.startsWith("image/"));
                if (!type) {
                    continue;
                }

                const blob = await item.getType(type);
                const reader = new FileReader();
                reader.onerror = () => {
                    console.error(reader.error);
                    toast.error(i18n.t("page.mod.toast.paste-preview.save-error"));
                };
                reader.onloadend = () => {
                    const base64data = reader.result;
                    if (typeof base64data !== "string") {
                        console.error("Failed to read clipboard image as data URL");
                        toast.error(i18n.t("page.mod.toast.paste-preview.save-error"));
                        return;
                    }

                    const promise = window.api.invoke(
                        "mod:pastePreview",
                        modPath,
                        base64data,
                        "base64",
                    );
                    toast.promise(promise, {
                        loading: i18n.t("page.mod.toast.paste-preview.saving"),
                        success: i18n.t("page.mod.toast.paste-preview.success"),
                        error: i18n.t("page.mod.toast.paste-preview.save-error"),
                    });
                    promise
                        .then(() => {
                            queryClient.invalidateQueries({
                                queryKey: ["modGroup", selectedGroupPath],
                            });
                        })
                        .catch((error) => {
                            console.error(error);
                        });
                };
                reader.readAsDataURL(blob);
                return;
            }
        }

        toast.warning(i18n.t("page.mod.toast.paste-preview.no-image"));
    } catch (error) {
        console.error(error);
        toast.error(i18n.t("page.mod.toast.paste-preview.clipboard-error"));
    }
}
