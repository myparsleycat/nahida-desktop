import { PreviewLightbox } from "@renderer/components/ui/preview-lightbox";
import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import i18n from "@renderer/lib/i18n";
import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { stripDisabledPrefix } from "@shared/mod";
import { formatDate, formatSize } from "@shared/utils";
import { FolderIcon } from "lucide-react";

import { ModContextMenu } from "./mod-context-menu";
import { getModColorClass } from "./utils";

export function ModListRow({
  mod,
  actions,
  handleToggle,
}: {
  mod: ModInfo;
  actions: ModActionApi;
  handleToggle: (mod: ModInfo, e?: React.MouseEvent) => void;
}) {
  const isMergeSelected = useModStore((s) => s.isMergeMode && s.selectedModPaths.has(mod.path));
  const localSrc = mod.preview
    ? `local://${mod.preview}?v=${encodeURIComponent(String(mod.mtime))}`
    : "";

  return (
    <ModContextMenu mod={mod} actions={actions}>
      <tr
        className={cn(
          "group relative cursor-pointer border-b border-transparent transition-colors",
          getModColorClass(mod.isEnabled),
          isMergeSelected && "ring-2 ring-primary ring-inset",
          "after:pointer-events-none after:absolute after:inset-0 hover:after:bg-black/10 dark:hover:after:bg-white/10",
        )}
        onClick={(e) => {
          const target = e.target as HTMLElement;
          if (target.closest("button") || target.closest(".preview-trigger")) {
            return;
          }
          handleToggle(mod, e);
        }}
      >
        <td className="w-10 py-2 pl-2 text-center align-middle">
          {mod.preview ? (
            <PreviewLightbox
              className="preview-trigger flex size-10 shrink-0 cursor-zoom-in items-center justify-center overflow-hidden rounded-sm bg-secondary"
              thumbnailSrc={localSrc}
              fullSrc={`${localSrc}&orig=true`}
              isVideo={/\.(mp4|webm|ogg)$/i.test(mod.preview)}
            />
          ) : (
            <div className="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-sm bg-secondary/20">
              <FolderIcon className="size-5 text-muted-foreground" />
            </div>
          )}
        </td>
        <td className="w-full max-w-0 p-2 text-left align-middle">
          <span className="block w-full truncate text-left font-medium">
            {stripDisabledPrefix(mod.name)}
          </span>
        </td>
        <td className="w-[1%] p-2 text-right align-middle whitespace-nowrap text-muted-foreground">
          {formatSize(mod.size || 0)}
        </td>
        <td className="w-[1%] p-2 pr-3 text-right align-middle whitespace-nowrap text-muted-foreground">
          {formatDate(new Date(mod.mtime), i18n.language)}
        </td>
      </tr>
    </ModContextMenu>
  );
}
