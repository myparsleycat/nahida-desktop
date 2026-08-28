import { Mod } from "@bindings/mod";
import { toErrorMessage } from "@shared/utils";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function useBulkModToggle() {
    const { t } = useTranslation();
    const queryClient = useQueryClient();
    const [isPending, setIsPending] = useState(false);

    const run = async (
        action: (groupPath: string) => Promise<void>,
        groupPath: string,
        successKey: string,
    ) => {
        if (isPending) return;
        setIsPending(true);
        try {
            await action(groupPath);
            await Promise.all([
                queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] }),
                queryClient.invalidateQueries({ queryKey: ["subGroups", groupPath] }),
            ]);
            toast.success(t(successKey));
        } catch (error) {
            toast.error(toErrorMessage(error));
        } finally {
            setIsPending(false);
        }
    };

    return {
        isPending,
        enableAll: (groupPath: string) =>
            void run(Mod.EnableAll, groupPath, "page.mod.content-header.all_enabled"),
        disableAll: (groupPath: string) =>
            void run(Mod.DisableAll, groupPath, "page.mod.content-header.all_disabled"),
    };
}
