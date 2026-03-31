import type { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

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
    const files = await window.api.invoke("util:getClipboardFiles");
    if (files.length > 0) {
      const filePath = files[0];
      if (filePath.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i)) {
        const promise = window.api.invoke("mod:pastePreview", modPath, filePath, "path");
        toast.promise(promise, {
          loading: "Copying preview...",
          success: "Preview updated",
          error: "Failed to copy preview",
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
        loading: "Downloading preview...",
        success: "Preview updated",
        error: "Failed to download preview",
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
            loading: "Saving preview...",
            success: "Preview updated",
            error: "Failed to save preview",
          });
          promise.then(() => {
            queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
          });
        };
        reader.readAsDataURL(blob);
        return;
      }
    }

    toast.warning("클립보드에 이미지, 이미지 URL, 또는 이미지 파일이 없습니다.");
  } catch (error) {
    console.error(error);
    toast.error("클립보드를 읽는데 실패했습니다.");
  }
}
