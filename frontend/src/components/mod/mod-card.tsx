import { Separator } from "@renderer/components/ui/separator";
import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import { useModDownloadTransfer } from "@renderer/hooks/use-mod-download-transfer";
import { localFileSrc } from "@renderer/lib/local-file";
import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { formatDate, formatSize } from "@shared/utils";
import { PackageIcon } from "lucide-react";
import { memo, useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";

import { ModCardHeader } from "./mod-card-header";
import { ModContextMenu } from "./mod-context-menu";
import { ModDownloadOverlay } from "./mod-download-overlay";
import { ModIniList } from "./mod-ini-list";
import { ModPreviewContainer } from "./mod-preview-container";
import { getModColorClass, hasMissingDependency } from "./utils";

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
  const { i18n } = useTranslation();
  const hasDependencies = Boolean(mod.dependencies?.length);
  const isMergeSelected = useModStore((s) => s.isMergeMode && s.selectedModPaths.has(mod.path));
  const downloadTransfer = useModDownloadTransfer(mod.path);
  const isDownloading = mod.isDownloading || Boolean(downloadTransfer);
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
      <ModContextMenu mod={mod} actions={actions} disabled={isDownloading}>
        <div
          aria-busy={isDownloading}
          aria-disabled={isDownloading}
          className={cn(
            "group/mod-card relative h-100 cursor-pointer overflow-hidden rounded-sm border-border/75 p-1 transition-shadow duration-150 hover:shadow-lg",
            isDownloading
              ? "cursor-wait bg-muted grayscale"
              : getModColorClass(mod.isEnabled, hasMissingDependency(mod)),
            !isDownloading &&
              isMergeSelected &&
              "ring-2 ring-primary ring-offset-2 ring-offset-background",
          )}
          onMouseDown={(e) => {
            mouseDownTargetRef.current = e.target;
          }}
          onClick={(e) => {
            if (isDownloading) {
              return;
            }
            const target = mouseDownTargetRef.current as HTMLElement;
            if (target && (target.tagName === "INPUT" || target.closest("button"))) {
              return;
            }
            onToggle(mod, e);
          }}
          draggable={false}
        >
          {/* Remount menu owners so their portaled content closes before this subtree becomes inert. */}
          <div
            key={isDownloading ? "downloading" : "interactive"}
            className="contents"
            inert={isDownloading}
          >
            {mod.preview?.match(/\.(jpeg|jpg|gif|png|webp|bmp)$/i) && (
              <div
                className="pointer-events-none absolute inset-0 z-0 scale-110 opacity-25 blur-lg"
                style={{ transform: "translateZ(0)", willChange: "filter" }}
              >
                <img
                  src={localFileSrc(mod.preview, { cacheKey: mod.mtime })}
                  alt="preview"
                  className="h-full w-full object-fill"
                />
              </div>
            )}

            <ModCardHeader mod={mod} actions={actions} />

            <div className="relative z-10 flex h-[calc(100%-2rem)] flex-row space-x-2">
              <div className="relative flex min-w-0 flex-1">
                <ModPreviewContainer
                  mod={mod}
                  selectedGroupPath={selectedGroupPath}
                  modelPreviewEligible={
                    !actions.isNteGame &&
                    !isDownloading &&
                    !mod.isDownloadPlaceholder &&
                    mod.inis.length > 0
                  }
                  onDeletePreview={() => actions.openDeletePreview(mod)}
                  onOpenModelViewer={() => void actions.openModelViewer(mod)}
                  onPaste={() => actions.openPastePreview(mod)}
                />

                {!mod.isDownloadPlaceholder && (
                  <div
                    className={cn(
                      "pointer-events-none absolute bottom-0 left-0 z-20 flex max-w-full flex-col rounded-sm bg-background/70 px-2 py-1 text-xs text-muted-foreground backdrop-blur-sm",
                      hasDependencies
                        ? "w-fit transition-[width] duration-200 ease-out [interpolate-size:allow-keywords] group-hover/mod-card:w-full"
                        : "w-full opacity-0 transition-opacity duration-150 group-hover/mod-card:opacity-100",
                    )}
                  >
                    {/* Animating grid rows from 0fr to 1fr expands the line to its content height.
                        w-0 with min-w-full keeps the line out of the collapsed fit-content width. */}
                    <div
                      className={cn(
                        hasDependencies &&
                          "grid w-0 min-w-full grid-rows-[0fr] opacity-0 transition-[grid-template-rows,opacity] duration-200 ease-out group-hover/mod-card:grid-rows-[1fr] group-hover/mod-card:opacity-100",
                      )}
                    >
                      <p className="min-h-0 overflow-hidden">
                        <span className={cn("block truncate", hasDependencies && "pb-0.5")}>
                          {formatSize(mod.size)} ·{" "}
                          {formatDate(new Date(mod.mtime), i18n.language, "Pp")}
                        </span>
                      </p>
                    </div>
                    {hasDependencies && (
                      <p className="flex items-center gap-1">
                        <PackageIcon className="size-3 shrink-0" />
                        <span className="truncate">
                          {mod.dependencies?.map((dependency, index) => (
                            <span
                              key={dependency.name}
                              className={dependency.installed ? undefined : "text-destructive"}
                            >
                              {index > 0 && ", "}
                              {dependency.name}
                            </span>
                          ))}
                        </span>
                      </p>
                    )}
                  </div>
                )}
              </div>

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
          </div>
          {downloadTransfer && <ModDownloadOverlay transfer={downloadTransfer} variant="card" />}
        </div>
      </ModContextMenu>
    </>
  );
});
