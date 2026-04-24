import type { DriveNameSortPolicy } from "@shared/drive";
import type { IpcHandlers } from "@shared/types.gen";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";

// oxlint-disable-next-line typescript/no-explicit-any
export function useSettings<T extends Record<string, any>>(
    fetchConfig: Record<keyof T, keyof IpcHandlers>,
) {
    const queryClient = useQueryClient();
    const queryKey = ["settings", fetchConfig];

    const { data, isLoading: isQueryLoading } = useQuery({
        queryKey,
        queryFn: async () => {
            const results = {} as T;
            const entries = Object.entries(fetchConfig);
            await Promise.all(
                entries.map(async ([key, ipc]) => {
                    const val = await window.api.invoke(ipc as keyof IpcHandlers);
                    results[key as keyof T] = val;
                }),
            );
            return results;
        },
        staleTime: Number.POSITIVE_INFINITY,
        refetchOnWindowFocus: false,
    });

    const [settings, setSettings] = useState<T>({} as T);
    const [isInitialized, setIsInitialized] = useState(false);

    useEffect(() => {
        if (data) {
            setSettings(data);
            setIsInitialized(true);
        }
    }, [data]);

    const update = useCallback(
        async (key: keyof T, value: T[keyof T], ipc: keyof IpcHandlers) => {
            setSettings((prev) => ({ ...prev, [key]: value }));

            queryClient.setQueryData(queryKey, (old: T | undefined) => {
                return old ? { ...old, [key]: value } : old;
            });

            return (await window.api.invoke(ipc, value)) as unknown;
        },
        [queryClient, queryKey],
    );

    return {
        settings,
        update,
        isLoading: isQueryLoading || !isInitialized,
        setSettings,
    };
}

export function useVirtualizationSettings() {
    const queryClient = useQueryClient();

    useEffect(() => {
        const removeListener = window.api.on("mod:update-settings", () => {
            queryClient.invalidateQueries({ queryKey: ["settings", "mod", "virtualization"] });
        });
        return () => removeListener();
    }, [queryClient]);

    return useQuery({
        queryKey: ["settings", "mod", "virtualization"],
        queryFn: async () => {
            const enabled = await window.api.invoke("setting:mod:getVirtualizationEnabled");
            const threshold = await window.api.invoke("setting:mod:getVirtualizationThreshold");
            return { enabled, threshold };
        },
    });
}

export function useSearchModPreviewSetting() {
    const queryClient = useQueryClient();

    useEffect(() => {
        const removeListener = window.api.on("mod:update-settings", () => {
            queryClient.invalidateQueries({ queryKey: ["settings", "mod", "searchModPreview"] });
        });
        return () => removeListener();
    }, [queryClient]);

    return useQuery({
        queryKey: ["settings", "mod", "searchModPreview"],
        queryFn: async () => {
            return await window.api.invoke("setting:mod:getSearchModPreview");
        },
    });
}

export function useDriveNameSortPolicy() {
    const queryClient = useQueryClient();

    useEffect(() => {
        const removeListener = window.api.on("drive:update-settings", () => {
            queryClient.invalidateQueries({ queryKey: ["settings", "drive", "nameSortPolicy"] });
        });
        return () => removeListener();
    }, [queryClient]);

    return useQuery({
        queryKey: ["settings", "drive", "nameSortPolicy"],
        queryFn: async (): Promise<DriveNameSortPolicy> => {
            return await window.api.invoke("setting:drive:getNameSortPolicy");
        },
    });
}
