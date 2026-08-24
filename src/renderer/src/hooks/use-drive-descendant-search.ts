import { isDriveSearchUnavailable } from "@renderer/lib/utils";
import { useViewStore } from "@renderer/store/drive";
import { toErrorMessage } from "@shared/utils";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function useDriveDescendantSearch(effectiveId: string) {
    const { t } = useTranslation();
    const searchInDirQuery = useViewStore((s) => s.searchInDirQuery);
    const setSearchInDirQuery = useViewStore((s) => s.setSearchInDirQuery);
    const includeSubdirs = useViewStore((s) => s.includeSubdirs);

    const [debouncedQ, setDebouncedQ] = useState("");
    const [prevEffectiveId, setPrevEffectiveId] = useState(effectiveId);

    if (effectiveId !== prevEffectiveId) {
        setPrevEffectiveId(effectiveId);
        setDebouncedQ("");
    }

    // biome-ignore lint/correctness/useExhaustiveDependencies: <>
    useEffect(() => {
        if (searchInDirQuery) {
            setSearchInDirQuery("");
        }
    }, [effectiveId]);

    useEffect(() => {
        const timeout = window.setTimeout(() => {
            setDebouncedQ(searchInDirQuery.trim());
        }, 300);

        return () => clearTimeout(timeout);
    }, [searchInDirQuery]);

    const isSubdirSearchMode = includeSubdirs && effectiveId !== "share";
    const trimmedQuery = searchInDirQuery.trim();
    const isSearching = isSubdirSearchMode && trimmedQuery.length >= 2;
    const isDescendantSearch = isSearching && debouncedQ.length >= 2;

    const searchQuery = useInfiniteQuery({
        queryKey: ["drive", "search", effectiveId, debouncedQ],
        enabled: isDescendantSearch,
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        retry: false,
        initialPageParam: undefined as string | undefined,
        queryFn: async ({ pageParam }) => {
            try {
                return await window.api.invoke("drive:get:search", effectiveId, {
                    q: debouncedQ,
                    limit: 50,
                    ...(pageParam === undefined ? {} : { cursor: pageParam }),
                });
            } catch (error) {
                if (isDriveSearchUnavailable(error)) {
                    throw new Error("search_unavailable");
                }
                throw error;
            }
        },
        getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    });

    const searchContents = useMemo(
        () => searchQuery.data?.pages.flatMap((page) => page.items) ?? [],
        [searchQuery.data],
    );

    const isSearchPending =
        isSearching &&
        (!isDescendantSearch || (searchQuery.isFetching && searchContents.length === 0));
    const isSearchFailed = isDescendantSearch && searchQuery.isError && !searchQuery.data;
    const searchErrorMessage = isSearchFailed
        ? isDriveSearchUnavailable(searchQuery.error)
            ? t("page.drive.head_buttons.search_unavailable")
            : toErrorMessage(searchQuery.error)
        : null;
    const isSearchEmpty =
        isDescendantSearch &&
        searchQuery.isFetched &&
        !searchQuery.isError &&
        searchContents.length === 0;

    const loadMore = async () => {
        try {
            await searchQuery.fetchNextPage();
        } catch (error) {
            toast.error(
                isDriveSearchUnavailable(error)
                    ? t("page.drive.head_buttons.search_unavailable")
                    : toErrorMessage(error),
            );
        }
    };

    return {
        isSearching,
        isDescendantSearch,
        searchContents,
        isSearchPending,
        isSearchFailed,
        searchErrorMessage,
        isSearchEmpty,
        hasNextPage: searchQuery.hasNextPage,
        isFetchingNextPage: searchQuery.isFetchingNextPage,
        loadMore,
    };
}
