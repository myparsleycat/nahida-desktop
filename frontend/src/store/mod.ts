import { Mod } from "@bindings/mod";
import type { DownloadSource } from "@shared/mod";
import type { FolderGroup, GameConfig, Preset } from "@shared/types";
import { createStore, useStore } from "zustand";

export type FolderSortKey = "name" | "mod-count" | "enabled-mod-count";
export type FolderSortDirection = "ascending" | "descending";
export type DownloadMode = NonNullable<ModState["downloadMode"]>;

interface ModState {
    selectedGame: string;
    setSelectedGame: (game: string) => void;
    deletingGame: string | null;
    setDeletingGame: (game: string | null) => void;
    selectedGroup: FolderGroup | null;
    setSelectedGroup: (group: FolderGroup | null) => void;
    selectedPreset: Preset | null;
    setSelectedPreset: (preset: Preset | null) => void;
    isPresetDialogOpen: boolean;
    setIsPresetDialogOpen: (open: boolean) => void;
    isSelectedPresetDialogOpen: boolean;
    setIsSelectedPresetDialogOpen: (open: boolean) => void;
    isAddGameDialogOpen: boolean;
    setIsAddGameDialogOpen: (open: boolean) => void;
    isDeleteGameDialogOpen: boolean;
    setIsDeleteGameDialogOpen: (open: boolean) => void;
    editingGame: GameConfig | null;
    setEditingGame: (game: GameConfig | null) => void;
    isEditGameDialogOpen: boolean;
    setIsEditGameDialogOpen: (open: boolean) => void;
    isNteLaunchDialogOpen: boolean;
    setIsNteLaunchDialogOpen: (open: boolean) => void;
    isCustomDownloadDialogOpen: boolean;
    setIsCustomDownloadDialogOpen: (open: boolean) => void;
    downloadMode: {
        downloadId: string;
        suggestedName?: string;
        suggestedNames?: string[];
        downloadTargetName?: string;
        downloadImporterKey?: string;
        downloadSource: DownloadSource;
    } | null;
    setDownloadMode: (
        mode: {
            downloadId: string;
            suggestedName?: string;
            suggestedNames?: string[];
            downloadTargetName?: string;
            downloadImporterKey?: string;
            downloadSource: DownloadSource;
        } | null,
    ) => void;
    userSelectedDuringDownload: boolean;
    markUserSelectedDuringDownload: () => void;
    resetUserSelectedDuringDownload: () => void;
    archiveExtractPrompt: { requestId: string; fileName: string } | null;
    setArchiveExtractPrompt: (prompt: { requestId: string; fileName: string } | null) => void;
    searchQuery: string;
    setSearchQuery: (query: string) => void;
    viewMode: "grid" | "list";
    setViewMode: (mode: "grid" | "list") => void;
    sortType: "name" | "date" | "size";
    setSortType: (type: "name" | "date" | "size") => void;
    sortOrder: "asc" | "desc";
    setSortOrder: (order: "asc" | "desc") => void;
    folderSortKey: FolderSortKey;
    setFolderSortKey: (key: FolderSortKey) => void;
    folderSortDirection: FolderSortDirection;
    setFolderSortDirection: (direction: FolderSortDirection) => void;
    expandedGroups: Set<string>;
    persistentGroups: Set<string>;
    iniListExpandedByGroupPath: Record<string, Record<string, boolean>>;
    toggleExpandedGroup: (path: string) => void;
    togglePersistentGroup: (path: string) => void;
    setExpandedGroup: (path: string, expanded: boolean) => void;
    setIniListExpanded: (groupPath: string, modId: string, expanded: boolean) => void;
    resetIniListExpanded: (groupPath: string) => void;
    initExpandedGroups: () => Promise<void>;
    isMergeMode: boolean;
    selectedModPaths: Set<string>;
    isMergeDialogOpen: boolean;
    enterMergeMode: () => void;
    exitMergeMode: () => void;
    toggleMergeSelection: (modPath: string) => void;
    removeMergeSelections: (modPaths: string[]) => void;
    setMergeDialogOpen: (open: boolean) => void;
}

