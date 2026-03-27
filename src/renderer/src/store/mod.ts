import type { FolderGroup, GameConfig, Preset } from "@shared/types.gen";
import { createStore, useStore } from "zustand";

interface ModState {
    selectedGame: string;
    setSelectedGame: (game: string) => void;
    deletingGame: string | null;
    setDeletingGame: (game: string | null) => void;
    selectedGroup: FolderGroup | null;
    setSelectedGroup: (group: FolderGroup | null) => void;
    selectedPreset: Preset | null;
    setSelectedPreset: (preset: Preset | null) => void;
    newPresetName: string;
    setNewPresetName: (name: string) => void;
    newPresetDescription: string;
    setNewPresetDescription: (description: string) => void;
    isPresetDialogOpen: boolean;
    setIsPresetDialogOpen: (open: boolean) => void;
    isSelectedPresetDialogOpen: boolean;
    setIsSelectedPresetDialogOpen: (open: boolean) => void;
    isAddGameDialogOpen: boolean;
    setIsAddGameDialogOpen: (open: boolean) => void;
    isDeleteGameDialogOpen: boolean;
    setIsDeleteGameDialogOpen: (open: boolean) => void;
    newGameName: string;
    setNewGameName: (name: string) => void;
    newGamePath: string;
    setNewGamePath: (path: string) => void;
    editingGame: GameConfig | null;
    setEditingGame: (game: GameConfig | null) => void;
    editGamePath: string;
    setEditGamePath: (path: string) => void;
    editGameImporter: string | null;
    setEditGameImporter: (importer: string | null) => void;
    isEditGameDialogOpen: boolean;
    setIsEditGameDialogOpen: (open: boolean) => void;
    isCustomDownloadDialogOpen: boolean;
    setIsCustomDownloadDialogOpen: (open: boolean) => void;
    downloadMode: { downloadId: string; suggestedName?: string } | null;
    setDownloadMode: (mode: { downloadId: string; suggestedName?: string } | null) => void;
    searchQuery: string;
    setSearchQuery: (query: string) => void;
    viewMode: "grid" | "list";
    setViewMode: (mode: "grid" | "list") => void;
    sortType: "name" | "date" | "size";
    setSortType: (type: "name" | "date" | "size") => void;
    sortOrder: "asc" | "desc";
    setSortOrder: (order: "asc" | "desc") => void;
    expandedGroups: Set<string>;
    persistentGroups: Set<string>;
    iniListExpandedByGroupPath: Record<string, Record<string, boolean>>;
    toggleExpandedGroup: (path: string) => void;
    togglePersistentGroup: (path: string) => void;
    setExpandedGroup: (path: string, expanded: boolean) => void;
    setIniListExpanded: (groupPath: string, modId: string, expanded: boolean) => void;
    resetIniListExpanded: (groupPath: string) => void;
    initExpandedGroups: () => Promise<void>;
}

export const modStore = createStore<ModState>((set) => ({
    selectedGame: "",
    setSelectedGame: (selectedGame) => set({ selectedGame }),
    deletingGame: null,
    setDeletingGame: (deletingGame) => set({ deletingGame }),
    selectedGroup: null,
    setSelectedGroup: (selectedGroup) => set({ selectedGroup }),
    selectedPreset: null,
    setSelectedPreset: (selectedPreset) => set({ selectedPreset }),
    newPresetName: "",
    setNewPresetName: (newPresetName) => set({ newPresetName }),
    newPresetDescription: "",
    setNewPresetDescription: (newPresetDescription) => set({ newPresetDescription }),
    isPresetDialogOpen: false,
    setIsPresetDialogOpen: (isPresetDialogOpen) => set({ isPresetDialogOpen }),
    isSelectedPresetDialogOpen: false,
    setIsSelectedPresetDialogOpen: (isSelectedPresetDialogOpen) =>
        set({ isSelectedPresetDialogOpen }),
    isAddGameDialogOpen: false,
    setIsAddGameDialogOpen: (isAddGameDialogOpen) => set({ isAddGameDialogOpen }),
    isDeleteGameDialogOpen: false,
    setIsDeleteGameDialogOpen: (isDeleteGameDialogOpen) => set({ isDeleteGameDialogOpen }),
    newGameName: "",
    setNewGameName: (newGameName) => set({ newGameName }),
    newGamePath: "",
    setNewGamePath: (newGamePath) => set({ newGamePath }),
    editingGame: null,
    setEditingGame: (editingGame) => set({ editingGame }),
    editGamePath: "",
    setEditGamePath: (editGamePath) => set({ editGamePath }),
    editGameImporter: null,
    setEditGameImporter: (editGameImporter) => set({ editGameImporter }),
    isEditGameDialogOpen: false,
    setIsEditGameDialogOpen: (isEditGameDialogOpen) => set({ isEditGameDialogOpen }),
    isCustomDownloadDialogOpen: false,
    setIsCustomDownloadDialogOpen: (isCustomDownloadDialogOpen) =>
        set({ isCustomDownloadDialogOpen }),
    downloadMode: null,
    setDownloadMode: (downloadMode) => set({ downloadMode }),
    searchQuery: "",
    setSearchQuery: (searchQuery) => set({ searchQuery }),
    viewMode: "grid",
    setViewMode: (viewMode) => set({ viewMode }),
    sortType: "name",
    setSortType: (sortType) => set({ sortType }),
    sortOrder: "asc",
    setSortOrder: (sortOrder) => set({ sortOrder }),

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

            window.api.invoke("mod:setExpandedGroups", Array.from(nextPersistent));
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

    initExpandedGroups: async () => {
        try {
            const paths = await window.api.invoke("mod:getExpandedGroups");
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
