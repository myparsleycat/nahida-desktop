import { useEffect } from "react";
import { useModStore } from "@renderer/store/mod";
import { useModGroup } from "./use-mod-data";
import { useFilteredMods } from "./use-filtered-mods";
import { useModMutations } from "./use-mod-mutations";

export function useModShortcuts() {
    const searchQuery = useModStore((s) => s.searchQuery);
    const setSearchQuery = useModStore((s) => s.setSearchQuery);
    const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
    const { data: activeGroup } = useModGroup(selectedGroupPath);
    const filteredMods = useFilteredMods(activeGroup?.mods || [], searchQuery);
    const { exclusiveToggleModMutation } = useModMutations();

    useEffect(() => {
        const handleKeyDown = (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && e.key === "f") {
                const characterSearch = document.getElementById("character-search-input");
                const modSearch = document.getElementById("mod-search-input");
                const activeElement = document.activeElement;

                if (activeElement === characterSearch) {
                    e.preventDefault();
                    modSearch?.focus();
                } else if (activeElement === modSearch) {
                    e.preventDefault();
                    characterSearch?.focus();
                } else {
                    e.preventDefault();
                    characterSearch?.focus();
                }
            }

            if (e.key === "Enter") {
                const modSearch = document.getElementById("mod-search-input");
                if (
                    document.activeElement === modSearch &&
                    searchQuery &&
                    filteredMods.length === 1
                ) {
                    e.preventDefault();
                    exclusiveToggleModMutation.mutate(filteredMods[0]);
                    setSearchQuery("");
                }
            }
        };

        window.addEventListener("keydown", handleKeyDown);
        return () => window.removeEventListener("keydown", handleKeyDown);
    }, [searchQuery, filteredMods, exclusiveToggleModMutation.mutate]);
}
