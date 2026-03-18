import { useModStore } from "@renderer/store/mod";
import type { ApplyPresetResult, FolderGroup, ModInfo } from "@shared/types.gen";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function useGameMutations() {
    const queryClient = useQueryClient();
    const setNewGameName = useModStore((s) => s.setNewGameName);
    const setNewGamePath = useModStore((s) => s.setNewGamePath);
    const setIsAddGameDialogOpen = useModStore((s) => s.setIsAddGameDialogOpen);

    const addGameMutation = useMutation({
        mutationFn: ({ name, path }: { name: string; path: string }) =>
            window.api.invoke("mod:addGame", name, path),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["games"] });
            setNewGameName("");
            setNewGamePath("");
            setIsAddGameDialogOpen(false);
            toast.success("게임이 추가되었습니다.");
        },
    });

    const deleteGameMutation = useMutation({
        mutationFn: (game: string) => window.api.invoke("mod:removeGame", game),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["games"] });
            toast.success("게임이 삭제되었습니다.");
        },
    });

    return { addGameMutation, deleteGameMutation };
}

export function useModMutations() {
    const { t } = useTranslation();
    const queryClient = useQueryClient();
    const selectedGroup = useModStore((s) => s.selectedGroup);

    const updateLocalGroupCache = (refreshedGroup: FolderGroup) => {
        queryClient.setQueryData(["modGroup", refreshedGroup.path], refreshedGroup);
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
                const errorMessage = (error as Error).message || "";
                if (errorMessage.includes("ALREADY_EXISTS")) {
                    const folderName = errorMessage.split("ALREADY_EXISTS:")[1] || t("g.unknown");
                    toast.error(
                        t("page.mod.hooks.use-mod-mutations.toggle-mutation.0", {
                            name: folderName,
                        }),
                    );
                } else if (errorMessage.includes("EBUSY")) {
                    toast.error(t("page.mod.hooks.use-mod-mutations.toggle-mutation.1"));
                } else {
                    toast.error(t("page.mod.hooks.use-mod-mutations.toggle-mutation.2"));
                }
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
                const errorMessage = (error as Error).message || "";
                if (errorMessage.includes("ALREADY_EXISTS")) {
                    const folderName = errorMessage.split("ALREADY_EXISTS:")[1] || t("g.unknown");
                    toast.error(
                        t("page.mod.hooks.use-mod-mutations.toggle-mutation.0", {
                            name: folderName,
                        }),
                    );
                } else if (errorMessage.includes("EBUSY")) {
                    toast.error(t("page.mod.hooks.use-mod-mutations.toggle-mutation.1"));
                } else {
                    toast.error(t("page.mod.hooks.use-mod-mutations.toggle-mutation.2"));
                }
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
        mutationFn: () => {
            if (!selectedGame) {
                throw new Error("GAME_NOT_SELECTED");
            }
            return window.api.invoke(
                "mod:createPreset",
                selectedGame,
                newPresetName,
                newPresetDescription,
            );
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["presets", selectedGame] });
            setNewPresetName("");
            setNewPresetDescription("");
            setIsPresetDialogOpen(false);
            toast.success("프리셋이 추가되었습니다.");
        },
        onError: (error) => {
            if ((error as Error).message.includes("PRESET_NAME_EXISTS")) {
                toast.error("이미 존재하는 프리셋 이름입니다.");
            }
        },
    });

    const applyPresetMutation = useMutation({
        mutationFn: (presetId: string) =>
            window.api.invoke("mod:applyPreset", presetId) as Promise<ApplyPresetResult>,
        onSuccess: (result) => {
            queryClient.invalidateQueries({ queryKey: ["modGroup"] });
            queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] });
            setIsSelectedPresetDialogOpen(false);
            if (result.missing.length > 0) {
                toast.warning(
                    `프리셋 적용 완료. 누락된 모드 ${result.missing.length}개가 있습니다.`,
                );
                return;
            }
            toast.success("프리셋이 적용되었습니다.");
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
            toast.success("프리셋이 삭제되었습니다.");
        },
    });

    return { createPresetMutation, applyPresetMutation, deletePresetMutation };
}
