import { Badge } from "@renderer/components/ui/badge";
import { Separator } from "@renderer/components/ui/separator";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { formatDate, formatSize } from "@shared/utils";
import { useRouteContext } from "@tanstack/react-router";
import { CalendarIcon, FolderIcon } from "lucide-react";
import { memo, useRef, useState } from "react";
import { ModCardHeader } from "./mod-card-header";
import { ModContextMenu } from "./mod-context-menu";
import { ModIniList } from "./mod-ini-list";
import { pasteModPreview } from "./paste-preview";
import { ModPreviewContainer } from "./mod-preview-container";
import { TextureResizeDialog } from "./texture-resize-dialog";
import { getModColorClass } from "./utils";

interface ModCardProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  onToggle: (mod: ModInfo, event?: React.MouseEvent) => void;
  isIniListExpanded: boolean;
  onIniListExpandedChange: (modPath: string, isExpanded: boolean) => void;
  fixTools: {
    id: string;
    name: string;
    type: string;
    size: number;
  }[];
  presets: {
    id: string;
    name: string;
  }[];
  onToggleKeyUpdate: (
    modPath: string,
    iniPath: string,
    sectionName: string,
    variable: string,
    value: string,
  ) => void;
}

export const ModCard = memo(function ModCard({
  mod,
  selectedGroupPath,
  onToggle,
  isIniListExpanded,
  onIniListExpandedChange,
  fixTools,
  presets,
  onToggleKeyUpdate,
}: ModCardProps) {
  const { queryClient } = useRouteContext({ from: "__root__" });
  const mouseDownTargetRef = useRef<EventTarget | null>(null);
  const [showTextureResizeDialog, setShowTextureResizeDialog] = useState(false);

  const handlePaste = () => pasteModPreview({ modPath: mod.path, selectedGroupPath, queryClient });
  const handleOpenTextureResizeDialog = () => setShowTextureResizeDialog(true);

  return (
    <>
      <ModContextMenu
        mod={mod}
        selectedGroupPath={selectedGroupPath}
        fixTools={fixTools}
        presets={presets}
        onOpenTextureResizeDialog={handleOpenTextureResizeDialog}
        onPaste={handlePaste}
      >
        <div
          className={cn(
            "rounded-sm overflow-hidden border-border/75 cursor-pointer p-1 h-100 relative hover:shadow-lg transition-shadow duration-150",
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
              <img
                src={`local://${mod.preview}`}
                alt="preview"
                className="w-full h-full object-fill"
              />
            </div>
          )}

          <ModCardHeader
            mod={mod}
            selectedGroupPath={selectedGroupPath}
            onOpenTextureResizeDialog={(e) => {
              e.stopPropagation();
              handleOpenTextureResizeDialog();
            }}
          />

          <div className="flex flex-row h-[calc(100%-2rem)] space-x-2 relative z-10">
            <ModPreviewContainer
              mod={mod}
              selectedGroupPath={selectedGroupPath}
              onPaste={handlePaste}
            />

            {mod.inis.length > 0 && (
              <>
                <div className="relative flex items-stretch">
                  <Separator orientation="vertical" />
                  <button
                    type="button"
                    aria-label={isIniListExpanded ? "Collapse ini list" : "Expand ini list"}
                    className="absolute inset-y-0 left-1/2 z-10 w-6 -translate-x-1/2 bg-transparent pointer-events-auto"
                    style={{ cursor: "col-resize" }}
                    onClick={(e) => {
                      e.stopPropagation();
                      onIniListExpandedChange(mod.path, !isIniListExpanded);
                    }}
                  />
                </div>

                <ModIniList
                  mod={mod}
                  expanded={isIniListExpanded}
                  onToggleKeyUpdate={onToggleKeyUpdate}
                />
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
      </ModContextMenu>

      <TextureResizeDialog
        open={showTextureResizeDialog}
        onOpenChange={setShowTextureResizeDialog}
        modPath={mod.path}
        modName={mod.name}
      />
    </>
  );
});
