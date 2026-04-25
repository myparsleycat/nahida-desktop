import type { GameBananaGameKey } from "@renderer/hooks/use-gamebanana-data";
import { createStore, useStore } from "zustand";

const DEFAULT_SUBFEED_PAGE = 1;
const DEFAULT_MODS_PAGE = 1;

interface GameBananaBreadcrumb {
    id: number;
    name: string;
}

interface GameBananaSelectedSubmission {
    id: number;
    modelName: string;
}

interface GameBananaState {
    selectedGame: GameBananaGameKey | "";
    selectedCategoryId?: number;
    categoryBreadcrumbs: GameBananaBreadcrumb[];
    selectedMod?: GameBananaSelectedSubmission;
    subfeedPage: number;
    modsPage: number;
    modSearch: string;
    setSelectedGame: (game: GameBananaGameKey) => void;
    setInitialGame: (game: GameBananaGameKey) => void;
    selectCategory: (categoryId: number, categoryName: string) => void;
    selectMod: (submission: GameBananaSelectedSubmission) => void;
    clearSelectedMod: () => void;
    selectBreadcrumbCategory: (index: number) => void;
    resetToGameHome: () => void;
    setSubfeedPage: (page: number) => void;
    setModsPage: (page: number) => void;
    setModSearch: (query: string) => void;
}

export const gameBananaStore = createStore<GameBananaState>((set, get) => ({
    selectedGame: "",
    selectedCategoryId: undefined,
    categoryBreadcrumbs: [],
    selectedMod: undefined,
    subfeedPage: DEFAULT_SUBFEED_PAGE,
    modsPage: DEFAULT_MODS_PAGE,
    modSearch: "",
    setSelectedGame: (selectedGame) =>
        set({
            selectedGame,
            selectedCategoryId: undefined,
            categoryBreadcrumbs: [],
            selectedMod: undefined,
            subfeedPage: DEFAULT_SUBFEED_PAGE,
            modsPage: DEFAULT_MODS_PAGE,
            modSearch: "",
        }),
    setInitialGame: (selectedGame) => {
        if (!get().selectedGame) {
            set({ selectedGame });
        }
    },
    selectCategory: (categoryId, categoryName) =>
        set((state) => {
            const nextBreadcrumbs =
                state.selectedCategoryId === undefined
                    ? [{ id: categoryId, name: categoryName }]
                    : [...state.categoryBreadcrumbs, { id: categoryId, name: categoryName }];

            return {
                selectedCategoryId: categoryId,
                categoryBreadcrumbs: nextBreadcrumbs,
                selectedMod: undefined,
                modsPage: DEFAULT_MODS_PAGE,
                modSearch: "",
            };
        }),
    selectMod: (selectedMod) => set({ selectedMod }),
    clearSelectedMod: () => set({ selectedMod: undefined }),
    selectBreadcrumbCategory: (index) =>
        set((state) => {
            const nextBreadcrumbs = state.categoryBreadcrumbs.slice(0, index + 1);
            const nextCategory = nextBreadcrumbs.at(-1);
            if (!nextCategory) {
                return state;
            }

            return {
                selectedCategoryId: nextCategory.id,
                categoryBreadcrumbs: nextBreadcrumbs,
                selectedMod: undefined,
                modsPage: DEFAULT_MODS_PAGE,
                modSearch: "",
            };
        }),
    resetToGameHome: () =>
        set({
            selectedCategoryId: undefined,
            categoryBreadcrumbs: [],
            selectedMod: undefined,
            subfeedPage: DEFAULT_SUBFEED_PAGE,
            modsPage: DEFAULT_MODS_PAGE,
            modSearch: "",
        }),
    setSubfeedPage: (subfeedPage) => set({ subfeedPage: Math.max(1, subfeedPage) }),
    setModsPage: (modsPage) => set({ modsPage: Math.max(1, modsPage) }),
    setModSearch: (modSearch) => set({ modSearch }),
}));

export function useGameBananaStore<T>(selector: (state: GameBananaState) => T): T {
    return useStore(gameBananaStore, selector);
}
