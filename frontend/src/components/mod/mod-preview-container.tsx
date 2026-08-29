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
import { localFileSrc } from "@renderer/lib/local-file";
import type { ModInfo } from "@renderer/types/mod";
import { ClipboardIcon, ImageIcon, TrashIcon, ZoomInIcon } from "lucide-react";
import { type SyntheticEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { Preview } from "./preview";

interface ModPreviewContainerProps {
  mod: ModInfo;
  onDeletePreview: () => void;
  onPaste: () => void;
}

export function ModPreviewContainer({ mod, onDeletePreview, onPaste }: ModPreviewContainerProps) {
  const { t } = useTranslation();
  const [lightboxOpen, setLightboxOpen] = useState(false);

  const handlePasteClick = (e?: SyntheticEvent) => {
    e?.stopPropagation();
    onPaste();
  };

  const previewContent = (
    <Preview
      path={mod.preview}
      alt={mod.name}
      cacheKey={mod.mtime}
      objectFit="contain"
      className="absolute inset-0"
      fallback={
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
      }
    />
  );

  return (
    <div className="relative flex flex-1 items-center justify-center overflow-hidden p-2">
      {mod.preview ? (
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
      ) : (
        previewContent
      )}
      {mod.preview && (
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
