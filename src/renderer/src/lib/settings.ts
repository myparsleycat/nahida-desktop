import type { AppSettings, SettingKey } from "@shared/settings";
import type { QueryClient } from "@tanstack/react-query";

type SettingsSubset<K extends readonly SettingKey[]> = {
    [P in K[number]]: AppSettings[P];
};

export function getSetting<K extends SettingKey>(key: K): Promise<AppSettings[K]>;
export function getSetting<K extends readonly SettingKey[]>(keys: K): Promise<SettingsSubset<K>>;
export function getSetting<K extends SettingKey | readonly SettingKey[]>(keyOrKeys: K) {
    if (Array.isArray(keyOrKeys)) {
        return window.api.invoke("setting:getMany", [...keyOrKeys]) as Promise<
            SettingsSubset<Extract<K, readonly SettingKey[]>>
        >;
    }

    const key = keyOrKeys as Extract<K, SettingKey>;

    return window.api.invoke("setting:get", key) as Promise<AppSettings[Extract<K, SettingKey>]>;
}

export function setSetting<K extends SettingKey>(key: K, value: AppSettings[K]) {
    return window.api.invoke("setting:set", key, value);
}

export function settingsManyQueryKey<K extends readonly SettingKey[]>(keys: K) {
    return ["settings", "many", ...keys] as const;
}

export function prefetchSettings<K extends readonly SettingKey[]>(
    queryClient: QueryClient,
    keys: K,
) {
    return queryClient.prefetchQuery({
        queryKey: settingsManyQueryKey(keys),
        queryFn: () => getSetting(keys),
    });
}
