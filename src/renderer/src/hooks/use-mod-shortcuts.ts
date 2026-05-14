import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { useEffect, useRef } from "react";

export function useModShortcuts(searchQuery: string, filteredMods: ModInfo[]) {
    const latestSearchQueryRef = useRef(searchQuery);
    const latestFilteredModsRef = useRef(filteredMods);
    const setSearchQuery = useModStore((s) => s.setSearchQuery);
    const setSearchQueryRef = useRef(setSearchQuery);
    const { exclusiveToggleModMutation } = useModMutations();
    const exclusiveToggleRef = useRef(exclusiveToggleModMutation.mutate);

    latestSearchQueryRef.current = searchQuery;
    latestFilteredModsRef.current = filteredMods;
    setSearchQueryRef.current = setSearchQuery;
    exclusiveToggleRef.current = exclusiveToggleModMutation.mutate;

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

            if (e.key !== "Enter") {
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
