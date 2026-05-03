import { modStore, useModStore } from "@renderer/store/mod";
import type {
    ApplyPresetResult,
    FolderGroup,
    GameConfig,
    ModInfo,
    PresetCreateConflict,
} from "@shared/types";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function useGameMutations() {
    const { t } = useTranslation();
    const queryClient = useQueryClient();
    const selectedGame = useModStore((s) => s.selectedGame);
    const setSelectedGame = useModStore((s) => s.setSelectedGame);
    const setDeletingGame = useModStore((s) => s.setDeletingGame);
    const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
    const setSelectedPreset = useModStore((s) => s.setSelectedPreset);
    const setNewGameName = useModStore((s) => s.setNewGameName);
    const setNewGamePath = useModStore((s) => s.setNewGamePath);
    const setNewGameImporter = useModStore((s) => s.setNewGameImporter);
    const setIsAddGameDialogOpen = useModStore((s) => s.setIsAddGameDialogOpen);
    const setIsDeleteGameDialogOpen = useModStore((s) => s.setIsDeleteGameDialogOpen);
    const setEditingGame = useModStore((s) => s.setEditingGame);
    const setEditGamePath = useModStore((s) => s.setEditGamePath);
    const setEditGameImporter = useModStore((s) => s.setEditGameImporter);
    const setIsEditGameDialogOpen = useModStore((s) => s.setIsEditGameDialogOpen);
    const getMutationErrorMessage = (error: unknown) => {
        if (error instanceof Error) {
            return error.message || "";
        }

        if (typeof error === "object" && error !== null) {
            const maybeError = error as { message?: string; code?: string };
            return maybeError.message || maybeError.code || "";
        }

        return "";
    };

    const addGameMutation = useMutation({
        mutationFn: ({ name, path, importer }: { name: string; path: string; importer: string | null }) =>
            window.api.invoke("mod:addGame", name, path, importer),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["games"] });
            setNewGameName("");
            setNewGamePath("");
            setNewGameImporter(null);
            setIsAddGameDialogOpen(false);
            toast.success(t("page.mod.hooks.use-mod-mutations.add-game-mutation.success"));
        },
        onError: (error) => {
            const errorMessage = (error as Error).message || "";

            if (errorMessage.includes("DUPLICATE_GAME_NAME")) {
                toast.warning(
                    t("page.mod.hooks.use-mod-mutations.add-game-mutation.duplicate-game-name"),
                );
                return;
            }

            if (errorMessage.includes("DUPLICATE_MOD_FOLDER_PATH")) {
                toast.warning(
                    t(
                        "page.mod.hooks.use-mod-mutations.add-game-mutation.duplicate-mod-folder-path",
                    ),
                );
                return;
            }

            if (errorMessage.includes("INVALID_PARAMS")) {
                toast.error(t("page.mod.hooks.use-mod-mutations.add-game-mutation.invalid-params"));
                return;
            }

            toast.error(t("page.mod.hooks.use-mod-mutations.add-game-mutation.failed"));
        },
    });

    const deleteGameMutation = useMutation({
        mutationFn: (game: string) => window.api.invoke("mod:removeGame", game),
        onSuccess: async (_, deletedGame) => {
            const currentGames =
                (queryClient.getQueryData(["games"]) as GameConfig[] | undefined) ?? [];
            const remainingGames = currentGames.filter((game) => game.game !== deletedGame);
            const nextSelectedGame =
                selectedGame === deletedGame ? (remainingGames[0]?.game ?? "") : selectedGame;

            queryClient.setQueryData(["games"], remainingGames);

            setDeletingGame(null);
            setIsDeleteGameDialogOpen(false);
            setSelectedPreset(null);
            setSelectedGroup(null);

            const editingGame = modStore.getState().editingGame;
            if (editingGame?.game === deletedGame) {
                setEditingGame(null);
                setEditGamePath("");
                setEditGameImporter(null);
                setIsEditGameDialogOpen(false);
            }

            if (selectedGame === deletedGame) {
                setSelectedGame(nextSelectedGame);
                await window.api.invoke("mod:setLastGame", nextSelectedGame);
            }

            queryClient.removeQueries({ queryKey: ["characters", deletedGame] });
            queryClient.removeQueries({ queryKey: ["presets", deletedGame] });
            queryClient.removeQueries({ queryKey: ["modGroup"] });
            queryClient.invalidateQueries({ queryKey: ["games"] });
            toast.success(t("page.mod.hooks.use-mod-mutations.delete-game-mutation.success"));
        },
        onError: (err) => {
            toast.error(err.message);
        },
    });

    const updateGameMutation = useMutation({
        mutationFn: ({
            game,
            updates,
        }: {
            game: string;
            updates: { modFolderPath: string; importer: string | null };
        }) => window.api.invoke("mod:updateGame", game, updates),
        onSuccess: async (_, variables) => {
            queryClient.invalidateQueries({ queryKey: ["games"] });

            if (selectedGame === variables.game) {
                setSelectedGroup(null);
                queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] });
                queryClient.invalidateQueries({ queryKey: ["modGroup"] });
                await window.api.invoke("mod:watchGame", variables.game);
            }

            setEditingGame(null);
            setEditGamePath("");
            setEditGameImporter(null);
            setIsEditGameDialogOpen(false);
            toast.success(t("page.mod.hooks.use-mod-mutations.update-game-mutation.success"));
        },
        onError: (error) => {
            const errorMessage = getMutationErrorMessage(error);

            if (errorMessage.includes("DUPLICATE_MOD_FOLDER_PATH")) {
                toast.warning(
                    t(
                        "page.mod.hooks.use-mod-mutations.add-game-mutation.duplicate-mod-folder-path",
                    ),
                );
                return;
            }

            if (errorMessage.includes("INVALID_PARAMS")) {
                toast.error(t("page.mod.hooks.use-mod-mutations.add-game-mutation.invalid-params"));
                return;
            }

            toast.error(
                errorMessage || t("page.mod.hooks.use-mod-mutations.add-game-mutation.failed"),
            );
        },
    });

    return { addGameMutation, deleteGameMutation, updateGameMutation };
}

