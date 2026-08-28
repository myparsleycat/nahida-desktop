import { modStore } from "@renderer/store/mod";
import type { QueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
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
        const off = Events.On("download:completed", (event) => {
            const data = event.data as { path: string; name: string; disableToast?: boolean };
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

        return off;
    }, [selectedGame, selectedGroupPath, queryClient, t]);
}

export function useModWatcherEvents(
    selectedGame: string | null,
    selectedGroupPath: string | undefined,
    queryClient: QueryClient,
) {
    useEffect(() => {
        const removeGameListener = Events.On("mod:update-game", () => {
            const invalidations: Promise<unknown>[] = [];

            if (selectedGame) {
                invalidations.push(
                    queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
                );
            }

            invalidations.push(queryClient.invalidateQueries({ queryKey: ["subGroups"] }));
            void Promise.all(invalidations);
        });

        const handleModUpdate = (_data: unknown) => {
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
        };

        const removeModsListener = Events.On("mod:update-mods", (event) => {
            handleModUpdate(event.data);
        });

        return () => {
            removeGameListener();
            removeModsListener();
        };
    }, [selectedGame, selectedGroupPath, queryClient]);
}

export function useDownloadArchiveExtractPromptHandler() {
    useEffect(() => {
        const off = Events.On("mod:archiveExtractPrompt", (event) => {
            modStore.getState().setArchiveExtractPrompt(event.data);
        });

        return off;
    }, []);
}
