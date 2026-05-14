import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import { useModActions } from "@renderer/hooks/use-mod-actions";
import { useModGroup } from "@renderer/hooks/use-mod-data";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useModShortcuts } from "@renderer/hooks/use-mod-shortcuts";
import { useModGridLayoutSettings, useVirtualizationSettings } from "@renderer/hooks/use-settings";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { useVirtualizer } from "@tanstack/react-virtual";
import { chunk } from "es-toolkit";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { normalizeModGridLayoutSettings, resolveModGridLayout } from "./grid-layout";
import { ModCard } from "./mod-card";

interface ModGridProps {
  isDragging?: boolean;
}

export function ModGrid(_props: ModGridProps) {
  const { t } = useTranslation();
  const searchQuery = useModStore((s) => s.searchQuery);
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);

  // const [parent] = useAutoAnimate({ duration: 150 });

  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const { data: activeGroup, isPlaceholderData, isPending } = useModGroup(selectedGroupPath);
  const actions = useModActions(selectedGroupPath);

  const { toggleModMutation, exclusiveToggleModMutation, updateToggleKeyMutation } =
    useModMutations();

  const mods = useFilteredMods(activeGroup?.mods || [], searchQuery);
  useModShortcuts(searchQuery, mods);
  const isLoading = isPending || isPlaceholderData;
  const showSkeleton = useDelayedSkeleton(isLoading);

  const { data: vSettings } = useVirtualizationSettings();
  const { data: gridLayoutSettings } = useModGridLayoutSettings();

  const [availableWidth, setAvailableWidth] = useState(0);
  const handleViewportRef = useCallback((node: HTMLDivElement | null) => {
    setViewport(node);
  }, []);

  useEffect(() => {
    if (!viewport) return;

    const updateWidth = () => {
      setAvailableWidth(viewport.clientWidth);
    };

    updateWidth();
    const frameId = window.requestAnimationFrame(updateWidth);
    const observer = new ResizeObserver(updateWidth);
    observer.observe(viewport);
    window.addEventListener("resize", updateWidth);

    return () => {
      window.cancelAnimationFrame(frameId);
      observer.disconnect();
      window.removeEventListener("resize", updateWidth);
    };
  }, [viewport]);

  const resolvedGridLayout = useMemo(
    () => resolveModGridLayout(availableWidth, normalizeModGridLayoutSettings(gridLayoutSettings)),
    [availableWidth, gridLayoutSettings],
  );
  const columnCount = resolvedGridLayout.columnCount;
  const overscan = useMemo(() => {
    const targetOverscanCardCount = 16;
    return Math.max(2, Math.min(6, Math.ceil(targetOverscanCardCount / columnCount)));
  }, [columnCount]);

  const rows = useMemo(() => chunk(mods, columnCount), [mods, columnCount]);
  const getModRenderKey = useCallback(
    (mod: ModInfo) => `${selectedGroupPath ?? ""}::${mod.path}`,
    [selectedGroupPath],
  );
  const isVirtualizationEnabled =
    (vSettings?.enabled ?? true) && mods.length >= (vSettings?.threshold ?? 30);

  const rowVirtualizer = useVirtualizer({
    count: isVirtualizationEnabled ? rows.length : 0,
    getItemKey: (index) => {
      const rowMods = rows[index] ?? [];
      return `${selectedGroupPath ?? ""}::${JSON.stringify(rowMods.map((mod) => mod.path))}`;
    },
    getScrollElement: () => viewport,
    estimateSize: useCallback(() => 400 + 12, []), // card height (400) + gap (12)
    overscan,
    measureElement: (element) => element?.getBoundingClientRect().height,
  });

  useEffect(() => {
    if (viewport) {
      viewport.scrollTop = 0;
    }
    if (isVirtualizationEnabled && rowVirtualizer) {
      rowVirtualizer.scrollToOffset(0);
    }
  }, [selectedGroupPath, searchQuery, rowVirtualizer, isVirtualizationEnabled, viewport]);

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

  const handleToggleKeyUpdate = useCallback(
    (
      modPath: string,
      iniFileName: string,
      sectionName: string,
      variable: string,
      value: string,
    ) => {
      updateToggleKeyMutation.mutate({
        modPath,
        iniFileName,
        sectionName,
        variable,
        value,
      });
    },
    [updateToggleKeyMutation.mutate],
  );

  if (!selectedGroupPath) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center text-muted-foreground h-full min-h-0">
        <p>{t("page.mod.empty_selection")}</p>
      </div>
    );
  }

  return (
    <div className="flex-1 min-h-0">
      <ScrollArea className="h-full overflow-y-auto" viewportRef={handleViewportRef}>
        <div className="relative w-full p-3">
          {showSkeleton ? (
            <div
              className="grid gap-3"
              style={{
                gridTemplateColumns: resolvedGridLayout.gridTemplateColumns,
                justifyContent: resolvedGridLayout.justifyContent,
              }}
            >
              {Array.from({ length: 12 }).map((_, index) => (
                <div
                  key={index.toString()}
                  className="flex flex-col space-y-3 rounded-lg border p-4"
                >
                  <Skeleton className="h-48 w-full rounded-md" />
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-4 w-1/2" />
                  </div>
                  <div className="flex gap-2">
                    <Skeleton className="h-9 flex-1" />
                    <Skeleton className="h-9 w-9" />
                  </div>
                </div>
              ))}
            </div>
          ) : !isVirtualizationEnabled ? (
            <div
              className="grid gap-3"
              style={{
                gridTemplateColumns: resolvedGridLayout.gridTemplateColumns,
                justifyContent: resolvedGridLayout.justifyContent,
              }}
              // ref={parent}
            >
              {mods.map((mod) => (
                <ModCard
                  key={getModRenderKey(mod)}
                  mod={mod}
                  selectedGroupPath={selectedGroupPath}
                  actions={actions}
                  onToggle={handleToggle}
                  onToggleKeyUpdate={handleToggleKeyUpdate}
                />
              ))}
            </div>
          ) : (
            <div
              style={{
                height: `${rowVirtualizer.getTotalSize()}px`,
                width: "100%",
                position: "relative",
              }}
            >
              {rowVirtualizer.getVirtualItems().map((virtualRow) => {
                const rowMods = rows[virtualRow.index];
                if (!rowMods) return null;

                return (
                  <div
                    key={virtualRow.key}
                    data-index={virtualRow.index}
                    style={{
                      position: "absolute",
                      top: 0,
                      left: 0,
                      width: "100%",
                      height: `${virtualRow.size}px`,
                      transform: `translateY(${virtualRow.start}px)`,
                    }}
                    className="grid gap-3"
                  >
                    <div
                      className="grid gap-3 w-full"
                      style={{
                        gridTemplateColumns: resolvedGridLayout.gridTemplateColumns,
                        justifyContent: resolvedGridLayout.justifyContent,
                      }}
                    >
                      {rowMods.map((mod) => (
                        <ModCard
                          key={getModRenderKey(mod)}
                          mod={mod}
                          selectedGroupPath={selectedGroupPath}
                          actions={actions}
                          onToggle={handleToggle}
                          onToggleKeyUpdate={handleToggleKeyUpdate}
                        />
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </ScrollArea>
      {actions.overlays}
    </div>
  );
}
