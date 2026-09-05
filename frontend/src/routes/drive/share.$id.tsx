import { Drive } from "@bindings/drive";
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
  UnsupportedExtensionsDialog,
} from "@renderer/components/akasha/dialogs";
import { Center, ServerCrash } from "@renderer/components/common";
import { ContextMenuProvider } from "@renderer/components/drive/context-menu";
import { DriveUploadDropOverlay } from "@renderer/components/drive/drive-upload-drop-overlay";
import { AliceLoader } from "@renderer/components/loaders";
import { Button } from "@renderer/components/ui/button";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useDrag } from "@renderer/hooks/drive";
import { useDriveDescendantSearch } from "@renderer/hooks/use-drive-descendant-search";
import { useDriveUploadRefresh } from "@renderer/hooks/use-drive-upload-refresh";
import { useDriveNameSortPolicy } from "@renderer/hooks/use-settings";
import { getSearchScore } from "@renderer/lib/sejong";
import { commonSort } from "@renderer/lib/utils";
import { useViewStore, viewStore } from "@renderer/store/drive";
import { FileDropTargetID, useWindowFileDrop } from "@renderer/wails/file-drop";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { createFileRoute, useLocation } from "@tanstack/react-router";
import { disassemble, getChoseong } from "es-hangul";
import { orderBy } from "es-toolkit";
import { FolderIcon, Share2Icon } from "lucide-react";
import { useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export const Route = createFileRoute("/drive/share/$id")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const { id } = Route.useParams();
  const location = useLocation();
  const effectiveId = id === "root" ? "share" : id;

  const {
    onDragEnter,
    onDragLeave,
    onDragOver,
    onDrop,
    uploadDragging,
    clearDragging,
    uploadPaths,
  } = useDrag();
  const searchInDirQuery = useViewStore((s) => s.searchInDirQuery);
  const includeSubdirs = useViewStore((s) => s.includeSubdirs);
  const sortType = useViewStore((s) => s.sortType);
  const layout = useViewStore((s) => s.layout);
  const { data: nameSortPolicy = "natural_ignore_spacing" } = useDriveNameSortPolicy();
  const queryKey = useMemo(() => ["drive", "share", effectiveId] as const, [effectiveId]);

  useDriveUploadRefresh(effectiveId, queryKey);

  const {
    isSearching,
    isDescendantSearch,
    searchContents,
    isSearchPending,
    isSearchFailed,
    searchErrorMessage,
    isSearchEmpty,
    hasNextPage,
    isFetchingNextPage,
    loadMore,
  } = useDriveDescendantSearch(effectiveId);

  const query = useQuery({
    queryKey,
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
      const data = await Drive.GetItem(effectiveId);
      return data;
    },
  });

  const isSubdirSearchMode = includeSubdirs && effectiveId !== "share";

  useEffect(() => {
    if (effectiveId && location.pathname.startsWith("/drive/share/")) {
      const setLastShareId = viewStore.getState().setLastShareId;
      setLastShareId(effectiveId);
    }
  }, [effectiveId, location.pathname]);

  const rawContents = useMemo(() => {
    const children = query.data?.children;
    if (!children) return [];
    return commonSort([...children], sortType, nameSortPolicy);
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

  const displayContents = isDescendantSearch ? searchContents : isSearching ? [] : localContents;

  const handleUploadPaths = async (paths: string[]) => {
    try {
      await uploadPaths(paths, effectiveId);
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

      toast.error(message);
    }
  };

  useWindowFileDrop(({ paths, target }) => {
    if (target.id === FileDropTargetID.driveContent) {
      clearDragging();
      void handleUploadPaths(paths);
    }
  });

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
              <AkashaBreadcrumb itemId={effectiveId} ancestors={query.data.ancestors ?? []} />
            ) : (
              <div className="flex-1"></div>
            )}

            <AkashaHeadButtons currentId={effectiveId} onUploadPaths={handleUploadPaths} />
          </div>

          <div
            id={FileDropTargetID.driveContent}
            data-file-drop-target
            className="relative flex flex-1 flex-col overflow-auto"
            onDragEnter={onDragEnter}
            onDragLeave={onDragLeave}
            onDragOver={onDragOver}
            onDrop={onDrop}
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

                      {isDescendantSearch && hasNextPage && (
                        <div className="flex justify-center p-4">
                          <Button
                            variant="outline"
                            disabled={isFetchingNextPage}
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
                    <p className="text-center text-lg">{searchErrorMessage}</p>
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
                  effectiveId === "share" && rawContents.length === 0 ? (
                    <Center className="flex-col">
                      <div>
                        <Share2Icon size="80" />
                      </div>
                      <p className="mt-4 text-center text-lg">
                        {t("page.share_drive.no_accessible_drives")}
                      </p>
                    </Center>
                  ) : (
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
                  )
                ) : query.isFetching && displayContents.length === 0 ? (
                  <AkashaSkeleton />
                ) : null}
              </HandlerProvider>
            </ContextMenuProvider>
            <DriveUploadDropOverlay
              visible={uploadDragging}
              folderName={query.data.content?.name ?? t("page.share_drive.title")}
            />
          </div>
        </div>

        <RenameDialog />
        <DeleteItemsDialog />
        <ConflictNameDialog />
        <UnsupportedExtensionsDialog />
        <NewDirectoryDialog contents={rawContents} />
        {/* <PubLinkDialog /> */}
      </>
    );
  } else {
    return <></>;
  }
}
