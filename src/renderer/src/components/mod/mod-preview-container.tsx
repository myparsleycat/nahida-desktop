import { Button } from "@renderer/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import { ClipboardIcon, ImageIcon, TrashIcon } from "lucide-react";
import { toast } from "sonner";
import { Preview } from "./preview";

interface ModPreviewContainerProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  onPaste: (e: React.MouseEvent) => void;
}

export function ModPreviewContainer({ mod, selectedGroupPath, onPaste }: ModPreviewContainerProps) {
  const { queryClient } = useRouteContext({ from: "__root__" });

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
            <span className="text-sm text-muted-foreground">No Preview</span>
            <Button variant="outline" size="sm" className="h-7 text-xs gap-1" onClick={onPaste}>
              <ClipboardIcon className="w-3 h-3" />
              Paste
            </Button>
          </div>
        </div>
      }
    />
  );

  const handleDelete = () => {
    if (!mod.preview) return;
    const promise = window.api.invoke("util:fs:trash", mod.preview);
    toast.promise(promise, {
      loading: "휴지통으로 이동 중...",
      success: "삭제 완료",
      error: "삭제 실패",
    });
    promise.then(() => {
      queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
    });
  };

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
              뷰어로 열기
            </ContextMenuItem>
            <ContextMenuItem onClick={handleDelete}>
              <TrashIcon />
              프리뷰 삭제
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ) : (
        previewContent
      )}
    </div>
  );
}
