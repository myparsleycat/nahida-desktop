import type { IpcHandlers } from "@shared/types";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";

export type GameBananaGames = Record<string, number>;
type GameBananaGameOverview = Awaited<ReturnType<IpcHandlers["gamebanana:getGameOverview"]>>;
type GameBananaGameSubfeed = Awaited<ReturnType<IpcHandlers["gamebanana:getGameSubfeed"]>>;
type GameBananaModCategoryOverview = Awaited<
    ReturnType<IpcHandlers["gamebanana:getModCategoryOverview"]>
>;
type GameBananaModOverview = Awaited<ReturnType<IpcHandlers["gamebanana:getModOverview"]>>;
type GameBananaModPosts = Awaited<ReturnType<IpcHandlers["gamebanana:getModPosts"]>>;

export type GameBananaGameKey = keyof GameBananaGames & string;
export type GameBananaModPostsSort = "popular" | "newest";
export interface GameBananaSubmissionSelection {
    id: number;
    modelName: string;
}

export function useGameBananaGames(enabled = true) {
    return useQuery<GameBananaGames>({
        queryKey: ["gamebanana", "games"],
        queryFn: () => window.api.invoke("gamebanana:getGames"),
        enabled,
    });
}

export function useGameBananaGameOverview(gameId?: number, enabled = true) {
    return useQuery<GameBananaGameOverview>({
        queryKey: ["gamebanana", "gameOverview", gameId],
        queryFn: () => window.api.invoke("gamebanana:getGameOverview", gameId as number),
        enabled: enabled && Number.isFinite(gameId),
        // placeholderData: keepPreviousData,
    });
}

export function useGameBananaGameSubfeed(gameId?: number, page = 1, enabled = true) {
    return useQuery<GameBananaGameSubfeed>({
        queryKey: ["gamebanana", "gameSubfeed", gameId, page],
        queryFn: () =>
            window.api.invoke("gamebanana:getGameSubfeed", { gameId: gameId as number, page }),
        enabled: enabled && Number.isFinite(gameId),
        // placeholderData: keepPreviousData,
    });
}

export function useGameBananaModCategoryOverview(categoryId?: number, page = 1, enabled = true) {
    return useQuery<GameBananaModCategoryOverview>({
        queryKey: ["gamebanana", "modCategoryOverview", categoryId, page],
        queryFn: () =>
            window.api.invoke("gamebanana:getModCategoryOverview", {
                categoryId: categoryId as number,
                page,
            }),
        enabled: enabled && Number.isFinite(categoryId),
        // placeholderData: keepPreviousData,
    });
}

export function useGameBananaModOverview(
    selection?: GameBananaSubmissionSelection,
    enabled = true,
) {
    return useQuery<GameBananaModOverview>({
        queryKey: ["gamebanana", "modOverview", selection?.modelName, selection?.id],
        queryFn: () =>
            window.api.invoke("gamebanana:getModOverview", {
                itemId: selection?.id as number,
                modelName: selection?.modelName ?? "Mod",
            }),
        enabled: enabled && Number.isFinite(selection?.id),
        // placeholderData: keepPreviousData,
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
            window.api.invoke("gamebanana:getModPosts", {
                modId: modId as number,
                modelName,
                page: pageParam,
                perPage: 15,
                sort,
            }),
        getNextPageParam: (lastPage, allPages) =>
            lastPage._aMetadata._bIsComplete ? undefined : page + allPages.length,
        enabled: enabled && Number.isFinite(modId),
    });
}
