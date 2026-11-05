import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import type { ModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useModView } from "@renderer/hooks/use-mod-view";
import { useModGridLayoutSettings } from "@renderer/hooks/use-settings";
import type { ModInfo } from "@renderer/types/mod";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import { resolveModGridLayout } from "./grid-layout";
import { GridModelPreviewProvider } from "./grid-model-preview";
import { ModCard } from "./mod-card";

interface ModGridProps {
  runner: ModFixRunner;
}

export function ModGrid({ runner }: ModGridProps) {
  const { t } = useTranslation();
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  const { searchQuery, selectedGroupPath, mods, actions, overlays, showSkeleton, handleToggle } =
    useModView(runner);
  const { updateToggleKeyMutation } = useModMutations();

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
    () => resolveModGridLayout(availableWidth, gridLayoutSettings),
    [availableWidth, gridLayoutSettings],
  );
  const getModRenderKey = useCallback(
    (mod: ModInfo) => `${selectedGroupPath ?? ""}::${mod.path}`,
    [selectedGroupPath],
  );

  useEffect(() => {
    if (viewport) {
      viewport.scrollTo({ top: 0 });
    }
  }, [selectedGroupPath, searchQuery, viewport]);

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
      <div className="flex h-full min-h-0 flex-1 flex-col items-center justify-center text-muted-foreground">
        <p>{t("page.mod.empty_selection")}</p>
      </div>
    );
  }

  return (
    <GridModelPreviewProvider key={selectedGroupPath} viewport={viewport}>
      <div className="min-h-0 flex-1">
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
                    className="flex flex-col space-y-3 rounded-lg bg-muted/40 p-4"
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
            ) : (
              <div
                className="grid gap-3"
                style={{
                  gridTemplateColumns: resolvedGridLayout.gridTemplateColumns,
                  justifyContent: resolvedGridLayout.justifyContent,
                }}
                // ref={parent}
              >
                {mods.map((mod) => (
                  <div
                    key={getModRenderKey(mod)}
                    className="min-w-0"
                    style={{
                      contentVisibility: "auto",
                      containIntrinsicBlockSize: "auto 400px",
                    }}
                  >
                    <ModCard
                      mod={mod}
                      selectedGroupPath={selectedGroupPath}
                      actions={actions}
                      onToggle={handleToggle}
                      onToggleKeyUpdate={handleToggleKeyUpdate}
                    />
                  </div>
                ))}
              </div>
            )}
          </div>
        </ScrollArea>
        {overlays}
      </div>
    </GridModelPreviewProvider>
  );
}
