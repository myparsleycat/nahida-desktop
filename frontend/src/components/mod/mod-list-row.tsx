import { PreviewLightbox } from "@renderer/components/ui/preview-lightbox";
import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import { useModDownloadTransfer } from "@renderer/hooks/use-mod-download-transfer";
import i18n from "@renderer/lib/i18n";
import { localFileSrc } from "@renderer/lib/local-file";
import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { stripDisabledPrefix } from "@shared/mod";
import { formatDate, formatSize } from "@shared/utils";
import { FolderIcon } from "lucide-react";
import { memo } from "react";

import { ModContextMenu } from "./mod-context-menu";
import { ModDownloadOverlay } from "./mod-download-overlay";
import { MOD_LIST_GRID_TEMPLATE_COLUMNS } from "./mod-list-layout";
import { getModColorClass } from "./utils";

export const ModListRow = memo(function ModListRow({
  mod,
  actions,
  handleToggle,
}: {
  mod: ModInfo;
  actions: ModActionApi;
  handleToggle: (mod: ModInfo, e?: React.MouseEvent) => void;
}) {
  const isMergeSelected = useModStore((s) => s.isMergeMode && s.selectedModPaths.has(mod.path));
  const downloadTransfer = useModDownloadTransfer(mod.path);
  const isDownloading = mod.isDownloading || Boolean(downloadTransfer);
  const localSrc = mod.preview ? localFileSrc(mod.preview, { cacheKey: mod.mtime }) : "";
  const fullSrc = mod.preview ? localFileSrc(mod.preview, { orig: true, cacheKey: mod.mtime }) : "";

  return (
    <ModContextMenu mod={mod} actions={actions} disabled={isDownloading}>
      <div
        role="row"
        aria-busy={isDownloading}
        aria-disabled={isDownloading}
        className={cn(
          "group relative grid h-14 cursor-pointer items-center border-b border-transparent transition-colors",
          isDownloading ? "cursor-wait bg-muted grayscale" : getModColorClass(mod.isEnabled),
          !isDownloading && isMergeSelected && "ring-2 ring-primary ring-inset",
          !isDownloading &&
            "after:pointer-events-none after:absolute after:inset-0 hover:after:bg-black/10 dark:hover:after:bg-white/10",
        )}
        style={{ gridTemplateColumns: MOD_LIST_GRID_TEMPLATE_COLUMNS }}
        onClick={(e) => {
          if (isDownloading) {
            return;
          }
          const target = e.target as HTMLElement;
          if (target.closest("button") || target.closest(".preview-trigger")) {
            return;
          }
          handleToggle(mod, e);
        }}
      >
        <div className="contents" inert={isDownloading}>
          <div role="cell" className="flex items-center justify-center py-2 pl-2 text-center">
            {mod.preview ? (
              <PreviewLightbox
                className="preview-trigger flex size-10 shrink-0 cursor-zoom-in items-center justify-center overflow-hidden rounded-sm bg-secondary"
                thumbnailSrc={localSrc}
                fullSrc={fullSrc}
                isVideo={/\.(mp4|webm|ogg)$/i.test(mod.preview)}
              />
            ) : (
              <div className="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-sm bg-secondary/20">
                <FolderIcon className="size-5 text-muted-foreground" />
              </div>
            )}
          </div>
          <div role="cell" className="min-w-0 p-2 text-left">
            <span className="block w-full truncate text-left font-medium">
              {stripDisabledPrefix(mod.name)}
            </span>
          </div>
          <div role="cell" className="p-2 text-right whitespace-nowrap text-muted-foreground">
            {mod.isDownloadPlaceholder ? "—" : formatSize(mod.size || 0)}
          </div>
          <div role="cell" className="p-2 pr-3 text-right whitespace-nowrap text-muted-foreground">
            {mod.isDownloadPlaceholder ? "—" : formatDate(new Date(mod.mtime), i18n.language)}
          </div>
        </div>
        {downloadTransfer && <ModDownloadOverlay transfer={downloadTransfer} variant="row" />}
      </div>
    </ModContextMenu>
  );
});
