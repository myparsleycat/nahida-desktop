import { Mod } from "@bindings/mod";
import { Shell } from "@bindings/platform";
import { Button } from "@renderer/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import { PreviewLightbox } from "@renderer/components/ui/preview-lightbox";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { localFileSrc } from "@renderer/lib/local-file";
import type { FolderGroup, ModInfo } from "@renderer/types/mod";
import { useQueryClient } from "@tanstack/react-query";
import {
  BoxIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ClipboardIcon,
  ImageIcon,
  TrashIcon,
  ZoomInIcon,
} from "lucide-react";
import { type SyntheticEvent, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { useGridModelPreview } from "./grid-model-preview";
import { Preview } from "./preview";
import { isPreviewMediaPath } from "./preview-media";

interface ModPreviewContainerProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  modelPreviewEligible: boolean;
  onDeletePreview: () => void;
  onOpenModelViewer: () => void;
  onPaste: () => void;
}

export function ModPreviewContainer({
  mod,
  selectedGroupPath,
  modelPreviewEligible,
  onDeletePreview,
  onOpenModelViewer,
  onPaste,
}: ModPreviewContainerProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [savingPreview, setSavingPreview] = useState(false);
  const savingRef = useRef(false);
  const images = mod.previewImages ?? [];
  const hasMediaPreview = Boolean(mod.preview && isPreviewMediaPath(mod.preview));
  const [observeModelPreview, modelPreviewEnabled, modelPreviewState] = useGridModelPreview(
    mod,
    modelPreviewEligible && !hasMediaPreview,
  );

  const handlePasteClick = (e?: SyntheticEvent) => {
    e?.stopPropagation();
    onPaste();
  };

  const changePreview = async (direction: -1 | 1) => {
    if (savingRef.current || images.length < 2) return;

    const currentIndex = images.findIndex((image) => image === mod.preview);
    const nextIndex =
      currentIndex === -1
        ? direction === 1
          ? 0
          : images.length - 1
        : (currentIndex + direction + images.length) % images.length;
    const imagePath = images[nextIndex];
    savingRef.current = true;
    setSavingPreview(true);
    try {
      await Mod.SetDefaultPreview(mod.path, imagePath);
      if (selectedGroupPath) {
        for (const key of ["modGroup", "modGroupLight"]) {
          queryClient.setQueryData<FolderGroup>([key, selectedGroupPath], (group) =>
            group
              ? {
                  ...group,
                  mods: group.mods.map((item) =>
                    item.path === mod.path ? { ...item, preview: imagePath } : item,
                  ),
                }
              : group,
          );
          void queryClient.invalidateQueries({ queryKey: [key, selectedGroupPath] });
        }
      }
    } catch {
      toast.error(t("page.mod.toast.change-preview-error"));
    } finally {
      savingRef.current = false;
      setSavingPreview(false);
    }
  };

  const fallback = (
    <div className="flex flex-col items-center justify-center gap-2">
      <ImageIcon className="h-12 w-12 text-muted-foreground/50" />
      <div className="flex flex-col items-center gap-1">
        <span className="text-sm text-muted-foreground">{t("page.mod.no-preview")}</span>
        <Button
          variant="outline"
          size="sm"
          className="h-7 gap-1 text-xs"
          onClick={handlePasteClick}
        >
          <ClipboardIcon className="h-3 w-3" />
          {t("page.mod.context-menu.paste-preview")}
        </Button>
      </div>
    </div>
  );

  const previewContent = hasMediaPreview ? (
    <Preview
      path={mod.preview}
      alt={mod.name}
      cacheKey={mod.mtime}
      objectFit="contain"
      className="absolute inset-0"
      fallback={fallback}
    />
  ) : modelPreviewState.status === "ready" ? (
    <img
      src={modelPreviewState.url}
      alt={mod.name}
      className="pointer-events-none absolute inset-0 h-full w-full object-contain"
      draggable={false}
    />
  ) : modelPreviewState.status === "loading" ? (
    <Skeleton className="absolute inset-0 h-full w-full rounded-none" />
  ) : (
    fallback
  );

  return (
    <div
      ref={observeModelPreview}
      className="relative flex flex-1 items-center justify-center overflow-hidden p-2"
    >
      {hasMediaPreview ? (
        <ContextMenu>
          <ContextMenuTrigger>{previewContent}</ContextMenuTrigger>
          <ContextMenuContent onClick={(e) => e.stopPropagation()}>
            <ContextMenuItem onClick={() => setLightboxOpen(true)}>
              <ZoomInIcon />
              {t("page.mod.context-menu.open-preview-lightbox")}
            </ContextMenuItem>

            <ContextMenuItem
              onClick={() => {
                if (!mod.preview) return;
                Shell.OpenExternal(mod.preview).catch((error) => {
                  toast.error("Failed to open external", {
                    description: error.message,
                  });
                });
              }}
            >
              <ImageIcon />
              {t("page.mod.context-menu.open-preview-viewer")}
            </ContextMenuItem>

            <ContextMenuSeparator />

            <ContextMenuItem onClick={handlePasteClick}>
              <ClipboardIcon />
              {t("page.mod.context-menu.paste-preview")}
            </ContextMenuItem>

            <ContextMenuItem variant="destructive" onClick={onDeletePreview}>
              <TrashIcon />
              {t("page.mod.context-menu.delete-preview")}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : modelPreviewEnabled ? (
        <ContextMenu>
          <ContextMenuTrigger>{previewContent}</ContextMenuTrigger>
          <ContextMenuContent onClick={(event) => event.stopPropagation()}>
            <ContextMenuItem onClick={onOpenModelViewer}>
              <BoxIcon />
              {t("page.mod.context-menu.open-model-viewer")}
            </ContextMenuItem>

            <ContextMenuSeparator />

            <ContextMenuItem onClick={handlePasteClick}>
              <ClipboardIcon />
              {t("page.mod.context-menu.paste-preview")}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        previewContent
      )}
      {hasMediaPreview && images.length > 1 && (
        <div className="pointer-events-none absolute inset-x-2 top-1/2 z-20 flex -translate-y-1/2 justify-between">
          {([-1, 1] as const).map((direction) => (
            <Button
              key={direction}
              type="button"
              variant="secondary"
              size="icon"
              aria-label={t(
                direction === -1 ? "page.mod.previous-preview" : "page.mod.next-preview",
              )}
              className="pointer-events-auto size-8 rounded-full bg-background/70 shadow-sm"
              disabled={savingPreview}
              onClick={(event) => {
                event.stopPropagation();
                void changePreview(direction);
              }}
              onPointerDown={(event) => event.stopPropagation()}
              onContextMenu={(event) => event.stopPropagation()}
            >
              {direction === -1 ? <ChevronLeftIcon /> : <ChevronRightIcon />}
            </Button>
          ))}
        </div>
      )}
      {hasMediaPreview && mod.preview && (
        <PreviewLightbox
          open={lightboxOpen}
          onOpenChange={setLightboxOpen}
          thumbnailSrc={localFileSrc(mod.preview, { cacheKey: mod.mtime })}
          fullSrc={localFileSrc(mod.preview, { orig: true, cacheKey: mod.mtime })}
          isVideo={/\.(mp4|webm|ogg)$/i.test(mod.preview)}
          alt={mod.name}
        />
      )}
    </div>
  );
}