export function useModMutations() {
    const { t } = useTranslation();
    const queryClient = useQueryClient();
    const selectedGroup = useModStore((s) => s.selectedGroup);

    const updateLocalGroupCache = (refreshedGroup: FolderGroup) => {
        queryClient.setQueryData(["modGroup", refreshedGroup.path], refreshedGroup);
    };

    const showToggleMutationError = (error: unknown) => {
        const errorMessage = (error as Error).message || "";
        if (errorMessage.includes("ALREADY_EXISTS")) {
            const folderName = errorMessage.split("ALREADY_EXISTS:")[1] || t("g.unknown");
            toast.error(
                t("page.mod.hooks.use-mod-mutations.toggle-mutation.0", {
                    name: folderName,
                }),
            );
            return;
        }

        if (errorMessage.includes("MOD_FOLDER_LOCKED")) {
            const processNames = errorMessage.split("MOD_FOLDER_LOCKED|")[1];
            if (processNames) {
                toast.warning(
                    t("page.mod.hooks.use-mod-mutations.toggle-mutation.1", {
                        processes: processNames,
                    }),
                );
            } else {
                toast.warning(t("page.mod.hooks.use-mod-mutations.toggle-mutation.1-fallback"));
            }
            return;
        }

        toast.error(t("page.mod.hooks.use-mod-mutations.toggle-mutation.2"));
    };

    const showRenameMutationError = (error: unknown) => {
        const errorMessage = (error as Error).message || "";
        if (errorMessage.includes("ALREADY_EXISTS")) {
            const folderName = errorMessage.split("ALREADY_EXISTS:")[1] || t("g.unknown");
            toast.error(
                t("page.mod.hooks.use-mod-mutations.rename-mutation.0", {
                    name: folderName,
                }),
            );
            return;
        }

        if (errorMessage.includes("MOD_FOLDER_LOCKED")) {
            const processNames = errorMessage.split("MOD_FOLDER_LOCKED|")[1];
            if (processNames) {
                toast.warning(
                    t("page.mod.hooks.use-mod-mutations.rename-mutation.1", {
                        processes: processNames,
                    }),
                );
            } else {
                toast.warning(t("page.mod.hooks.use-mod-mutations.rename-mutation.1-fallback"));
            }
            return;
        }

        if (
            errorMessage.includes("INVALID_WINDOWS_FILENAME") ||
            errorMessage.includes("INVALID_MOD_NAME")
        ) {
            toast.error(t("page.mod.hooks.use-mod-mutations.rename-mutation.2"));
            return;
        }

        toast.error(t("page.mod.hooks.use-mod-mutations.rename-mutation.3"));
    };

    const toggleModMutation = useMutation({
        mutationFn: async (mod: ModInfo) => {
            try {
                await window.api.invoke("mod:toggle", mod.path);
                const currentGroupPath = selectedGroup?.path;
                if (currentGroupPath) {
                    const refreshedGroup = (await window.api.invoke(
                        "mod:getMods",
                        currentGroupPath,
                    )) as FolderGroup;
                    return refreshedGroup;
                }
                return null;
            } catch (error) {
                showToggleMutationError(error);
                throw error;
            }
        },
        onSuccess: (refreshedGroup) => {
            if (refreshedGroup) {
                updateLocalGroupCache(refreshedGroup);
            }
        },
    });

    const exclusiveToggleModMutation = useMutation({
        mutationFn: async (mod: ModInfo) => {
            try {
                await window.api.invoke("mod:exclusiveToggle", mod.path);
                const currentGroupPath = selectedGroup?.path;
                if (currentGroupPath) {
                    const refreshedGroup = (await window.api.invoke(
                        "mod:getMods",
                        currentGroupPath,
                    )) as FolderGroup;
                    return refreshedGroup;
                }
                return null;
            } catch (error) {
                showToggleMutationError(error);
                throw error;
            }
        },
        onSuccess: (refreshedGroup) => {
            if (refreshedGroup) {
                updateLocalGroupCache(refreshedGroup);
            }
        },
    });

    const updateToggleKeyMutation = useMutation({
        mutationFn: async (params: {
            modPath: string;
            iniFileName: string;
            sectionName: string;
            variable: string;
            value: string;
        }) => {
            await window.api.invoke(
                "mod:updateToggleKey",
                params.modPath,
                params.iniFileName,
                params.sectionName,
                params.variable,
                params.value,
            );
            const currentGroupPath = selectedGroup?.path;
            if (currentGroupPath) {
                const refreshedGroup = (await window.api.invoke(
                    "mod:getMods",
                    currentGroupPath,
                )) as FolderGroup;
                return refreshedGroup;
            }
            return null;
        },
        onSuccess: (refreshedGroup) => {
            if (refreshedGroup) {
                updateLocalGroupCache(refreshedGroup);
            }
        },
    });

    const renameModMutation = useMutation({
        mutationFn: async ({ mod, newName }: { mod: ModInfo; newName: string }) => {
            try {
                await window.api.invoke("mod:rename", mod.path, newName);
                const currentGroupPath = selectedGroup?.path;
                if (currentGroupPath) {
                    const refreshedGroup = (await window.api.invoke(
                        "mod:getMods",
                        currentGroupPath,
                    )) as FolderGroup;
                    return refreshedGroup;
                }
                return null;
            } catch (error) {
                showRenameMutationError(error);
                throw error;
            }
        },
        onSuccess: (refreshedGroup) => {
            if (refreshedGroup) {
                updateLocalGroupCache(refreshedGroup);
            }
            toast.success(t("page.mod.toast.rename-success"));
        },
    });

    return {
        toggleModMutation,
        exclusiveToggleModMutation,
        updateToggleKeyMutation,
        renameModMutation,
    };
}

