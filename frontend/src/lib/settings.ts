import { Setting } from "@bindings/setting";
import { APP_SETTINGS, type AppSettings, type SettingKey } from "@shared/settings";
import type { QueryClient } from "@tanstack/react-query";

type SettingsSubset<K extends readonly SettingKey[]> = {
    [P in K[number]]: AppSettings[P];
};

export function getSetting<K extends SettingKey>(key: K): Promise<AppSettings[K]>;
export function getSetting<K extends readonly SettingKey[]>(keys: K): Promise<SettingsSubset<K>>;
export function getSetting<K extends SettingKey | readonly SettingKey[]>(keyOrKeys: K) {
    if (Array.isArray(keyOrKeys)) {
        return Setting.GetMany(keyOrKeys) as unknown as Promise<
            SettingsSubset<Extract<K, readonly SettingKey[]>>
        >;
    }

    const key = keyOrKeys as Extract<K, SettingKey>;

    return Setting.Get(key) as unknown as Promise<AppSettings[Extract<K, SettingKey>]>;
}

export function setSetting<K extends SettingKey>(key: K, value: AppSettings[K]) {
    return Setting.Set(key, value);
}

export function settingsManyQueryKey<K extends readonly SettingKey[]>(keys: K) {
    return ["settings", "many", ...keys] as const;
}

// Settings queries never go stale on their own, so every cached copy of a changed setting is
// updated here, including copies held for consumers that are not mounted.
export function applySettingUpdate(queryClient: QueryClient, key: unknown, value: unknown) {
    const definition = Object.values(APP_SETTINGS).find(
        (setting) => setting.publicKey === key || setting.storageKey === key,
    );
    if (!definition) {
        return;
    }

    // The advanced editor reports the storage key with the raw stored text, not the typed value.
    const typed = definition.publicKey === key;
    if (typed) {
        queryClient.setQueryData(["settings", definition.publicKey], value);
    }
    void queryClient.invalidateQueries({
        predicate: (query) =>
            query.queryKey[0] === "settings" &&
            query.queryKey.includes(definition.publicKey) &&
            (!typed || query.queryKey[1] === "many"),
    });
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
