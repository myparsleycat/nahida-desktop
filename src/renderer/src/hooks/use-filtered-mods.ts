import { getSearchScore } from "@renderer/lib/sejong";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@shared/types";
import { disassemble, getChoseong } from "es-hangul";
import { useMemo } from "react";

const nameCollator = new Intl.Collator(undefined, {
    numeric: true,
    sensitivity: "base",
    caseFirst: "false",
});

export function useFilteredMods(mods: ModInfo[], searchQuery: string) {
    const sortType = useModStore((s) => s.sortType);
    const sortOrder = useModStore((s) => s.sortOrder);

    return useMemo(() => {
        const scoredMods = mods.map((m) => {
            if (!searchQuery) return { mod: m, score: 0 };
            const query = searchQuery.toLowerCase();
            const lowerName = m.name.toLowerCase();
            const cachedData = {
                lowerName,
                jamo: disassemble(lowerName),
                chosung: getChoseong(lowerName),
            };
            return {
                mod: m,
                score: getSearchScore(m.name, query, cachedData),
            };
        });

        const filtered = searchQuery ? scoredMods.filter((sm) => sm.score > 0) : scoredMods;

        return filtered
            .sort((a, b) => {
                if (a.mod.isEnabled !== b.mod.isEnabled) {
                    return a.mod.isEnabled ? -1 : 1;
                }

                if (searchQuery && a.score !== b.score) {
                    return b.score - a.score;
                }

                let comparison = 0;
                if (sortType === "name") {
                    comparison = nameCollator.compare(a.mod.name, b.mod.name);
                } else if (sortType === "date") {
                    comparison = a.mod.mtime - b.mod.mtime;
                } else if (sortType === "size") {
                    comparison = a.mod.size - b.mod.size;
                }

                return sortOrder === "asc" ? comparison : -comparison;
            })
            .map((sm) => sm.mod);
    }, [mods, searchQuery, sortType, sortOrder]);
}
