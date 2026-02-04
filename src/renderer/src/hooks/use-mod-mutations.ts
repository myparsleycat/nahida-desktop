import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { FolderGroup, ModInfo } from "@shared/types.gen";
import { toast } from "sonner";
import { useModStore } from "@renderer/store/mod";

export function useGameMutations() {
    const queryClient = useQueryClient();
    const setSelectedGame = useModStore((s) => s.setSelectedGame);
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
            setSelectedGame("");
            toast.success("게임이 삭제되었습니다.");
        },
    });

    return { addGameMutation, deleteGameMutation };
}

export function useModMutations() {
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
            } catch (error: any) {
                const errorMessage = error.message || "";
                if (errorMessage.includes("ALREADY_EXISTS")) {
                    const folderName = errorMessage.split("ALREADY_EXISTS:")[1] || "알 수 없는";
                    toast.error(`이미 "${folderName}" 폴더가 존재합니다.`);
                } else {
                    toast.error("모드 상태 변경에 실패했습니다.");
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
            } catch (error: any) {
                const errorMessage = error.message || "";
                if (errorMessage.includes("ALREADY_EXISTS")) {
                    const folderName = errorMessage.split("ALREADY_EXISTS:")[1] || "알 수 없는";
                    toast.error(`이미 "${folderName}" 폴더가 존재합니다.`);
                } else {
                    toast.error("모드 상태 변경에 실패했습니다.");
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

    return { toggleModMutation, exclusiveToggleModMutation, updateToggleKeyMutation };
}

export function usePresetMutations() {
    const queryClient = useQueryClient();
    const selectedGame = useModStore((s) => s.selectedGame);
    const newPresetName = useModStore((s) => s.newPresetName);
    const setNewPresetName = useModStore((s) => s.setNewPresetName);
    const setIsPresetDialogOpen = useModStore((s) => s.setIsPresetDialogOpen);
    const setIsSelectedPresetDialogOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);
    const setSelectedPreset = useModStore((s) => s.setSelectedPreset);

    const createPresetMutation = useMutation({
        mutationFn: () => {
            if (!selectedGame) {
                throw new Error("게임이 선택되지 않았습니다.");
            }
            return window.api.invoke("mod:createPreset", selectedGame, newPresetName);
        },
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["presets", selectedGame] });
            setNewPresetName("");
            setIsPresetDialogOpen(false);
            toast.success("프리셋이 추가되었습니다.");
        },
    });

    const applyPresetMutation = useMutation({
        mutationFn: (presetId: string) => window.api.invoke("mod:applyPreset", presetId),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["modGroup"] });
            queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] });
            setIsSelectedPresetDialogOpen(false);
            toast.success("프리셋이 적용되었습니다.");
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
