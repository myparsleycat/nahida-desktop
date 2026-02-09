import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

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
