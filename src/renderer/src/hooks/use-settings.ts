import { getSetting, setSetting } from "@renderer/lib/settings";
import type { AppSettings, SettingKey } from "@shared/settings";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";

type SettingsConfig = Record<string, SettingKey>;

type SettingsShape<TConfig extends SettingsConfig> = {
    [P in keyof TConfig]: AppSettings[TConfig[P]];
};

function useInvalidateOnSettingUpdate(keys: readonly SettingKey[], queryKey: readonly unknown[]) {
    const queryClient = useQueryClient();

    useEffect(() => {
        const removeListener = window.api.on("setting:update", ({ key }) => {
            if (keys.includes(key as SettingKey)) {
                queryClient.invalidateQueries({ queryKey: [...queryKey] });
            }
        });

        return () => removeListener();
    }, [keys, queryClient, queryKey]);
}

export function useSetting<K extends SettingKey>(key: K) {
    const queryKey = useMemo(() => ["settings", key] as const, [key]);

    useInvalidateOnSettingUpdate([key], queryKey);

    return useQuery({
        queryKey,
        queryFn: () => getSetting(key),
        staleTime: Number.POSITIVE_INFINITY,
        refetchOnWindowFocus: false,
    });
}

export function useSettings<TConfig extends SettingsConfig>(settingsConfig: TConfig) {
    const queryClient = useQueryClient();
    const entries = useMemo(
        () => Object.entries(settingsConfig) as [keyof TConfig, TConfig[keyof TConfig]][],
        [settingsConfig],
    );
    const settingKeys = useMemo(() => entries.map(([, key]) => key), [entries]);
    const queryKey = useMemo(() => ["settings", ...settingKeys] as const, [settingKeys]);

    useInvalidateOnSettingUpdate(settingKeys, queryKey);

    const { data, isLoading: isQueryLoading } = useQuery({
        queryKey,
        queryFn: async () => {
            const resolved = await getSetting(settingKeys as readonly SettingKey[]);
            const nextSettings = {} as SettingsShape<TConfig>;

            for (const [alias, settingKey] of entries) {
                nextSettings[alias] = resolved[settingKey] as SettingsShape<TConfig>[typeof alias];
            }

            return nextSettings;
        },
        staleTime: Number.POSITIVE_INFINITY,
        refetchOnWindowFocus: false,
    });

    const [settings, setSettings] = useState<SettingsShape<TConfig>>({} as SettingsShape<TConfig>);
    const [isInitialized, setIsInitialized] = useState(false);

    useEffect(() => {
        if (data) {
            setSettings(data);
            setIsInitialized(true);
        }
    }, [data]);

    const update = async <K extends keyof TConfig>(key: K, value: SettingsShape<TConfig>[K]) => {
        const nextSettings = { ...settings, [key]: value };
        setSettings(nextSettings);
        queryClient.setQueryData(queryKey, nextSettings);
        await setSetting(settingsConfig[key], value);
    };

    return {
        settings,
        update,
        isLoading: isQueryLoading || !isInitialized,
        setSettings,
    };
}

export function useVirtualizationSettings() {
    useInvalidateOnSettingUpdate(
        ["mod.virtualizationEnabled", "mod.virtualizationThreshold"],
        ["settings", "mod.virtualizationEnabled", "mod.virtualizationThreshold"],
    );

    return useQuery({
        queryKey: ["settings", "mod.virtualizationEnabled", "mod.virtualizationThreshold"],
        queryFn: async () => {
            const settings = await getSetting([
                "mod.virtualizationEnabled",
                "mod.virtualizationThreshold",
            ] as const);

            return {
                enabled: settings["mod.virtualizationEnabled"],
                threshold: settings["mod.virtualizationThreshold"],
            };
        },
        staleTime: Number.POSITIVE_INFINITY,
        refetchOnWindowFocus: false,
    });
}

export function useModGridLayoutSettings() {
    useInvalidateOnSettingUpdate(
        [
            "mod.gridLayoutMode",
            "mod.gridResponsiveBaseWidth",
            "mod.gridFixedCardWidth",
            "mod.gridFixedColumnCount",
        ],
        [
            "settings",
            "mod.gridLayoutMode",
            "mod.gridResponsiveBaseWidth",
            "mod.gridFixedCardWidth",
            "mod.gridFixedColumnCount",
        ],
    );

    return useQuery({
        queryKey: [
            "settings",
            "mod.gridLayoutMode",
            "mod.gridResponsiveBaseWidth",
            "mod.gridFixedCardWidth",
            "mod.gridFixedColumnCount",
        ],
        queryFn: async () => {
            const settings = await getSetting([
                "mod.gridLayoutMode",
                "mod.gridResponsiveBaseWidth",
                "mod.gridFixedCardWidth",
                "mod.gridFixedColumnCount",
            ] as const);

            return {
                mode: settings["mod.gridLayoutMode"],
                responsiveBaseWidth: settings["mod.gridResponsiveBaseWidth"],
                fixedCardWidth: settings["mod.gridFixedCardWidth"],
                fixedColumnCount: settings["mod.gridFixedColumnCount"],
            };
        },
        staleTime: Number.POSITIVE_INFINITY,
        refetchOnWindowFocus: false,
    });
}

export function useSidebarLayoutSetting() {
    return useSetting("mod.sidebarLayout");
}

export function useCharacterSidebarWidthSetting() {
    return useSetting("mod.characterSidebarWidth");
}

export function useSearchModPreviewSetting() {
    return useSetting("mod.searchModPreview");
}

export function useDriveNameSortPolicy() {
    return useSetting("drive.nameSortPolicy");
}
