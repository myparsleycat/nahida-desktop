import { useQuery } from "@tanstack/react-query";
import type { FolderGroup, Preset, GameConfig } from "@shared/types";

export function useGames() {
    return useQuery<GameConfig[]>({
        queryKey: ["games"],
        queryFn: () => window.api.invoke("mod:getGames"),
    });
}

export function useModGroups(selectedGame: string) {
    return useQuery<FolderGroup[]>({
        queryKey: ["mods", selectedGame],
        queryFn: () => window.api.invoke("mod:list", selectedGame),
        enabled: !!selectedGame,
    });
}

export function usePresets(selectedGame: string) {
    return useQuery<Preset[]>({
        queryKey: ["presets", selectedGame],
        queryFn: () => window.api.invoke("mod:getPresets", selectedGame),
        enabled: !!selectedGame,
    });
}
