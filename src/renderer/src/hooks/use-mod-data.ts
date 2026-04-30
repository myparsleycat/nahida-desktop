import type { FolderGroup, GameConfig, Preset } from "@shared/types";
import { keepPreviousData, useQuery } from "@tanstack/react-query";

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
        queryFn: () => window.api.invoke("mod:getMods", groupPath as string),
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

export function useEnabledImporters() {
    return useQuery({
        queryKey: ["enabledImporters"],
        queryFn: () => window.api.invoke("xxmi:getEnabledImporters"),
    });
}
