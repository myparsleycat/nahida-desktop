import { getSetting, setSetting, settingsManyQueryKey } from "@renderer/lib/settings";
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
        const removeListener = window.api.on("setting:update", ({ key, value }) => {
            if (keys.includes(key as SettingKey)) {
                if (queryKey.length === 2 && queryKey[0] === "settings" && queryKey[1] === key) {
                    queryClient.setQueryData(queryKey, value);
                    return;
                }

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
    const queryKey = useMemo(() => settingsManyQueryKey(settingKeys), [settingKeys]);

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
        const singleSettingQueryKey = ["settings", settingsConfig[key]] as const;
        setSettings(nextSettings);
        queryClient.setQueryData(queryKey, nextSettings);
        queryClient.setQueryData<SettingsShape<TConfig>[K]>(singleSettingQueryKey, value);
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
    const keys = ["mod.virtualizationEnabled", "mod.virtualizationThreshold"] as const;
    const queryKey = settingsManyQueryKey(keys);

    useInvalidateOnSettingUpdate(keys, queryKey);

    return useQuery({
        queryKey,
        queryFn: async () => {
            const settings = await getSetting(keys);

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
    const keys = [
        "mod.gridLayoutMode",
        "mod.gridResponsiveBaseWidth",
        "mod.gridFixedCardWidth",
        "mod.gridFixedColumnCount",
    ] as const;
    const queryKey = settingsManyQueryKey(keys);

    useInvalidateOnSettingUpdate(keys, queryKey);

    return useQuery({
        queryKey,
        queryFn: async () => {
            const settings = await getSetting(keys);

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
