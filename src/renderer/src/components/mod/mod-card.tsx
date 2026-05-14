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
                src={`local://${mod.preview}?v=${encodeURIComponent(String(mod.mtime))}`}
                alt="preview"
                className="w-full h-full object-fill"
              />
            </div>
          )}

          <ModCardHeader mod={mod} actions={actions} />

          <div className="flex flex-row h-[calc(100%-2rem)] space-x-2 relative z-10">
            <ModPreviewContainer
              mod={mod}
              onDeletePreview={() => actions.openDeletePreview(mod)}
              onPaste={() => actions.openPastePreview(mod)}
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
    </>
  );
});
