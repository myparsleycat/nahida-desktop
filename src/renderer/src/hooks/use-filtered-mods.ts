import { useMemo } from "react";
import type { ModInfo } from "@shared/types";
import { disassemble, getChoseong } from "es-hangul";
import { getSearchScore } from "@renderer/lib/sejong";

export function useFilteredMods(mods: ModInfo[], searchQuery: string) {
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
                return a.mod.name.localeCompare(b.mod.name);
            })
            .map((sm) => sm.mod);
    }, [mods, searchQuery]);
}
