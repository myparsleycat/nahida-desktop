import { Button } from "@renderer/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import { ClipboardIcon, ImageIcon, TrashIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Preview } from "./preview";

interface ModPreviewContainerProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  onPaste: () => void;
}

export function ModPreviewContainer({ mod, selectedGroupPath, onPaste }: ModPreviewContainerProps) {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();
  const handlePasteClick = (e?: React.SyntheticEvent) => {
    e?.stopPropagation();
    onPaste();
  };

  const previewContent = (
    <Preview
      path={mod.preview}
      alt={mod.name}
      objectFit="contain"
      className="absolute inset-0"
      fallback={
        <div className="flex flex-col items-center justify-center gap-2">
          <ImageIcon className="w-12 h-12 text-muted-foreground/50" />
          <div className="flex flex-col items-center gap-1">
            <span className="text-sm text-muted-foreground">{t("page.mod.no-preview")}</span>
            <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={handlePasteClick}>
              <ClipboardIcon className="w-3 h-3" />
              {t("page.mod.context-menu.paste-preview")}
            </Button>
          </div>
        </div>
      }
    />
  );

  const handleDelete = () => {
    if (!mod.preview) return;
    confirmTrash({
      path: mod.preview,
      title: t("page.mod.dialog.delete-preview.title"),
      description: t("page.mod.dialog.delete-preview.description", { name: mod.name }),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
      },
    });
  };

  return (
    <div className="flex-1 p-2 flex items-center justify-center relative overflow-hidden">
      {mod.preview ? (
        <ContextMenu>
          <ContextMenuTrigger>{previewContent}</ContextMenuTrigger>
          <ContextMenuContent onClick={(e) => e.stopPropagation()}>
            <ContextMenuItem onClick={handlePasteClick}>
              <ClipboardIcon />
              {t("page.mod.context-menu.paste-preview")}
            </ContextMenuItem>
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
            <ContextMenuItem variant="destructive" onClick={handleDelete}>
              <TrashIcon />
              {t("page.mod.context-menu.delete-preview")}
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        previewContent
      )}
      {confirmTrashDialog}
    </div>
  );
}
