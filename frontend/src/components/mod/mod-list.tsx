import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import { useModActions } from "@renderer/hooks/use-mod-actions";
import { useModGroup } from "@renderer/hooks/use-mod-data";
import { useModsWithDownloadPlaceholders } from "@renderer/hooks/use-mod-download-placeholders";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useModShortcuts } from "@renderer/hooks/use-mod-shortcuts";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { ListHead } from "./mod-list-head";
import { MOD_LIST_ROW_HEIGHT } from "./mod-list-layout";
import { ModListRow } from "./mod-list-row";

interface ModListProps {
  isDragging?: boolean;
}

export function ModList(_props: ModListProps) {
  const { t } = useTranslation();
  const searchQuery = useModStore((s) => s.searchQuery);
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);

  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const { data: activeGroup, isPlaceholderData, isPending } = useModGroup(selectedGroupPath);
  const actions = useModActions(selectedGroupPath);

  const isMergeMode = useModStore((s) => s.isMergeMode);
  const toggleMergeSelection = useModStore((s) => s.toggleMergeSelection);
  const { toggleModMutation, exclusiveToggleModMutation } = useModMutations();

  const displayMods = useModsWithDownloadPlaceholders(
    selectedGroupPath,
    isPlaceholderData ? [] : activeGroup?.mods || [],
  );
  const mods = useFilteredMods(displayMods, searchQuery);
  useModShortcuts(searchQuery, mods);
  const isLoading = isPending || isPlaceholderData;
  const showDelayedSkeleton = useDelayedSkeleton(isLoading);
  const showSkeleton =
    displayMods.length === 0 && ((isPlaceholderData && activeGroup != null) || showDelayedSkeleton);
  const getRowKey = useCallback(
    (index: number) => {
      const modPath = mods[index]?.path;
      return modPath ? `${selectedGroupPath ?? ""}::${modPath}` : index;
    },
    [mods, selectedGroupPath],
  );
  const rowVirtualizer = useVirtualizer({
    count: mods.length,
    getItemKey: getRowKey,
    getScrollElement: () => viewport,
    estimateSize: useCallback(() => MOD_LIST_ROW_HEIGHT, []),
    overscan: 6,
    directDomUpdates: true,
  });

  useEffect(() => {
    if (!viewport) {
      return;
    }

    viewport.scrollTo({ top: 0 });
    rowVirtualizer.scrollToOffset(0);
  }, [rowVirtualizer, searchQuery, selectedGroupPath, viewport]);

  const handleToggle = useCallback(
    (mod: ModInfo, event?: React.MouseEvent) => {
      if (isMergeMode) {
        toggleMergeSelection(mod.path);
        return;
      }
      if (event && (event.ctrlKey || event.metaKey)) {
        exclusiveToggleModMutation.mutate(mod);
      } else {
        toggleModMutation.mutate(mod);
      }
    },
    [
      isMergeMode,
      toggleMergeSelection,
      toggleModMutation.mutate,
      exclusiveToggleModMutation.mutate,
    ],
  );

  if (!selectedGroupPath) {
    return (
      <div className="flex h-full min-h-0 flex-1 flex-col items-center justify-center text-muted-foreground">
        <p>{t("page.mod.empty_selection")}</p>
      </div>
    );
  }

  return (
    <div role="table" className="flex min-h-0 flex-1 flex-col text-sm">
      {showSkeleton ? (
        <ScrollArea className="min-h-0 flex-1 overflow-y-auto">
          <div className="grid gap-2 p-3">
            {Array.from({ length: 15 }).map((_, i) => (
              <Skeleton key={i.toString()} className="h-12 w-full" />
            ))}
          </div>
        </ScrollArea>
      ) : (
        <>
          <ListHead />
          <ScrollArea className="min-h-0 flex-1 overflow-y-auto" viewportRef={setViewport}>
            <div ref={rowVirtualizer.containerRef} role="rowgroup" className="relative w-full">
              {rowVirtualizer.getVirtualItems().map((virtualRow) => {
                const mod = mods[virtualRow.index];
                if (!mod) {
                  return null;
                }

                return (
                  <div
                    key={virtualRow.key}
                    ref={rowVirtualizer.measureElement}
                    data-index={virtualRow.index}
                    className="absolute top-0 left-0 w-full"
                    style={{ height: `${virtualRow.size}px` }}
                  >
                    <ModListRow mod={mod} actions={actions} handleToggle={handleToggle} />
                  </div>
                );
              })}
            </div>
          </ScrollArea>
        </>
      )}
      {actions.overlays}
    </div>
  );
}
