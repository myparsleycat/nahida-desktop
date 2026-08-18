import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { modStore, useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { useEffect, useRef } from "react";

export function useModShortcuts(searchQuery: string, filteredMods: ModInfo[]) {
    const latestSearchQueryRef = useRef(searchQuery);
    const latestFilteredModsRef = useRef(filteredMods);
    const isMergeMode = useModStore((s) => s.isMergeMode);
    const exitMergeMode = useModStore((s) => s.exitMergeMode);
    const setSearchQuery = useModStore((s) => s.setSearchQuery);
    const setSearchQueryRef = useRef(setSearchQuery);
    const { exclusiveToggleModMutation } = useModMutations();
    const exclusiveToggleRef = useRef(exclusiveToggleModMutation.mutate);

    const isMergeModeRef = useRef(isMergeMode);
    const exitMergeModeRef = useRef(exitMergeMode);
    latestSearchQueryRef.current = searchQuery;
    latestFilteredModsRef.current = filteredMods;
    setSearchQueryRef.current = setSearchQuery;
    exclusiveToggleRef.current = exclusiveToggleModMutation.mutate;
    isMergeModeRef.current = isMergeMode;
    exitMergeModeRef.current = exitMergeMode;

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
                if (e.defaultPrevented || !isMergeModeRef.current || hasOpenDialogOrOverlay()) {
                    return;
                }

                e.preventDefault();
                exitMergeModeRef.current();
                return;
            }

            if (e.key !== "Enter") {
                return;
            }

            if (isMergeModeRef.current) {
                return;
            }

            const modSearch = document.getElementById("mod-search-input");
            if (document.activeElement !== modSearch || !latestSearchQueryRef.current) {
                return;
            }

            if (latestFilteredModsRef.current.length !== 1) {
                return;
            }

            e.preventDefault();
            exclusiveToggleRef.current(latestFilteredModsRef.current[0]);
            setSearchQueryRef.current("");
        };

        window.addEventListener("keydown", handleKeyDown);
        return () => window.removeEventListener("keydown", handleKeyDown);
    }, []);
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

    if (typeof document === "undefined") {
        return false;
    }

    return Boolean(
        document.querySelector(
            '[role="dialog"], [role="alertdialog"], [data-slot="dialog-content"], [data-slot="alert-dialog-content"], [data-slot="dialog-overlay"], [data-slot="alert-dialog-overlay"]',
        ),
    );
}
