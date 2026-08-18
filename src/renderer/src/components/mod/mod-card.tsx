import { Badge } from "@renderer/components/ui/badge";
import { Separator } from "@renderer/components/ui/separator";
import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { formatDate, formatSize } from "@shared/utils";
import { CalendarIcon, FolderIcon } from "lucide-react";
import { memo, useCallback, useRef } from "react";

import { ModCardHeader } from "./mod-card-header";
import { ModContextMenu } from "./mod-context-menu";
import { ModIniList } from "./mod-ini-list";
import { ModPreviewContainer } from "./mod-preview-container";
import { getModColorClass } from "./utils";

interface ModCardProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  actions: ModActionApi;
  onToggle: (mod: ModInfo, event?: React.MouseEvent) => void;
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
  actions,
  onToggle,
  onToggleKeyUpdate,
}: ModCardProps) {
  const isMergeSelected = useModStore((s) => s.isMergeMode && s.selectedModPaths.has(mod.path));
  const setIniListExpanded = useModStore((s) => s.setIniListExpanded);
  const isIniListExpanded = useModStore((s) =>
    selectedGroupPath
      ? (s.iniListExpandedByGroupPath[selectedGroupPath]?.[mod.path] ?? true)
      : true,
  );
  const mouseDownTargetRef = useRef<EventTarget | null>(null);
  const handleIniListExpandedChange = useCallback(() => {
    if (!selectedGroupPath) {
      return;
    }

    setIniListExpanded(selectedGroupPath, mod.path, !isIniListExpanded);
  }, [isIniListExpanded, mod.path, selectedGroupPath, setIniListExpanded]);

  return (
    <>
      <ModContextMenu mod={mod} actions={actions}>
        <div
          className={cn(
            "relative h-100 cursor-pointer overflow-hidden rounded-sm border-border/75 p-1 transition-shadow duration-150 hover:shadow-lg",
            getModColorClass(mod.isEnabled),
            isMergeSelected && "ring-2 ring-primary ring-offset-2 ring-offset-background",
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
              className="pointer-events-none absolute inset-0 z-0 scale-110 opacity-25 blur-lg"
              style={{ transform: "translateZ(0)", willChange: "filter" }}
            >
              <img
                src={`local://${mod.preview}?v=${encodeURIComponent(String(mod.mtime))}`}
                alt="preview"
                className="h-full w-full object-fill"
              />
            </div>
          )}

          <ModCardHeader mod={mod} actions={actions} />

          <div className="relative z-10 flex h-[calc(100%-2rem)] flex-row space-x-2">
            <ModPreviewContainer
              mod={mod}
              onDeletePreview={() => actions.openDeletePreview(mod)}
              onPaste={() => actions.openPastePreview(mod)}
            />

            {!actions.isNteGame && mod.inis.length > 0 && (
              <>
                <div className="relative flex items-stretch">
                  <Separator orientation="vertical" />
                  <button
                    type="button"
                    aria-label={isIniListExpanded ? "Collapse ini list" : "Expand ini list"}
                    className="pointer-events-auto absolute inset-y-0 left-1/2 z-10 w-6 -translate-x-1/2 bg-transparent"
                    style={{ cursor: "col-resize" }}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleIniListExpandedChange();
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

          <div className="absolute bottom-1 left-1 z-10 flex flex-col space-y-1">
            <Badge
              className="flex h-5 items-center gap-1.5 bg-background/35 text-xs text-foreground backdrop-blur"
              style={{ transform: "translateZ(0)", willChange: "backdrop-filter" }}
            >
              <FolderIcon />
              {formatSize(mod.size)}
            </Badge>
            <Badge
              className="flex h-5 items-center gap-1.5 bg-background/35 text-xs text-foreground backdrop-blur"
              style={{ transform: "translateZ(0)", willChange: "backdrop-filter" }}
            >
              <CalendarIcon />
              {formatDate(new Date(mod.mtime), "ko")}
            </Badge>
          </div>
        </div>
      </ModContextMenu>
    </>
  );
});
