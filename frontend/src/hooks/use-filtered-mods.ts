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

    const hasQuery = searchQuery.length > 0;
    const searchIndex = useMemo(
        () =>
            hasQuery
                ? mods.map((mod) => {
                      const lowerName = mod.name.toLowerCase();
                      return {
                          lowerName,
                          jamo: disassemble(lowerName),
                          chosung: getChoseong(lowerName),
                      };
                  })
                : [],
        [hasQuery, mods],
    );

    return useMemo(() => {
        const query = searchQuery.toLowerCase();
        const scoredMods = mods.map((m, index) => ({
            mod: m,
            score: searchQuery ? getSearchScore(m.name, query, searchIndex[index]) : 0,
        }));

        const filtered = searchQuery ? scoredMods.filter((sm) => sm.score > 0) : scoredMods;

        return filtered
            .sort((a, b) => {
                if (Boolean(a.mod.isDownloading) !== Boolean(b.mod.isDownloading)) {
                    return a.mod.isDownloading ? -1 : 1;
                }

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
    }, [mods, searchIndex, searchQuery, sortType, sortOrder]);
}
