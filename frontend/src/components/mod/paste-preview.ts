import { Mod } from "@bindings/mod";
import { Shell } from "@bindings/platform";
import i18n from "@renderer/lib/i18n";
import { Logger } from "@renderer/lib/logger";
import type { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { isPreviewImagePath } from "./preview-media";

interface PasteModPreviewOptions {
    modPath: string;
    selectedGroupPath?: string;
    queryClient: QueryClient;
}

export async function pasteModPreview({
    modPath,
    selectedGroupPath,
    queryClient,
}: PasteModPreviewOptions) {
    try {
        const files = (await Shell.GetClipboardFiles()) ?? [];
        if (files.length > 0) {
            const filePath = files[0];
            if (isPreviewImagePath(filePath)) {
                const promise = Mod.PastePreview(modPath, filePath, "path", null);
                toast.promise(promise, {
                    loading: i18n.t("page.mod.toast.paste-preview.copying"),
                    success: i18n.t("page.mod.toast.paste-preview.success"),
                    error: i18n.t("page.mod.toast.paste-preview.copy-error"),
                });
                promise
                    .then(() => {
                        void queryClient.invalidateQueries({
                            queryKey: ["modGroup", selectedGroupPath],
                        });
                    })
                    .catch((error) => {
                        Logger.capture("components/mod/paste-preview.ts", error);
                    });
                return;
            }
        }

        const text = await navigator.clipboard.readText();
        if (text?.startsWith("http") && isPreviewImagePath(text)) {
            const promise = Mod.PastePreview(modPath, text, "url", null);
            toast.promise(promise, {
                loading: i18n.t("page.mod.toast.paste-preview.downloading"),
                success: i18n.t("page.mod.toast.paste-preview.success"),
                error: i18n.t("page.mod.toast.paste-preview.download-error"),
            });
            promise
                .then(() => {
                    void queryClient.invalidateQueries({
                        queryKey: ["modGroup", selectedGroupPath],
                    });
                })
                .catch((error) => {
                    Logger.capture("components/mod/paste-preview.ts", error);
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
                    Logger.capture("components/mod/paste-preview.ts", reader.error);
                    toast.error(i18n.t("page.mod.toast.paste-preview.save-error"));
                };
                reader.onloadend = () => {
                    const base64data = reader.result;
                    if (typeof base64data !== "string") {
                        Logger.capture(
                            "components/mod/paste-preview.ts",
                            "Failed to read clipboard image as data URL",
                        );
                        toast.error(i18n.t("page.mod.toast.paste-preview.save-error"));
                        return;
                    }

                    const promise = Mod.PastePreview(modPath, base64data, "base64", null);
                    toast.promise(promise, {
                        loading: i18n.t("page.mod.toast.paste-preview.saving"),
                        success: i18n.t("page.mod.toast.paste-preview.success"),
                        error: i18n.t("page.mod.toast.paste-preview.save-error"),
                    });
                    promise
                        .then(() => {
                            void queryClient.invalidateQueries({
                                queryKey: ["modGroup", selectedGroupPath],
                            });
                        })
                        .catch((error) => {
                            Logger.capture("components/mod/paste-preview.ts", error);
                        });
                };
                reader.readAsDataURL(blob);
                return;
            }
        }

        toast.warning(i18n.t("page.mod.toast.paste-preview.no-image"));
    } catch (error) {
        Logger.capture("components/mod/paste-preview.ts", error);
        toast.error(i18n.t("page.mod.toast.paste-preview.clipboard-error"));
    }
}
