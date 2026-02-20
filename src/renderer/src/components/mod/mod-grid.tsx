import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import { useModGroup } from "@renderer/hooks/use-mod-data";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useVirtualizationSettings } from "@renderer/hooks/use-settings";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { useVirtualizer } from "@tanstack/react-virtual";
import { chunk } from "es-toolkit";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ModCard } from "./mod-card";

interface ModGridProps {
  isDragging?: boolean;
}

export function ModGrid(_props: ModGridProps) {
  const { t } = useTranslation();
  const searchQuery = useModStore((s) => s.searchQuery);
  const scrollAreaRef = useRef<HTMLDivElement>(null);

  // const [parent] = useAutoAnimate({ duration: 150 });

  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const { data: activeGroup, isPlaceholderData, isPending } = useModGroup(selectedGroupPath);

  const { toggleModMutation, exclusiveToggleModMutation, updateToggleKeyMutation } =
    useModMutations();

  const mods = useFilteredMods(activeGroup?.mods || [], searchQuery);
  const isLoading = isPending || isPlaceholderData;
  const showSkeleton = useDelayedSkeleton(isLoading);

  const { data: vSettings } = useVirtualizationSettings();

  const [columnCount, setColumnCount] = useState(1);

  useEffect(() => {
    if (!scrollAreaRef.current) return;

    const viewport = scrollAreaRef.current.querySelector("[data-radix-scroll-area-viewport]");
    if (!viewport) return;

    const updateColumns = () => {
      const width = viewport.clientWidth;
      if (width < 800) setColumnCount(1);
      else if (width < 1200) setColumnCount(2);
      else if (width < 1600) setColumnCount(3);
      else if (width < 2000) setColumnCount(4);
      else if (width < 2400) setColumnCount(5);
      else if (width < 2800) setColumnCount(6);
      else setColumnCount(7);
    };

    updateColumns();
    const observer = new ResizeObserver(updateColumns);
    observer.observe(viewport);

    return () => observer.disconnect();
  }, [scrollAreaRef.current]);

  const rows = useMemo(() => chunk(mods, columnCount), [mods, columnCount]);
  const isVirtualizationEnabled =
    (vSettings?.enabled ?? true) && mods.length >= (vSettings?.threshold ?? 30);

  const rowVirtualizer = useVirtualizer({
    count: isVirtualizationEnabled ? rows.length : 0,
    getScrollElement: () =>
      scrollAreaRef.current?.querySelector("[data-radix-scroll-area-viewport]") || null,
    estimateSize: useCallback(() => 400 + 12, []), // card height (400) + gap (12)
    overscan: 10,
    measureElement: (element) => element?.getBoundingClientRect().height,
  });

  useEffect(() => {
    const viewport = scrollAreaRef.current?.querySelector("[data-radix-scroll-area-viewport]");
    if (viewport) {
      viewport.scrollTop = 0;
    }
    if (isVirtualizationEnabled && rowVirtualizer) {
      rowVirtualizer.scrollToOffset(0);
    }
  }, [selectedGroupPath, searchQuery, rowVirtualizer, isVirtualizationEnabled]);

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
      if (!value.trim()) {
        toast.error("값을 입력해주세요.");
        return;
      }
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
    <div ref={scrollAreaRef} className="flex-1 min-h-0">
      <ScrollArea className="h-full overflow-y-auto">
        <div className="relative w-full p-3">
          {showSkeleton ? (
            <div
              className="grid gap-3"
              style={{
                gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
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
                gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
              }}
              // ref={parent}
            >
              {mods.map((mod) => (
                <ModCard
                  key={mod.path}
                  mod={mod}
                  selectedGroupPath={selectedGroupPath}
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
                        gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
                      }}
                    >
                      {rowMods.map((mod) => (
                        <ModCard
                          key={mod.path}
                          mod={mod}
                          selectedGroupPath={selectedGroupPath}
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
    </div>
  );
}
