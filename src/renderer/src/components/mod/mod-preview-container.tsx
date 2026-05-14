import { Button } from "@renderer/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import type { ModInfo } from "@renderer/types/mod";
import { ClipboardIcon, ImageIcon, TrashIcon } from "lucide-react";
import type { SyntheticEvent } from "react";
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
          <ImageIcon className="w-12 h-12 text-muted-foreground/50" />
          <div className="flex flex-col items-center gap-1">
            <span className="text-sm text-muted-foreground">{t("page.mod.no-preview")}</span>
            <Button
              variant="outline"
              size="sm"
              className="h-7 text-xs gap-1"
              onClick={handlePasteClick}
            >
              <ClipboardIcon className="w-3 h-3" />
              {t("page.mod.context-menu.paste-preview")}
            </Button>
          </div>
        </div>
      }
    />
  );

  return (
    <div className="flex-1 p-2 flex items-center justify-center relative overflow-hidden">
      {mod.preview ? (
        <ContextMenu>
          <ContextMenuTrigger>{previewContent}</ContextMenuTrigger>
          <ContextMenuContent onClick={(e) => e.stopPropagation()}>
            <ContextMenuItem
              onClick={() => {
                if (!mod.preview) return;
                window.api.invoke("util:openExternal", mod.preview).catch((error) => {
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
    </div>
  );
}
