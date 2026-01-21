import { createFileRoute, useLocation } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { useViewStore, viewStore } from "@renderer/store/drive";
import { useEffect, useMemo } from "react";
import { commonSort } from "@renderer/lib/utils";
import { disassemble, getChoseong } from "es-hangul";
import { getSearchScore } from "@renderer/lib/sejong";
import { Center, ServerCrash } from "@renderer/components/common";
import { AliceLoader } from "@renderer/components/loaders";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import {
  AkashaBreadcrumb,
  AkashaHeadButtons,
  AkashaSkeleton,
  ContentMenuGrid,
  ContentMenuList,
  ContextMenuProvider,
  HandlerProvider,
} from "@renderer/components/akasha";
import { FolderIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  NewDirectoryDialog,
  PubLinkDialog,
  RenameDialog,
} from "@renderer/components/akasha/dialogs";
import { useDrag } from "@renderer/hooks/drive";
import { Titlebar } from "@renderer/components/titlebar";
import { orderBy } from "es-toolkit";

export const Route = createFileRoute("/drive/share/$id")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
  const { id } = Route.useParams();
  const location = useLocation();

  const { onDragEnter, onDragLeave, onDragOver, onDrop } = useDrag();
  const searchInDirQuery = useViewStore((s) => s.searchInDirQuery);
  const setSearchInDirQuery = useViewStore((s) => s.setSearchInDirQuery);
  const sortType = useViewStore((s) => s.sortType);
  const layout = useViewStore((s) => s.layout);

  const query = useQuery({
    queryKey: ["drive", "share", id],
    enabled: !!id,
    placeholderData: (prev) => prev,
    refetchIntervalInBackground: true,
    refetchInterval: (query) => {
      if (typeof document !== "undefined" && document.hidden) {
        return 60000 * 3; // 3분 (백그라운드)
      }
      return 30000; // 30초 (포그라운드)
    },
    queryFn: async () => {
      const data = await window.api.invoke("drive:get:item", id);
      return data;
    },
  });

  useEffect(() => {
    if (searchInDirQuery) {
      setSearchInDirQuery("");
    }
  }, [id]);

  useEffect(() => {
    if (id) {
      const setLastShareId = viewStore.getState().setLastShareId;
      setLastShareId(id);
    }
  }, [id]);

  const rawContents = useMemo(() => {
    if (!query.data?.children) return [];
    return commonSort([...query.data?.children], sortType);
  }, [query.data?.children, sortType]);

  const sortedContents = useMemo(() => {
    if (!rawContents) return [];
    if (!searchInDirQuery) return rawContents;

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
  }, [rawContents, searchInDirQuery]);

  if (!query.data && query.isFetching) {
    return (
      <>
        <Titlebar title={{ text: "공유 드라이브", position: "center" }} />
        <Center>
          <AliceLoader />
        </Center>
      </>
    );
  } else if (query.isError) {
    return (
      <>
        <Titlebar title={{ text: "공유 드라이브", position: "center" }} />
        <Center>
          <ServerCrash />
        </Center>
      </>
    );
  }

  if (query.data) {
    return (
      <>
        <Titlebar title={{ text: "공유 드라이브", position: "center" }} />

        <div className="w-full h-full flex flex-col select-none">
          <div className="w-full h-12 flex flex-row items-center p-2 border-b select-none">
            {location.pathname !== "/drive/share" ? (
              <AkashaBreadcrumb itemId={id} ancestors={query.data.ancestors} />
            ) : (
              <div className="flex-1"></div>
            )}

            <AkashaHeadButtons content={query.data.content!} />
          </div>

          <div
            className="flex flex-col flex-1 overflow-auto"
            onDragEnter={onDragEnter}
            onDragLeave={onDragLeave}
            onDragOver={onDragOver}
            onDrop={(e) => onDrop(e, id)}
          >
            <ContextMenuProvider>
              <HandlerProvider queryData={query} sortedContents={sortedContents}>
                {sortedContents.length > 0 ? (
                  <ScrollArea className="flex-1 flex flex-col h-full">
                    <>
                      {layout === "list" ? (
                        <ContentMenuList
                          sortedContents={sortedContents}
                          isFetching={query.isFetching}
                          itemId={id}
                        />
                      ) : layout === "grid" ? (
                        <ContentMenuGrid
                          sortedContents={sortedContents}
                          isFetching={query.isFetching}
                          itemId={id}
                        />
                      ) : null}
                    </>
                  </ScrollArea>
                ) : query.isFetched && sortedContents.length < 1 ? (
                  <Center className="flex-col">
                    <div>
                      <FolderIcon size="80" />
                    </div>
                    <p className="text-lg text-center mt-4">
                      {t("drive.ui.no_contents_section_message.0")}
                    </p>
                    <p className="text-muted-foreground text-center">
                      {t("drive.ui.no_contents_section_message.1")}
                    </p>
                  </Center>
                ) : query.isFetching && sortedContents.length === 0 ? (
                  <AkashaSkeleton />
                ) : null}
              </HandlerProvider>
            </ContextMenuProvider>
          </div>
        </div>

        <RenameDialog />
        <NewDirectoryDialog contents={sortedContents} />
        <PubLinkDialog />
      </>
    );
  } else {
    return <></>;
  }
}
