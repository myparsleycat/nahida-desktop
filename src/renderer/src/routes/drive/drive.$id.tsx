import {
  AkashaBreadcrumb,
  AkashaHeadButtons,
  AkashaSkeleton,
  ContentMenuGrid,
  ContentMenuList,
  HandlerProvider,
} from "@renderer/components/akasha";
import {
  ConflictNameDialog,
  DeleteItemsDialog,
  NewDirectoryDialog,
  // PubLinkDialog,
  RenameDialog,
} from "@renderer/components/akasha/dialogs";
import { Center, ServerCrash } from "@renderer/components/common";
import { ContextMenuProvider } from "@renderer/components/drive/context-menu";
import { DriveImportOverlay } from "@renderer/components/drive/drive-import-overlay";
import { AliceLoader } from "@renderer/components/loaders";
import { Button } from "@renderer/components/ui/button";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useDrag } from "@renderer/hooks/drive";
import { useAuth } from "@renderer/hooks/use-auth";
import { useDriveUploadRefresh } from "@renderer/hooks/use-drive-upload-refresh";
import { useDriveNameSortPolicy } from "@renderer/hooks/use-settings";
import { getSearchScore } from "@renderer/lib/sejong";
import { commonSort, isDriveSearchUnavailable } from "@renderer/lib/utils";
import { useViewStore, viewStore } from "@renderer/store/drive";
import type { Content } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, useLocation } from "@tanstack/react-router";
import { disassemble, getChoseong } from "es-hangul";
import { orderBy } from "es-toolkit";
import { FolderIcon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/drive/drive/$id")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const { id } = Route.useParams();
  const { session } = useAuth();
  const location = useLocation();
  const effectiveId = id === "root" ? (session?.drive.rootId ?? id) : id;

  const { onDragEnter, onDragLeave, onDragOver, onDrop } = useDrag();
  const searchInDirQuery = useViewStore((s) => s.searchInDirQuery);
  const setSearchInDirQuery = useViewStore((s) => s.setSearchInDirQuery);
  const includeSubdirs = useViewStore((s) => s.includeSubdirs);
  const sortType = useViewStore((s) => s.sortType);
  const layout = useViewStore((s) => s.layout);
  const { data: nameSortPolicy = "natural_ignore_spacing" } = useDriveNameSortPolicy();

  useDriveUploadRefresh(effectiveId, ["drive", "drive", effectiveId]);

  const [debouncedQ, setDebouncedQ] = useState("");
  const [extraItems, setExtraItems] = useState<Content[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [isLoadingMore, setIsLoadingMore] = useState(false);

  const query = useQuery({
    queryKey: ["drive", "drive", effectiveId],
    enabled: !!effectiveId,
    placeholderData: (prev) => prev,
    refetchIntervalInBackground: true,
    refetchInterval: () => {
      if (typeof document !== "undefined" && document.hidden) {
        return 60000 * 3; // 3분 (백그라운드)
      }
      return 30000; // 30초 (포그라운드)
    },
    queryFn: async () => {
      const data = await window.api.invoke("drive:get:item", effectiveId);
      return data;
    },
  });

  // biome-ignore lint/correctness/useExhaustiveDependencies: <>
  useEffect(() => {
    if (searchInDirQuery) {
      setSearchInDirQuery("");
    }
    setDebouncedQ("");
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

  const searchQuery = useQuery({
    queryKey: ["drive", "search", effectiveId, debouncedQ],
    enabled: isDescendantSearch,
    staleTime: 30_000,
    refetchOnWindowFocus: false,
    queryFn: async () => {
      try {
        return await window.api.invoke("drive:get:search", effectiveId, {
          q: debouncedQ,
          limit: 50,
        });
      } catch (error) {
        if (isDriveSearchUnavailable(error)) {
          toast.error(t("page.drive.head_buttons.search_unavailable"));
          throw new Error("search_unavailable");
        }
        throw error;
      }
    },
  });

  useEffect(() => {
    setExtraItems([]);
    setNextCursor(null);
    setIsLoadingMore(false);
  }, [effectiveId, debouncedQ]);

  useEffect(() => {
    setExtraItems([]);
    setNextCursor(searchQuery.data?.nextCursor ?? null);
  }, [searchQuery.data]);

  useEffect(() => {
    if (effectiveId && location.pathname.startsWith("/drive/drive/")) {
      const setLastDriveId = viewStore.getState().setLastDriveId;
      setLastDriveId(effectiveId);
    }
  }, [effectiveId, location.pathname]);

  const rawContents = useMemo(() => {
    if (!query.data?.children) return [];
    return commonSort([...query.data.children], sortType, nameSortPolicy);
  }, [query.data?.children, sortType, nameSortPolicy]);

  const localContents = useMemo(() => {
    if (!rawContents) return [];
    if (isSubdirSearchMode || !searchInDirQuery) return rawContents;

    const query = searchInDirQuery.toLowerCase();

    return orderBy(
      rawContents
        .map((item) => {
          const lowerName = item.name.toLowerCase();
          const cachedData = {
            lowerName,
            jamo: disassemble(lowerName),
            chosung: getChoseong(lowerName),
          };
          return {
            item,
            score: getSearchScore(item.name, query, cachedData),
          };
        })
        .filter((scoredItem) => scoredItem.score > 0),
      [(di) => di.score],
      ["desc"],
    ).map((scoredItem) => scoredItem.item);
  }, [rawContents, searchInDirQuery, isSubdirSearchMode]);

  const searchContents = useMemo(() => {
    if (!searchQuery.data) return extraItems;
    return [...searchQuery.data.items, ...extraItems];
  }, [searchQuery.data, extraItems]);

  const displayContents = isDescendantSearch ? searchContents : isSearching ? [] : localContents;
  const isSearchPending =
    isSearching && (!isDescendantSearch || (searchQuery.isFetching && searchContents.length === 0));
  const isSearchFailed = isDescendantSearch && searchQuery.isError;
  const isSearchEmpty =
    isDescendantSearch &&
    searchQuery.isFetched &&
    !searchQuery.isError &&
    searchContents.length === 0;

  async function loadMore() {
    if (!nextCursor || isLoadingMore) return;

    const identity = `${effectiveId}:${debouncedQ}`;
    setIsLoadingMore(true);
    try {
      const data = await window.api.invoke("drive:get:search", effectiveId, {
        q: debouncedQ,
        limit: 50,
        cursor: nextCursor,
      });

      if (identity !== `${effectiveId}:${debouncedQ}`) return;

      setExtraItems((prev) => [...prev, ...data.items]);
      setNextCursor(data.nextCursor);
    } catch {
      if (identity !== `${effectiveId}:${debouncedQ}`) return;
      toast.error(t("page.drive.head_buttons.search_unavailable"));
    } finally {
      if (identity === `${effectiveId}:${debouncedQ}`) {
        setIsLoadingMore(false);
      }
    }
  }

  const handleDrop = async (e: React.DragEvent<HTMLDivElement>) => {
    try {
      await onDrop(e, effectiveId);
    } catch (error) {
      const message = toErrorMessage(error);
      const code =
        typeof error === "object" &&
        error !== null &&
        "code" in error &&
        typeof (error as { code?: unknown }).code === "string"
          ? ((error as { code?: string }).code ?? "")
          : "";

      if (code === "NO_UPLOADABLE_FILES" || message.includes("NO_UPLOADABLE_FILES")) {
        toast.warning(t("page.drive.toast.no_uploadable_files"));
        return;
      }

      throw error;
    }
  };

  if (!query.data && query.isFetching) {
    return (
      <Center>
        <AliceLoader />
      </Center>
    );
  } else if (query.isError) {
    return (
      <Center>
        <ServerCrash message={toErrorMessage(query.error)} />
      </Center>
    );
  }

  if (query.data) {
    return (
      <>
        <div className="flex h-full w-full flex-col select-none">
          <div className="flex h-12 w-full flex-row items-center border-b p-2 select-none">
            {location.pathname !== "/drive/share" ? (
              <AkashaBreadcrumb itemId={effectiveId} ancestors={query.data.ancestors} />
            ) : (
              <div className="flex-1"></div>
            )}

            <AkashaHeadButtons currentId={effectiveId} />
          </div>

          <div
            className="flex flex-1 flex-col overflow-auto"
            onDragEnter={onDragEnter}
            onDragLeave={onDragLeave}
            onDragOver={onDragOver}
            onDrop={handleDrop}
          >
            <ContextMenuProvider>
              <HandlerProvider
                queryData={query}
                sortedContents={displayContents}
                currentId={effectiveId}
              >
                {displayContents.length > 0 ? (
                  <ScrollArea className="flex h-full flex-1 flex-col">
                    <>
                      {layout === "list" ? (
                        <ContentMenuList
                          sortedContents={displayContents}
                          isFetching={query.isFetching}
                          itemId={effectiveId}
                        />
                      ) : layout === "grid" ? (
                        <ContentMenuGrid
                          sortedContents={displayContents}
                          isFetching={query.isFetching}
                          itemId={effectiveId}
                        />
                      ) : null}

                      {isDescendantSearch && nextCursor && (
                        <div className="flex justify-center p-4">
                          <Button
                            variant="outline"
                            disabled={isLoadingMore}
                            onClick={() => {
                              void loadMore();
                            }}
                          >
                            {t("page.drive.head_buttons.search_load_more")}
                          </Button>
                        </div>
                      )}
                    </>
                  </ScrollArea>
                ) : isSearchFailed ? (
                  <Center className="flex-col">
                    <p className="text-center text-lg">
                      {t("page.drive.head_buttons.search_unavailable")}
                    </p>
                  </Center>
                ) : isSearchEmpty ? (
                  <Center className="flex-col">
                    <div>
                      <FolderIcon size="80" />
                    </div>
                    <p className="mt-4 text-center text-lg">
                      {t("page.drive.head_buttons.search_no_results")}
                    </p>
                  </Center>
                ) : isSearchPending ? (
                  <AkashaSkeleton />
                ) : query.isFetched && displayContents.length < 1 ? (
                  <Center className="flex-col">
                    <div>
                      <FolderIcon size="80" />
                    </div>
                    <p className="mt-4 text-center text-lg">
                      {t("page.drive.no_contents_section_message.0")}
                    </p>
                    <p className="text-center text-muted-foreground">
                      {t("page.drive.no_contents_section_message.1")}
                    </p>
                  </Center>
                ) : query.isFetching && displayContents.length === 0 ? (
                  <AkashaSkeleton />
                ) : null}
              </HandlerProvider>
            </ContextMenuProvider>
          </div>
        </div>

        <RenameDialog />
        <DeleteItemsDialog />
        <ConflictNameDialog />
        <NewDirectoryDialog contents={rawContents} />
        {/* <PubLinkDialog /> */}

        <DriveImportOverlay destinationId={effectiveId} />
      </>
    );
  } else {
    return <></>;
  }
}
