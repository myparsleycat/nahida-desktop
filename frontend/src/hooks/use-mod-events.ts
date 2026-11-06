import { modStore } from "@renderer/store/mod";
import { getParentGroupPath } from "@shared/mod";
import type { GameConfig } from "@shared/types";
import type { QueryClient, QueryKey } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { normalizeModDownloadPath } from "./use-mod-download-transfer";

const WATCHER_INVALIDATION_DELAY_MS = 100;

export function useModRefreshOnFocus(selectedGame: string | null, queryClient: QueryClient) {
    useEffect(() => {
        const handleFocus = () => {
            if (selectedGame) {
                void queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] });
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
                    queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
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
        // A change one level inside a top-level folder reaches both the game and the folder watcher,
        // so their invalidations are merged to scan each listing once.
        const pending = new Map<string, QueryKey>();
        let timer: ReturnType<typeof setTimeout> | undefined;
        const flush = () => {
            clearTimeout(timer);
            timer = undefined;
            const queryKeys = [...pending.values()];
            pending.clear();
            void Promise.all(
                queryKeys.map((queryKey) => queryClient.invalidateQueries({ queryKey })),
            );
        };
        const enqueue = (queryKeys: QueryKey[]) => {
            for (const queryKey of queryKeys) {
                pending.set(JSON.stringify(queryKey), queryKey);
            }
            clearTimeout(timer);
            timer = setTimeout(flush, WATCHER_INVALIDATION_DELAY_MS);
        };
        const characterKeys: QueryKey[] = selectedGame ? [["characters", selectedGame]] : [];

        const removeGameListener = Events.On("mod:update-game", () => {
            enqueue([...characterKeys, ["subGroups"]]);
        });

        const removeModsListener = Events.On("mod:update-mods", () => {
            enqueue([
                ...characterKeys,
                ...(selectedGroupPath ? [["modGroup", selectedGroupPath]] : []),
                ...selectedGroupSubGroupKeys(queryClient, selectedGame, selectedGroupPath),
            ]);
        });

        return () => {
            removeGameListener();
            removeModsListener();
            if (timer !== undefined) {
                flush();
            }
        };
    }, [selectedGame, selectedGroupPath, queryClient]);
}

// Group counts cover direct children only, so a change inside the selected group can alter the listings of
// the group and of its ancestors below the mod folder.
function selectedGroupSubGroupKeys(
    queryClient: QueryClient,
    selectedGame: string | null,
    selectedGroupPath: string | undefined,
): QueryKey[] {
    if (!selectedGroupPath) {
        return [["subGroups"]];
    }
    const modFolderPath = queryClient
        .getQueryData<GameConfig[]>(["games"])
        ?.find((game) => game.game === selectedGame)?.modFolderPath;
    if (!modFolderPath) {
        return [["subGroups"]];
    }

    const root = normalizeModDownloadPath(modFolderPath);
    const keys: QueryKey[] = [];
    for (
        let path: string | null = selectedGroupPath;
        path && normalizeModDownloadPath(path).startsWith(`${root}/`);
        path = getParentGroupPath(path)
    ) {
        keys.push(["subGroups", path]);
    }
    if (keys.length === 0) {
        return [["subGroups"]];
    }

    // The folder watcher also reports changes two levels down, which alter the listings of expanded subgroups.
    const selected = normalizeModDownloadPath(selectedGroupPath);
    for (const query of queryClient.getQueryCache().findAll({ queryKey: ["subGroups"] })) {
        const path = query.queryKey[1];
        if (typeof path === "string" && normalizeModDownloadPath(path).startsWith(`${selected}/`)) {
            keys.push(query.queryKey);
        }
    }
    return keys;
}

export function useDownloadArchiveExtractPromptHandler() {
    useEffect(() => {
        const offPrompt = Events.On("mod:archiveExtractPrompt", (event) => {
            modStore.getState().setArchiveExtractPrompt(event.data);
        });
        // The backend drops a prompt whose download was cancelled, so its dialog can no longer be answered.
        const offDismissed = Events.On("mod:archiveExtractPromptDismissed", (event) => {
            const state = modStore.getState();
            if (state.archiveExtractPrompt?.requestId === event.data.requestId) {
                state.setArchiveExtractPrompt(null);
            }
        });

        return () => {
            offPrompt();
            offDismissed();
        };
    }, []);
}
