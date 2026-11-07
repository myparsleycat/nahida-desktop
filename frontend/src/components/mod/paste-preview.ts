import { Mod } from "@bindings/mod";
import { Shell } from "@bindings/platform";
import i18n from "@renderer/lib/i18n";
import { Logger } from "@renderer/lib/logger";
import type { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { isPreviewImagePath } from "./preview-media";

export type ClipboardPreview =
    | { kind: "path"; path: string }
    | { kind: "url"; url: string }
    | { kind: "blob"; blob: Blob };

export async function readClipboardPreview(): Promise<ClipboardPreview | null> {
    const files = (await Shell.GetClipboardFiles()) ?? [];
    if (files.length > 0 && isPreviewImagePath(files[0])) {
        return { kind: "path", path: files[0] };
    }

    const text = await navigator.clipboard.readText();
    if (text.startsWith("http") && isPreviewImagePath(text)) {
        return { kind: "url", url: text };
    }

    for (const item of await navigator.clipboard.read()) {
        const type = item.types.find((value) => value === "image/png" || value === "image/jpeg");
        if (type) {
            return { kind: "blob", blob: await item.getType(type) };
        }
    }
    return null;
}

interface SavePastedPreviewOptions {
    modPath: string;
    preview: ClipboardPreview;
    selectedGroupPath?: string;
    queryClient: QueryClient;
}

export function savePastedPreview({
    modPath,
    preview,
    selectedGroupPath,
    queryClient,
}: SavePastedPreviewOptions) {
    const invalidate = () =>
        void queryClient.invalidateQueries({
            queryKey: ["modGroup", selectedGroupPath],
        });

    if (preview.kind === "path") {
        Mod.PastePreview(modPath, preview.path, "path", null)
            .then(invalidate)
            .catch((error) => {
                Logger.capture("components/mod/paste-preview.ts", error);
                toast.error(i18n.t("page.mod.toast.paste-preview.copy-error"));
            });
        return;
    }

    if (preview.kind === "url") {
        // The download can take a while, so the loading toast stays until the preview itself shows the result.
        const id = toast.loading(i18n.t("page.mod.toast.paste-preview.downloading"));
        Mod.PastePreview(modPath, preview.url, "url", null)
            .then(() => {
                toast.dismiss(id);
                invalidate();
            })
            .catch((error) => {
                Logger.capture("components/mod/paste-preview.ts", error);
                toast.error(i18n.t("page.mod.toast.paste-preview.download-error"), { id });
            });
        return;
    }

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

        Mod.PastePreview(modPath, base64data, "base64", null)
            .then(invalidate)
            .catch((error) => {
                Logger.capture("components/mod/paste-preview.ts", error);
                toast.error(i18n.t("page.mod.toast.paste-preview.save-error"));
            });
    };
    reader.readAsDataURL(preview.blob);
}
