import { useQuery, keepPreviousData } from "@tanstack/react-query";
import type { FolderGroup, Preset, GameConfig } from "@shared/types.gen";

export function useGames() {
    return useQuery<GameConfig[]>({
        queryKey: ["games"],
        queryFn: () => window.api.invoke("mod:getGames"),
    });
}

export function useCharacters(selectedGame: string) {
    return useQuery<FolderGroup[]>({
        queryKey: ["characters", selectedGame],
        queryFn: () => window.api.invoke("mod:getCharacters", selectedGame),
        enabled: !!selectedGame,
        placeholderData: keepPreviousData,
    });
}

export function useModGroup(groupPath?: string) {
    return useQuery<FolderGroup>({
        queryKey: ["modGroup", groupPath],
        queryFn: () => window.api.invoke("mod:getMods", groupPath!),
        enabled: !!groupPath,
        placeholderData: keepPreviousData,
    });
}

export function usePresets(selectedGame: string) {
    return useQuery<Preset[]>({
        queryKey: ["presets", selectedGame],
        queryFn: () => window.api.invoke("mod:getPresets", selectedGame),
        enabled: !!selectedGame,
    });
}
