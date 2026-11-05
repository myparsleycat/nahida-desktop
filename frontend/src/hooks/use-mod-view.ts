import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import { useModActions } from "@renderer/hooks/use-mod-actions";
import { useModGroup } from "@renderer/hooks/use-mod-data";
import { useModsWithDownloadPlaceholders } from "@renderer/hooks/use-mod-download-placeholders";
import type { ModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useModShortcuts } from "@renderer/hooks/use-mod-shortcuts";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { type KeyboardEvent, type MouseEvent, useCallback } from "react";

export type ModToggleEvent = MouseEvent | KeyboardEvent;

export function useModView(runner: ModFixRunner) {
    const searchQuery = useModStore((s) => s.searchQuery);
    const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
    const {
        data: activeGroup,
        dataUpdatedAt,
        isPlaceholderData,
        isPending,
    } = useModGroup(selectedGroupPath);
    const { actions, overlays } = useModActions(selectedGroupPath, runner);

    const isMergeMode = useModStore((s) => s.isMergeMode);
    const toggleMergeSelection = useModStore((s) => s.toggleMergeSelection);
    const { toggleModMutation, exclusiveToggleModMutation } = useModMutations();

    const displayMods = useModsWithDownloadPlaceholders(
        selectedGroupPath,
        isPlaceholderData ? [] : activeGroup?.mods || [],
    );
    const mods = useFilteredMods(displayMods, searchQuery);
    useModShortcuts(searchQuery, mods, actions.confirmEnable);

    const showDelayedSkeleton = useDelayedSkeleton(isPending || isPlaceholderData);
    const showSkeleton =
        displayMods.length === 0 &&
        ((isPlaceholderData && activeGroup != null) || showDelayedSkeleton);

    const { confirmEnable } = actions;
    const handleToggle = useCallback(
        (mod: ModInfo, event: ModToggleEvent) => {
            if (isMergeMode) {
                toggleMergeSelection(mod.path);
                return;
            }
            const isExclusive = event.ctrlKey || event.metaKey;
            const request = { mod, groupPath: selectedGroupPath };
            confirmEnable(mod, () => {
                if (isExclusive) {
                    exclusiveToggleModMutation.mutate(request);
                } else {
                    toggleModMutation.mutate(request);
                }
            });
        },
        [
            confirmEnable,
            isMergeMode,
            selectedGroupPath,
            toggleMergeSelection,
            toggleModMutation.mutate,
            exclusiveToggleModMutation.mutate,
        ],
    );

    return {
        searchQuery,
        selectedGroupPath,
        dataUpdatedAt,
        mods,
        actions,
        overlays,
        showSkeleton,
        handleToggle,
    };
}
