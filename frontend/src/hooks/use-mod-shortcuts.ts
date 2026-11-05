import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { globalStore } from "@renderer/store/global";
import { modStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { useEffect, useLayoutEffect, useRef } from "react";

import { findModDownloadTransfer } from "./use-mod-download-transfer";

export function useModShortcuts(
    searchQuery: string,
    filteredMods: ModInfo[],
    confirmEnable: ModActionApi["confirmEnable"],
) {
    const latestSearchQueryRef = useRef(searchQuery);
    const latestFilteredModsRef = useRef(filteredMods);
    const { exclusiveToggleModMutation } = useModMutations();
    const exclusiveToggleRef = useRef(exclusiveToggleModMutation.mutate);
    const confirmEnableRef = useRef(confirmEnable);
    // The filtered list follows a deferred query, so an Enter pressed before it catches up waits for that query.
    const pendingEnterQueryRef = useRef<string | null>(null);

    useLayoutEffect(() => {
        latestSearchQueryRef.current = searchQuery;
        latestFilteredModsRef.current = filteredMods;
        exclusiveToggleRef.current = exclusiveToggleModMutation.mutate;
        confirmEnableRef.current = confirmEnable;
    });

    useEffect(
        () =>
            modStore.subscribe((state, previous) => {
                if (state.searchQuery !== previous.searchQuery) {
                    pendingEnterQueryRef.current = null;
                }
            }),
        [],
    );

    useEffect(() => {
        if (pendingEnterQueryRef.current !== searchQuery) {
            return;
        }
        pendingEnterQueryRef.current = null;
        if (
            document.activeElement === document.getElementById("mod-search-input") &&
            !modStore.getState().isMergeMode &&
            filteredMods.length === 1
        ) {
            toggleSearchMatch(
                filteredMods[0],
                confirmEnableRef.current,
                exclusiveToggleRef.current,
            );
        }
    }, [filteredMods, searchQuery]);

    useEffect(() => {
        const handleKeyDown = (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && e.key === "f") {
                const characterSearch = document.getElementById("character-search-input");
                const modSearch = document.getElementById("mod-search-input");
                const activeElement = document.activeElement;

                if (activeElement === characterSearch) {
                    e.preventDefault();
                    modSearch?.focus();
                    return;
                }

                if (activeElement === modSearch) {
                    e.preventDefault();
                    characterSearch?.focus();
                    return;
                }

                e.preventDefault();
                characterSearch?.focus();
            }

            if (e.key === "Escape") {
                if (
                    e.defaultPrevented ||
                    !modStore.getState().isMergeMode ||
                    hasOpenDialogOrOverlay()
                ) {
                    return;
                }

                e.preventDefault();
                modStore.getState().exitMergeMode();
                return;
            }

            // Enter that commits an IME composition, such as a Hangul syllable, is not a submit.
            if (e.key !== "Enter" || e.isComposing) {
                return;
            }

            if (modStore.getState().isMergeMode) {
                return;
            }

            const modSearch = document.getElementById("mod-search-input");
            const { searchQuery } = modStore.getState();
            if (document.activeElement !== modSearch || !searchQuery) {
                return;
            }
            if (latestSearchQueryRef.current !== searchQuery) {
                e.preventDefault();
                pendingEnterQueryRef.current = searchQuery;
                return;
            }

            if (latestFilteredModsRef.current.length !== 1) {
                return;
            }

            e.preventDefault();
            toggleSearchMatch(
                latestFilteredModsRef.current[0],
                confirmEnableRef.current,
                exclusiveToggleRef.current,
            );
        };

        window.addEventListener("keydown", handleKeyDown);
        return () => window.removeEventListener("keydown", handleKeyDown);
    }, []);
}

function toggleSearchMatch(
    mod: ModInfo,
    confirmEnable: ModActionApi["confirmEnable"],
    exclusiveToggle: ReturnType<typeof useModMutations>["exclusiveToggleModMutation"]["mutate"],
) {
    if (mod.isDownloading || findModDownloadTransfer(globalStore.getState().transfers, mod.path)) {
        return;
    }

    const request = { mod, groupPath: modStore.getState().selectedGroup?.path };
    confirmEnable(mod, () => exclusiveToggle(request));
    modStore.getState().setSearchQuery("");
}

function hasOpenDialogOrOverlay() {
    const state = modStore.getState();
    if (
        state.isMergeDialogOpen ||
        state.isCustomDownloadDialogOpen ||
        state.isPresetDialogOpen ||
        state.isSelectedPresetDialogOpen ||
        state.isAddGameDialogOpen ||
        state.isDeleteGameDialogOpen ||
        state.isEditGameDialogOpen ||
        state.isNteLaunchDialogOpen ||
        Boolean(state.downloadMode) ||
        Boolean(state.archiveExtractPrompt)
    ) {
        return true;
    }

    return Boolean(
        document.querySelector(
            '[role="dialog"], [role="alertdialog"], [data-slot="dialog-content"], [data-slot="alert-dialog-content"], [data-slot="dialog-overlay"], [data-slot="alert-dialog-overlay"]',
        ),
    );
}
