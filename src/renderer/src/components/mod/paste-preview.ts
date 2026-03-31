import i18n from "@renderer/lib/i18n";
import type { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

interface PasteModPreviewOptions {
  modPath: string;
  selectedGroupPath?: string;
  queryClient: QueryClient;
}

export function hasModPreviewFile(modPath: string, previewPath?: string) {
  if (!previewPath) return false;

  const normalizedModPath = modPath.replace(/\\/g, "/").replace(/\/+$/, "");
  const normalizedPreviewPath = previewPath.replace(/\\/g, "/");

  if (!normalizedPreviewPath.startsWith(`${normalizedModPath}/`)) {
    return false;
  }

  const relativePreviewPath = normalizedPreviewPath.slice(normalizedModPath.length + 1);
  return /^preview\.[^/]+$/i.test(relativePreviewPath);
}

export async function pasteModPreview({
  modPath,
  selectedGroupPath,
  queryClient,
}: PasteModPreviewOptions) {
  try {
    const files = await window.api.invoke("util:getClipboardFiles");
    if (files.length > 0) {
      const filePath = files[0];
      if (filePath.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i)) {
        const promise = window.api.invoke("mod:pastePreview", modPath, filePath, "path");
        toast.promise(promise, {
          loading: i18n.t("page.mod.toast.paste-preview.copying"),
          success: i18n.t("page.mod.toast.paste-preview.success"),
          error: i18n.t("page.mod.toast.paste-preview.copy-error"),
        });
        promise.then(() => {
          queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
        });
        return;
      }
    }

    const text = await navigator.clipboard.readText();
    if (text?.startsWith("http") && text.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i)) {
      const promise = window.api.invoke("mod:pastePreview", modPath, text, "url");
      toast.promise(promise, {
        loading: i18n.t("page.mod.toast.paste-preview.downloading"),
        success: i18n.t("page.mod.toast.paste-preview.success"),
        error: i18n.t("page.mod.toast.paste-preview.download-error"),
      });
      promise.then(() => {
        queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
      });
      return;
    }

    const items = await navigator.clipboard.read();
    for (const item of items) {
      if (item.types.includes("image/png") || item.types.includes("image/jpeg")) {
        const blob = await item.getType(item.types.find((t) => t.startsWith("image/"))!);
        const reader = new FileReader();
        reader.onloadend = () => {
          const base64data = reader.result as string;
          const promise = window.api.invoke("mod:pastePreview", modPath, base64data, "base64");
          toast.promise(promise, {
            loading: i18n.t("page.mod.toast.paste-preview.saving"),
            success: i18n.t("page.mod.toast.paste-preview.success"),
            error: i18n.t("page.mod.toast.paste-preview.save-error"),
          });
          promise.then(() => {
            queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
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
