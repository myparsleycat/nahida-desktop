import type { QueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { toast } from "sonner";
import { modStore } from "@renderer/store/mod";

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
                if (!data.disableToast) {
                    toast.success(`"${data.name}" 다운로드가 완료되었습니다.`);
                }
            }
        });

        return () => {
            unsubscribe();
        };
    }, [selectedGame, queryClient]);
}

export function useModWatcherEvents(
    selectedGame: string | null,
    selectedGroupPath: string | undefined,
    queryClient: QueryClient,
) {
    useEffect(() => {
        const removeGameListener = window.api.on("mod:update-game", () => {
            if (selectedGame) {
                queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] });
            }
        });

        const removeModsListener = window.api.on("mod:update-mods", () => {
            const invalidations: Promise<unknown>[] = [];

            if (selectedGame) {
                invalidations.push(
                    queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
                );
            }

            if (selectedGroupPath) {
                invalidations.push(
                    queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] }),
                );
            }

            void Promise.all(invalidations);
        });

        return () => {
            removeGameListener();
            removeModsListener();
        };
    }, [selectedGame, selectedGroupPath, queryClient]);
}

export function useDownloadArchiveExtractPromptHandler() {
    useEffect(() => {
        const unsubscribe = window.api.on("mod:archiveExtractPrompt", (data) => {
            modStore.getState().setArchiveExtractPrompt(data);
        });

        return () => {
            unsubscribe();
        };
    }, []);
}
