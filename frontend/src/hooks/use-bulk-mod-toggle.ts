import { Mod } from "@bindings/mod";
import { toErrorMessage } from "@shared/utils";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

export function useBulkModToggle() {
    const queryClient = useQueryClient();
    const [isPending, setIsPending] = useState(false);

    const run = async (action: (groupPath: string) => Promise<void>, groupPath: string) => {
        if (isPending) return;
        setIsPending(true);
        try {
            await action(groupPath);
            await Promise.all([
                queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] }),
                queryClient.invalidateQueries({ queryKey: ["subGroups", groupPath] }),
            ]);
        } catch (error) {
            toast.error(toErrorMessage(error));
        } finally {
            setIsPending(false);
        }
    };

    return {
        isPending,
        enableAll: (groupPath: string) => void run(Mod.EnableAll, groupPath),
        disableAll: (groupPath: string) => void run(Mod.DisableAll, groupPath),
    };
}
