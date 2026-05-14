import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import { useModActions } from "@renderer/hooks/use-mod-actions";
import { useModGroup } from "@renderer/hooks/use-mod-data";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useModShortcuts } from "@renderer/hooks/use-mod-shortcuts";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { ListHead } from "./mod-list-head";
import { ModListRow } from "./mod-list-row";

interface ModListProps {
  isDragging?: boolean;
}

export function ModList(_props: ModListProps) {
  const { t } = useTranslation();
  const searchQuery = useModStore((s) => s.searchQuery);

  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const { data: activeGroup, isPlaceholderData, isPending } = useModGroup(selectedGroupPath);
  const actions = useModActions(selectedGroupPath);

  const { toggleModMutation, exclusiveToggleModMutation } = useModMutations();

  const mods = useFilteredMods(activeGroup?.mods || [], searchQuery);
  useModShortcuts(searchQuery, mods);
  const getModRenderKey = useCallback(
    (mod: ModInfo) => `${selectedGroupPath ?? ""}::${mod.path}`,
    [selectedGroupPath],
  );
  const isLoading = isPending || isPlaceholderData;
  const showSkeleton = useDelayedSkeleton(isLoading);

  const handleToggle = useCallback(
    (mod: ModInfo, event?: React.MouseEvent) => {
      if (event && (event.ctrlKey || event.metaKey)) {
        exclusiveToggleModMutation.mutate(mod);
      } else {
        toggleModMutation.mutate(mod);
      }
    },
    [toggleModMutation.mutate, exclusiveToggleModMutation.mutate],
  );

  if (!selectedGroupPath) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground h-full min-h-0">
        <p>{t("page.mod.empty_selection")}</p>
      </div>
    );
  }

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      {showSkeleton ? (
        <ScrollArea className="flex-1 min-h-0 overflow-y-auto">
          <div className="p-3 grid gap-2">
            {Array.from({ length: 15 }).map((_, i) => (
              <Skeleton key={i.toString()} className="h-12 w-full" />
            ))}
          </div>
        </ScrollArea>
      ) : (
        <>
          <table className="w-full border-collapse table-auto text-sm shrink-0">
            <ListHead />
          </table>
          <ScrollArea className="flex-1 min-h-0 overflow-y-auto">
            <div className="w-full relative">
              <table className="w-full border-collapse table-auto relative text-sm">
                <tbody>
                  {mods.map((mod) => (
                    <ModListRow
                      key={getModRenderKey(mod)}
                      mod={mod}
                      actions={actions}
                      handleToggle={handleToggle}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          </ScrollArea>
        </>
      )}
      {actions.overlays}
    </div>
  );
}
