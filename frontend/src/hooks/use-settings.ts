import {
    applySettingUpdate,
    getSetting,
    setSetting,
    settingsManyQueryKey,
} from "@renderer/lib/settings";
import type { AppSettings, SettingKey } from "@shared/settings";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useMemo, useRef, type SetStateAction } from "react";

type SettingsConfig = Record<string, SettingKey>;

type SettingsShape<TConfig extends SettingsConfig> = {
    [P in keyof TConfig]: AppSettings[TConfig[P]];
};

type SettingUpdate<T> = T | ((current: T) => T);

// Every window mounts this once at its root; the setting hooks below rely on it to stay current
// because their queries never go stale on their own.
export function useSettingUpdateSync() {
    const queryClient = useQueryClient();

    useEffect(
        () =>
            Events.On("setting:update", (event) => {
                const payload: unknown = Array.isArray(event.data) ? event.data[0] : event.data;
                if (!payload || typeof payload !== "object" || !("key" in payload)) return;
                applySettingUpdate(
                    queryClient,
                    payload.key,
                    "value" in payload ? payload.value : undefined,
                );
            }),
        [queryClient],
    );
}

export function useSetting<K extends SettingKey>(key: K) {
    return useQuery({
        queryKey: ["settings", key] as const,
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

export function useModGridLayoutSettings() {
    const keys = [
        "mod.gridLayoutMode",
        "mod.gridResponsiveBaseWidth",
        "mod.gridFixedCardWidth",
        "mod.gridFixedColumnCount",
    ] as const;
    return useQuery({
        queryKey: settingsManyQueryKey(keys),
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
