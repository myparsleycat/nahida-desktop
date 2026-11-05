import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import type { ModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { useModView } from "@renderer/hooks/use-mod-view";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { ListHead } from "./mod-list-head";
import { MOD_LIST_ROW_HEIGHT } from "./mod-list-layout";
import { ModListRow } from "./mod-list-row";

interface ModListProps {
  runner: ModFixRunner;
}

export function ModList({ runner }: ModListProps) {
  const { t } = useTranslation();
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  const { searchQuery, selectedGroupPath, mods, actions, overlays, showSkeleton, handleToggle } =
    useModView(runner);
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
      {overlays}
    </div>
  );
}
