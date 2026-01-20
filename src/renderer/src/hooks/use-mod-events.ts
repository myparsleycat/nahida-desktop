import { useEffect } from "react";
import { QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

export function useModRefreshOnFocus(selectedGame: string | null, queryClient: QueryClient) {
    useEffect(() => {
        const handleFocus = () => {
            if (selectedGame) {
                queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] });
            }
        };

        window.addEventListener("focus", handleFocus);
        return () => {
            window.removeEventListener("focus", handleFocus);
        };
    }, [selectedGame, queryClient]);
}

export function useDownloadCompletionHandler(
    selectedGame: string | null,
    queryClient: QueryClient,
) {
    useEffect(() => {
        const unsubscribe = window.api.on("download:completed", (data) => {
            if (selectedGame) {
                queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] });
                toast.success(`"${data.name}" 다운로드가 완료되었습니다.`);
            }
        });

        return () => {
            unsubscribe();
        };
    }, [selectedGame, queryClient]);
}