export const modStore = createStore<ModState>((set) => ({
    selectedGame: "",
    setSelectedGame: (selectedGame) =>
        set({
            selectedGame,
            isMergeMode: false,
            selectedModPaths: new Set(),
            isMergeDialogOpen: false,
        }),
    deletingGame: null,
    setDeletingGame: (deletingGame) => set({ deletingGame }),
    selectedGroup: null,
    setSelectedGroup: (selectedGroup) =>
        set({
            selectedGroup,
            isMergeMode: false,
            selectedModPaths: new Set(),
            isMergeDialogOpen: false,
        }),
    selectedPreset: null,
    setSelectedPreset: (selectedPreset) => set({ selectedPreset }),
    isPresetDialogOpen: false,
    setIsPresetDialogOpen: (isPresetDialogOpen) => set({ isPresetDialogOpen }),
    isSelectedPresetDialogOpen: false,
    setIsSelectedPresetDialogOpen: (isSelectedPresetDialogOpen) =>
        set({ isSelectedPresetDialogOpen }),
    isAddGameDialogOpen: false,
    setIsAddGameDialogOpen: (isAddGameDialogOpen) => set({ isAddGameDialogOpen }),
    isDeleteGameDialogOpen: false,
    setIsDeleteGameDialogOpen: (isDeleteGameDialogOpen) => set({ isDeleteGameDialogOpen }),
    editingGame: null,
    setEditingGame: (editingGame) => set({ editingGame }),
    isEditGameDialogOpen: false,
    setIsEditGameDialogOpen: (isEditGameDialogOpen) => set({ isEditGameDialogOpen }),
    isNteLaunchDialogOpen: false,
    setIsNteLaunchDialogOpen: (isNteLaunchDialogOpen) => set({ isNteLaunchDialogOpen }),
    isCustomDownloadDialogOpen: false,
    setIsCustomDownloadDialogOpen: (isCustomDownloadDialogOpen) =>
        set({ isCustomDownloadDialogOpen }),
    downloadMode: null,
    setDownloadMode: (downloadMode) => set({ downloadMode }),
    userSelectedDuringDownload: false,
    markUserSelectedDuringDownload: () =>
        set((state) => (state.downloadMode ? { userSelectedDuringDownload: true } : state)),
    resetUserSelectedDuringDownload: () => set({ userSelectedDuringDownload: false }),
    archiveExtractPrompt: null,
    setArchiveExtractPrompt: (archiveExtractPrompt) => set({ archiveExtractPrompt }),
    searchQuery: "",
    setSearchQuery: (searchQuery) => set({ searchQuery }),
    viewMode: "grid",
    setViewMode: (viewMode) => set({ viewMode }),
    sortType: "name",
    setSortType: (sortType) => set({ sortType }),
    sortOrder: "asc",
    setSortOrder: (sortOrder) => set({ sortOrder }),
    folderSortKey: "name",
    setFolderSortKey: (folderSortKey) => set({ folderSortKey }),
    folderSortDirection: "ascending",
    setFolderSortDirection: (folderSortDirection) => set({ folderSortDirection }),

    expandedGroups: new Set<string>(),
    persistentGroups: new Set<string>(),
    iniListExpandedByGroupPath: {},

    toggleExpandedGroup: (path) =>
        set((state) => {
            const next = new Set(state.expandedGroups);
            if (next.has(path)) {
                next.delete(path);
            } else {
                next.add(path);
            }
            return { expandedGroups: next };
        }),

    togglePersistentGroup: (path) =>
        set((state) => {
            const nextExpanded = new Set(state.expandedGroups);
            const nextPersistent = new Set(state.persistentGroups);

            if (nextPersistent.has(path)) {
                nextPersistent.delete(path);
            } else {
                nextPersistent.add(path);
                nextExpanded.add(path);
            }

            void Mod.SetExpandedGroups(Array.from(nextPersistent));
            return { expandedGroups: nextExpanded, persistentGroups: nextPersistent };
        }),

    setExpandedGroup: (path, expanded) =>
        set((state) => {
            const next = new Set(state.expandedGroups);
            if (expanded) {
                next.add(path);
            } else {
                next.delete(path);
            }
            return { expandedGroups: next };
        }),

    setIniListExpanded: (groupPath, modId, expanded) =>
        set((state) => {
            const prevGroupState = state.iniListExpandedByGroupPath[groupPath] ?? {};
            if (prevGroupState[modId] === expanded) {
                return state;
            }

            return {
                iniListExpandedByGroupPath: {
                    ...state.iniListExpandedByGroupPath,
                    [groupPath]: {
                        ...prevGroupState,
                        [modId]: expanded,
                    },
                },
            };
        }),

    resetIniListExpanded: (groupPath) =>
        set((state) => {
            if (!(groupPath in state.iniListExpandedByGroupPath)) {
                return state;
            }

            const next = { ...state.iniListExpandedByGroupPath };
            delete next[groupPath];
            return { iniListExpandedByGroupPath: next };
        }),

    isMergeMode: false,
    selectedModPaths: new Set<string>(),
    isMergeDialogOpen: false,
    enterMergeMode: () => set({ isMergeMode: true, selectedModPaths: new Set() }),
    exitMergeMode: () =>
        set({ isMergeMode: false, selectedModPaths: new Set(), isMergeDialogOpen: false }),
    toggleMergeSelection: (modPath) =>
        set((state) => {
            const next = new Set(state.selectedModPaths);
            if (next.has(modPath)) next.delete(modPath);
            else next.add(modPath);
            return { selectedModPaths: next };
        }),
    removeMergeSelections: (modPaths) =>
        set((state) => {
            const next = new Set(state.selectedModPaths);
            const changed = modPaths.reduce(
                (removed, modPath) => next.delete(modPath) || removed,
                false,
            );
            if (!changed) {
                return state;
            }
            return {
                selectedModPaths: next,
                isMergeDialogOpen: next.size >= 2 && state.isMergeDialogOpen,
            };
        }),
    setMergeDialogOpen: (isMergeDialogOpen) => set({ isMergeDialogOpen }),

    initExpandedGroups: async () => {
        try {
            const paths = await Mod.GetExpandedGroups();
            if (paths && Array.isArray(paths)) {
                const pathSet = new Set(paths);
                set((state) => ({
                    persistentGroups: pathSet,
                    // 기존에 임시로 확장한 그룹들을 유지하면서 새로 불러온 영구 지정 그룹들을 병합
                    expandedGroups: new Set([...state.expandedGroups, ...paths]),
                }));
            }
        } catch (error) {
            console.error("Failed to initialize expanded groups", error);
        }
    },
}));

export function useModStore<T>(selector: (state: ModState) => T): T {
    return useStore(modStore, selector);
}
