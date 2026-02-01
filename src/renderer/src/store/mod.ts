import { FolderGroup, Preset } from "@shared/types";
import { createStore, useStore } from "zustand";

interface ModState {
    selectedGame: string;
    setSelectedGame: (game: string) => void;
    selectedGroup: FolderGroup | null;
    setSelectedGroup: (group: FolderGroup | null) => void;
    selectedPreset: Preset | null;
    setSelectedPreset: (preset: Preset | null) => void;
    newPresetName: string;
    setNewPresetName: (name: string) => void;
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
    downloadMode: { downloadId: string; suggestedName?: string } | null;
    setDownloadMode: (mode: { downloadId: string; suggestedName?: string } | null) => void;
    searchQuery: string;
    setSearchQuery: (query: string) => void;
    sortType: "name" | "date" | "size";
    setSortType: (type: "name" | "date" | "size") => void;
    sortOrder: "asc" | "desc";
    setSortOrder: (order: "asc" | "desc") => void;
}

export const modStore = createStore<ModState>((set) => ({
    selectedGame: "",
    setSelectedGame: (selectedGame) => set({ selectedGame }),
    selectedGroup: null,
    setSelectedGroup: (selectedGroup) => set({ selectedGroup }),
    selectedPreset: null,
    setSelectedPreset: (selectedPreset) => set({ selectedPreset }),
    newPresetName: "",
    setNewPresetName: (newPresetName) => set({ newPresetName }),
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
    downloadMode: null,
    setDownloadMode: (downloadMode) => set({ downloadMode }),
    searchQuery: "",
    setSearchQuery: (searchQuery) => set({ searchQuery }),
    sortType: "name",
    setSortType: (sortType) => set({ sortType }),
    sortOrder: "asc",
    setSortOrder: (sortOrder) => set({ sortOrder }),
}));

export function useModStore<T>(selector: (state: ModState) => T): T {
    return useStore(modStore, selector);
}
