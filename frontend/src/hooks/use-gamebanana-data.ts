import { GameBanana } from "@bindings/gamebanana";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

export type GameBananaGames = Record<string, number>;
type GameBananaGameOverview = Awaited<ReturnType<typeof GameBanana.GetGameOverview>>;
type GameBananaGameSubfeed = Awaited<ReturnType<typeof GameBanana.GetGameSubfeed>>;
type GameBananaModCategoryOverview = Awaited<ReturnType<typeof GameBanana.GetModCategoryOverview>>;
type GameBananaModOverview = Awaited<ReturnType<typeof GameBanana.GetModOverview>>;
type GameBananaModPosts = Awaited<ReturnType<typeof GameBanana.GetModPosts>>;

export type GameBananaGameKey = keyof GameBananaGames;
export type GameBananaModPostsSort = "popular" | "newest";
export type GameBananaModIndexSort =
    | "Generic_Newest"
    | "Generic_MostLiked"
    | "Generic_MostDownloaded"
    | "Generic_MostViewed";
export interface GameBananaSubmissionSelection {
    id: number;
    modelName: string;
}

export function useGameBananaGames(enabled = true) {
    return useQuery<GameBananaGames>({
        queryKey: ["gamebanana", "games"],
        queryFn: async () => ((await GameBanana.GetGames()) ?? {}) as GameBananaGames,
        enabled,
    });
}

export function useGameBananaGameOverview(gameId?: number, enabled = true) {
    return useQuery<GameBananaGameOverview>({
        queryKey: ["gamebanana", "gameOverview", gameId],
        queryFn: () => GameBanana.GetGameOverview(gameId as number),
        enabled: enabled && Number.isFinite(gameId),
    });
}

export function useGameBananaGameSubfeed(gameId?: number, page = 1, enabled = true) {
    return useQuery<GameBananaGameSubfeed>({
        queryKey: ["gamebanana", "gameSubfeed", gameId, page],
        queryFn: () => GameBanana.GetGameSubfeed({ gameId: gameId as number, page }),
        enabled: enabled && Number.isFinite(gameId),
    });
}

export function useGameBananaModCategoryOverview(
    categoryId?: number,
    page = 1,
    sort: GameBananaModIndexSort = "Generic_Newest",
    enabled = true,
) {
    return useQuery<GameBananaModCategoryOverview>({
        queryKey: ["gamebanana", "modCategoryOverview", categoryId, page, sort],
        queryFn: () =>
            GameBanana.GetModCategoryOverview({
                categoryId: categoryId as number,
                page,
                modSort: sort,
            }),
        enabled: enabled && Number.isFinite(categoryId),
    });
}

export function useGameBananaModOverview(
    selection?: GameBananaSubmissionSelection,
    enabled = true,
) {
    return useQuery<GameBananaModOverview>({
        queryKey: ["gamebanana", "modOverview", selection?.modelName, selection?.id],
        queryFn: () =>
            GameBanana.GetModOverview({
                itemId: selection?.id as number,
                modelName: selection?.modelName ?? "Mod",
            }),
        enabled: enabled && Number.isFinite(selection?.id),
    });
}

export function useGameBananaModPosts(
    modId?: number,
    modelName = "Mod",
    page = 1,
    sort: GameBananaModPostsSort = "popular",
    enabled = true,
) {
    return useInfiniteQuery<GameBananaModPosts>({
        queryKey: ["gamebanana", "modPosts", modelName, modId, page, sort],
        initialPageParam: page,
        queryFn: ({ pageParam }) =>
            GameBanana.GetModPosts({
                modId: modId as number,
                modelName,
                page: pageParam as number,
                perPage: 15,
                sort,
            }),
        getNextPageParam: (lastPage, allPages) =>
            lastPage._aMetadata._bIsComplete ? undefined : page + allPages.length,
        enabled: enabled && Number.isFinite(modId),
    });
}