export function usePresetMutations() {
    const { t } = useTranslation();
    const queryClient = useQueryClient();
    const selectedGame = useModStore((s) => s.selectedGame);
    const newPresetName = useModStore((s) => s.newPresetName);
    const newPresetDescription = useModStore((s) => s.newPresetDescription);
    const setNewPresetName = useModStore((s) => s.setNewPresetName);
    const setNewPresetDescription = useModStore((s) => s.setNewPresetDescription);
    const setIsPresetDialogOpen = useModStore((s) => s.setIsPresetDialogOpen);
    const setIsSelectedPresetDialogOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);
    const setSelectedPreset = useModStore((s) => s.setSelectedPreset);

    const createPresetMutation = useMutation({
        mutationFn: ({ resolveConflicts = false }: { resolveConflicts?: boolean } = {}) => {
            if (!selectedGame) {
                throw new Error("GAME_NOT_SELECTED");
            }
            return window.api.invoke(
                "mod:createPreset",
                selectedGame,
                newPresetName,
                newPresetDescription,
                resolveConflicts,
            );
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["presets", selectedGame] });
            setNewPresetName("");
            setNewPresetDescription("");
            setIsPresetDialogOpen(false);
            toast.success(t("page.mod.hooks.use-mod-mutations.create-preset-mutation.success"));
        },
        onError: (error) => {
            if ((error as Error).message.includes("PRESET_NAME_EXISTS")) {
                toast.error(
                    t("page.mod.hooks.use-mod-mutations.create-preset-mutation.duplicate-name"),
                );
                return;
            }

            if ((error as Error).message.includes("PRESET_CONFLICT_RESOLUTION_FAILED")) {
                toast.error(
                    t(
                        "page.mod.hooks.use-mod-mutations.create-preset-mutation.conflict-resolve-failed",
                    ),
                );
            }
        },
    });

    const getPresetCreateConflicts = async (): Promise<PresetCreateConflict[]> => {
        if (!selectedGame) {
            throw new Error("GAME_NOT_SELECTED");
        }

        return await window.api.invoke("mod:getPresetCreateConflicts", selectedGame);
    };

    const applyPresetMutation = useMutation({
        mutationFn: (presetId: string) =>
            window.api.invoke("mod:applyPreset", presetId) as Promise<ApplyPresetResult>,
        onSuccess: (result) => {
            queryClient.invalidateQueries({ queryKey: ["modGroup"] });
            queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] });
            setIsSelectedPresetDialogOpen(false);
            if (result.missing.length > 0) {
                toast.warning(
                    t("page.mod.hooks.use-mod-mutations.apply-preset-mutation.missing", {
                        count: result.missing.length,
                    }),
                );
                return;
            }
            toast.success(t("page.mod.hooks.use-mod-mutations.apply-preset-mutation.success"));
        },
        onError: (error) => {
            if ((error as Error).message.includes("LEGACY_PRESET_NOT_SUPPORTED")) {
                toast.error(t("page.mod.hooks.use-mod-mutations.apply-preset-mutation.legacy"));
            }
        },
    });

    const deletePresetMutation = useMutation({
        mutationFn: (presetId: string) => window.api.invoke("mod:deletePreset", presetId),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["presets", selectedGame] });
            setSelectedPreset(null);
            setIsSelectedPresetDialogOpen(false);
            toast.success(t("page.mod.hooks.use-mod-mutations.delete-preset-mutation.success"));
        },
    });

    return {
        createPresetMutation,
        getPresetCreateConflicts,
        applyPresetMutation,
        deletePresetMutation,
    };
}
