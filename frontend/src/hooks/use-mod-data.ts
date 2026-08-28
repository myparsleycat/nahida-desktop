import { Mod } from "@bindings/mod";
import { XXMI } from "@bindings/xxmi";
import type { FolderGroup, GameConfig, Preset } from "@shared/types";
import { keepPreviousData, useQuery } from "@tanstack/react-query";

export function useGames() {
    return useQuery<GameConfig[]>({
        queryKey: ["games"],
        queryFn: async () => ((await Mod.GetGames()) ?? []) as GameConfig[],
    });
}

export function useCharacters(selectedGame: string) {
    return useQuery<FolderGroup[]>({
        queryKey: ["characters", selectedGame],
        queryFn: async () => ((await Mod.GetCharacters(selectedGame, null)) ?? []) as FolderGroup[],
        enabled: !!selectedGame,
        placeholderData: keepPreviousData,
    });
}

export function useModGroup(groupPath?: string) {
    const lightQuery = useQuery<FolderGroup>({
        queryKey: ["modGroupLight", groupPath],
        queryFn: async () => (await Mod.GetModsLight(groupPath as string)) as FolderGroup,
        enabled: !!groupPath,
        placeholderData: keepPreviousData,
    });
    const fullQuery = useQuery<FolderGroup>({
        queryKey: ["modGroup", groupPath],
        queryFn: async () => (await Mod.GetMods(groupPath as string)) as FolderGroup,
        enabled: !!groupPath,
        placeholderData: keepPreviousData,
    });

    const hasFreshFull = !fullQuery.isPlaceholderData && fullQuery.data != null;
    const hasFreshLight = !lightQuery.isPlaceholderData && lightQuery.data != null;
    const data = hasFreshFull
        ? fullQuery.data
        : hasFreshLight
          ? lightQuery.data
          : (fullQuery.data ?? lightQuery.data);

    return {
        ...fullQuery,
        data,
        isPending: lightQuery.isPending && fullQuery.isPending,
        isPlaceholderData: !hasFreshFull && !hasFreshLight,
        isFetching: fullQuery.isFetching || lightQuery.isFetching,
    };
}

export function usePresets(selectedGame: string) {
    return useQuery<Preset[]>({
        queryKey: ["presets", selectedGame],
        queryFn: async () => ((await Mod.GetPresets(selectedGame)) ?? []) as Preset[],
        enabled: !!selectedGame,
    });
}

export function useEnabledImporters() {
    return useQuery({
        queryKey: ["enabledImporters"],
        queryFn: () => XXMI.GetEnabledImporters(),
    });
}
