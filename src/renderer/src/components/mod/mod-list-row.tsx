import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import i18n from "@renderer/lib/i18n";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { formatDate, formatSize } from "@shared/utils";
import { FolderIcon } from "lucide-react";
import { ModContextMenu } from "./mod-context-menu";
import { ModPreviewLightbox } from "./mod-preview-lightbox";
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
  return (
    <ModContextMenu mod={mod} actions={actions}>
      <tr
        className={cn(
          "relative group cursor-pointer border-b border-transparent transition-colors",
          getModColorClass(mod.isEnabled),
          "after:absolute after:inset-0 after:pointer-events-none hover:after:bg-black/10 dark:hover:after:bg-white/10",
        )}
        onClick={(e) => {
          const target = e.target as HTMLElement;
          if (target.closest("button") || target.closest(".preview-trigger")) {
            return;
          }
          handleToggle(mod, e);
        }}
      >
        <td className="py-2 pl-2 align-middle text-center w-10">
          {mod.preview ? (
            <div className="preview-trigger" onClick={(e) => e.stopPropagation()}>
              <ModPreviewLightbox preview={mod.preview} cacheKey={mod.mtime} />
            </div>
          ) : (
            <div className="size-10 rounded-sm bg-secondary/20 flex items-center justify-center overflow-hidden shrink-0">
              <FolderIcon className="size-5 text-muted-foreground" />
            </div>
          )}
        </td>
        <td className="p-2 align-middle text-left w-full max-w-0">
          <span className="truncate block w-full text-left font-medium">
            {mod.name.replace(/disabled/gi, "").trim()}
          </span>
        </td>
        <td className="p-2 align-middle text-muted-foreground whitespace-nowrap text-right w-[1%]">
          {formatSize(mod.size || 0)}
        </td>
        <td className="p-2 pr-3 align-middle text-muted-foreground whitespace-nowrap text-right w-[1%]">
          {formatDate(new Date(mod.mtime), i18n.language)}
        </td>
      </tr>
    </ModContextMenu>
  );
}
