import { getSetting, setSetting, settingsManyQueryKey } from "@renderer/lib/settings";
import type { AppSettings, SettingKey } from "@shared/settings";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useMemo, useRef, type SetStateAction } from "react";

type SettingsConfig = Record<string, SettingKey>;

type SettingsShape<TConfig extends SettingsConfig> = {
    [P in keyof TConfig]: AppSettings[TConfig[P]];
};

type SettingUpdate<T> = T | ((current: T) => T);

function useInvalidateOnSettingUpdate(keys: readonly SettingKey[], queryKey: readonly unknown[]) {
    const queryClient = useQueryClient();

    useEffect(() => {
        const off = Events.On("setting:update", (event) => {
            const { key, value } = event.data as { key: string; value: unknown };
            if (keys.includes(key as SettingKey)) {
                if (queryKey.length === 2 && queryKey[0] === "settings" && queryKey[1] === key) {
                    queryClient.setQueryData(queryKey, value);
                    return;
                }

                void queryClient.invalidateQueries({ queryKey: [...queryKey] });
            }
        });

        return off;
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

    const settingUpdateQueueRef = useRef(Promise.resolve());

    const setSettings = useCallback(
        (action: SetStateAction<SettingsShape<TConfig>>) => {
            queryClient.setQueryData<SettingsShape<TConfig>>(queryKey, (prev) => {
                const current = prev ?? ({} as SettingsShape<TConfig>);
                if (typeof action === "function") {
                    return (action as (current: SettingsShape<TConfig>) => SettingsShape<TConfig>)(
                        current,
                    );
                }
                return action;
            });
        },
        [queryClient, queryKey],
    );

    const update = useCallback(
        async <K extends keyof TConfig>(
            key: K,
            updateValue: SettingUpdate<SettingsShape<TConfig>[K]>,
        ) => {
            const currentSettings = queryClient.getQueryData<SettingsShape<TConfig>>(queryKey);
            if (currentSettings == null) {
                return;
            }
            const value =
                typeof updateValue === "function"
                    ? (
                          updateValue as (
                              current: SettingsShape<TConfig>[K],
                          ) => SettingsShape<TConfig>[K]
                      )(currentSettings[key])
                    : updateValue;
            const nextSettings = { ...currentSettings, [key]: value };
            const singleSettingQueryKey = ["settings", settingsConfig[key]] as const;
            queryClient.setQueryData(queryKey, nextSettings);
            queryClient.setQueryData<SettingsShape<TConfig>[K]>(singleSettingQueryKey, value);
            const pendingUpdate = settingUpdateQueueRef.current
                .catch(() => {})
                .then(() => setSetting(settingsConfig[key], value));
            settingUpdateQueueRef.current = pendingUpdate;
            await pendingUpdate;
        },
        [queryClient, queryKey, settingsConfig],
    );

    return {
        settings: data ?? ({} as SettingsShape<TConfig>),
        update,
        isLoading: isQueryLoading || data === undefined,
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
