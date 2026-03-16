import type {
    FolderGroup,
    GameConfig,
    ModTogglePersistPreset,
    ModTogglePersistState,
    Preset,
} from "@shared/types.gen";
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

export function useModTogglePersistSnapshot(
    selectedGame: string,
    modPath: string,
    enabled = true,
) {
    return useQuery<ModTogglePersistState[]>({
        queryKey: ["modTogglePersistSnapshot", selectedGame, modPath],
        queryFn: () => window.api.invoke("mod:getTogglePersistSnapshot", selectedGame, modPath),
        enabled: enabled && !!selectedGame && !!modPath,
    });
}

export function useModTogglePersistPresets(
    selectedGame: string,
    modPath: string,
    enabled = true,
) {
    return useQuery<ModTogglePersistPreset[]>({
        queryKey: ["modTogglePersistPresets", selectedGame, modPath],
        queryFn: () => window.api.invoke("mod:getTogglePersistPresets", selectedGame, modPath),
        enabled: enabled && !!selectedGame && !!modPath,
    });
}

export function useEnabledImporters() {
    return useQuery({
        queryKey: ["enabledImporters"],
        queryFn: () => window.api.invoke("xxmi:getEnabledImporters"),
    });
}
