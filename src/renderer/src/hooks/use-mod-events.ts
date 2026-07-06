import { modStore } from "@renderer/store/mod";
import type { QueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function useModRefreshOnFocus(selectedGame: string | null, queryClient: QueryClient) {
    useEffect(() => {
        const handleFocus = () => {
            if (selectedGame) {
                void queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] });
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
    selectedGroupPath: string | undefined,
    queryClient: QueryClient,
) {
    const { t } = useTranslation();

    useEffect(() => {
        const unsubscribe = window.api.on("download:completed", (data) => {
            const invalidations: Promise<unknown>[] = [];

            if (selectedGame) {
                invalidations.push(
                    queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] }),
                );
            }

            if (
                selectedGroupPath &&
                (data.path === selectedGroupPath ||
                    data.path.startsWith(`${selectedGroupPath}\\`) ||
                    data.path.startsWith(`${selectedGroupPath}/`))
            ) {
                invalidations.push(
                    queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] }),
                );
            }

            void Promise.all(invalidations);

            if (!data.disableToast) {
                toast.success(t("page.mod.download_completed", { name: data.name }));
            }
        });

        return () => {
            unsubscribe();
        };
    }, [selectedGame, selectedGroupPath, queryClient, t]);
}

export function useModWatcherEvents(
    selectedGame: string | null,
    selectedGroupPath: string | undefined,
    queryClient: QueryClient,
) {
    useEffect(() => {
        const removeGameListener = window.api.on("mod:update-game", () => {
            const invalidations: Promise<unknown>[] = [];

            if (selectedGame) {
                invalidations.push(
                    queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
                );
            }

            invalidations.push(queryClient.invalidateQueries({ queryKey: ["subGroups"] }));
            void Promise.all(invalidations);
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

            invalidations.push(queryClient.invalidateQueries({ queryKey: ["subGroups"] }));
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
