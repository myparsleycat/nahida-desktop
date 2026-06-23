import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

type BulkChannel = "mod:enableAll" | "mod:disableAll";

export function useBulkModToggle() {
    const { t } = useTranslation();
    const queryClient = useQueryClient();
    const [isPending, setIsPending] = useState(false);

    const run = async (channel: BulkChannel, groupPath: string, successKey: string) => {
        if (isPending) return;
        setIsPending(true);
        try {
            await window.api.invoke(channel, groupPath);
            await Promise.all([
                queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] }),
                queryClient.invalidateQueries({ queryKey: ["subGroups", groupPath] }),
            ]);
            toast.success(t(successKey));
        } catch (error) {
            toast.error(error instanceof Error ? error.message : String(error));
        } finally {
            setIsPending(false);
        }
    };

    return {
        isPending,
        enableAll: (groupPath: string) =>
            void run("mod:enableAll", groupPath, "page.mod.content-header.all_enabled"),
        disableAll: (groupPath: string) =>
            void run("mod:disableAll", groupPath, "page.mod.content-header.all_disabled"),
    };
}
