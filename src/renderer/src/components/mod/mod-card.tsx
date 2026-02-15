import { Badge } from "@renderer/components/ui/badge";
import { Separator } from "@renderer/components/ui/separator";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { formatDate, formatSize } from "@shared/utils";
import { useRouteContext } from "@tanstack/react-router";
import { CalendarIcon, FolderIcon } from "lucide-react";
import { memo, useRef } from "react";
import { toast } from "sonner";
import { ModCardHeader } from "./mod-card-header";
import { ModIniList } from "./mod-ini-list";
import { ModPreviewContainer } from "./mod-preview-container";
import { getModColorClass } from "./utils";

interface ModCardProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  onToggle: (mod: ModInfo, event?: React.MouseEvent) => void;
  onToggleKeyUpdate: (
    modPath: string,
    iniFileName: string,
    sectionName: string,
    variable: string,
    value: string,
  ) => void;
}

export const ModCard = memo(function ModCard({
  mod,
  selectedGroupPath,
  onToggle,
  onToggleKeyUpdate,
}: ModCardProps) {
  const { queryClient } = useRouteContext({ from: "__root__" });
  const mouseDownTargetRef = useRef<EventTarget | null>(null);

  const handlePaste = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      const files = await window.api.invoke("util:getClipboardFiles");
      if (files.length > 0) {
        const filePath = files[0];
        if (filePath.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i)) {
          const promise = window.api.invoke("mod:pastePreview", mod.path, filePath, "path");
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
      if (text?.startsWith("http")) {
        if (text.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i)) {
          const promise = window.api.invoke("mod:pastePreview", mod.path, text, "url");
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
      }

      const items = await navigator.clipboard.read();
      for (const item of items) {
        if (item.types.includes("image/png") || item.types.includes("image/jpeg")) {
          const blob = await item.getType(item.types.find((t) => t.startsWith("image/"))!);
          const reader = new FileReader();
          reader.onloadend = () => {
            const base64data = reader.result as string;
            const promise = window.api.invoke("mod:pastePreview", mod.path, base64data, "base64");
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
  };

  return (
    <div
      className={cn(
        "rounded-sm overflow-hidden border-border/75 cursor-pointer p-1 h-[400px] relative hover:shadow-lg transition-shadow duration-150",
        getModColorClass(mod.isEnabled),
      )}
      onMouseDown={(e) => {
        mouseDownTargetRef.current = e.target;
      }}
      onClick={(e) => {
        const target = mouseDownTargetRef.current as HTMLElement;
        if (target && (target.tagName === "INPUT" || target.closest("button"))) {
          return;
        }
        onToggle(mod, e);
      }}
      draggable={false}
    >
      {mod.preview?.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i) && (
        <div
          className="absolute inset-0 z-0 blur-lg scale-110 pointer-events-none opacity-25"
          style={{ transform: "translateZ(0)", willChange: "filter" }}
        >
          <img src={`local://${mod.preview}`} alt="preview" className="w-full h-full object-fill" />
        </div>
      )}

      <ModCardHeader mod={mod} selectedGroupPath={selectedGroupPath} />

      <div className="flex flex-row h-[calc(100%-2rem)] space-x-2 relative z-10">
        <ModPreviewContainer
          mod={mod}
          selectedGroupPath={selectedGroupPath}
          onPaste={handlePaste}
        />

        {mod.inis.length > 0 && (
          <>
            <Separator orientation="vertical" />
            <ModIniList mod={mod} onToggleKeyUpdate={onToggleKeyUpdate} />
          </>
        )}
      </div>

      <div className="absolute left-1 bottom-1 flex flex-col space-y-1 z-10">
        <Badge
          className="bg-background/35 backdrop-blur text-foreground text-xs h-5 flex items-center gap-1.5"
          style={{ transform: "translateZ(0)", willChange: "backdrop-filter" }}
        >
          <FolderIcon />
          {formatSize(mod.size)}
        </Badge>
        <Badge
          className="bg-background/35 backdrop-blur text-foreground text-xs h-5 flex items-center gap-1.5"
          style={{ transform: "translateZ(0)", willChange: "backdrop-filter" }}
        >
          <CalendarIcon />
          {formatDate(new Date(mod.mtime), "ko")}
        </Badge>
      </div>
    </div>
  );
});
