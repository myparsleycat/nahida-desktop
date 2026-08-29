import { Button, buttonVariants } from "@renderer/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Input } from "@renderer/components/ui/input";
import { PreviewLightbox } from "@renderer/components/ui/preview-lightbox";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { useAuth } from "@renderer/hooks/use-auth";
import { useDriveClipboardActions } from "@renderer/hooks/use-drive-clipboard";
import { downloadItems } from "@renderer/lib/download";
import i18n from "@renderer/lib/i18n";
import { cn } from "@renderer/lib/utils";
import {
  useContentMenu,
  useDialogStore,
  useSelectionStore,
  useViewStore,
} from "@renderer/store/drive";
import type { Content } from "@shared/types";
import { formatDate, formatSize, getRandInt } from "@shared/utils";
import type { UseQueryResult } from "@tanstack/react-query";
import { useLocation, useNavigate, useParams } from "@tanstack/react-router";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  DownloadIcon,
  FileIcon,
  FileTextIcon,
  FolderIcon,
  FolderTree,
  LinkIcon,
  LayoutGridIcon,
  ListIcon,
  LoaderIcon,
  SearchIcon,
  UploadIcon,
} from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export interface Ancestor {
  id: string;
  parentId?: string | null;
  name: string;
  depth: number;
}

interface AkashaBreadcrumbProps {
  itemId: string;
  ancestors?: Ancestor[] | null;
}

export function AkashaBreadcrumb(props: AkashaBreadcrumbProps) {
  const { itemId: _itemId, ancestors: rawAncestors } = props;
  const ancestors = rawAncestors ?? [];
  const { session } = useAuth();
  const navi = useNavigate();
  const { t } = useTranslation();
  const location = useLocation();
  const setPendingDriveRevealId = useViewStore((s) => s.setPendingDriveRevealId);
  const setPendingShareRevealId = useViewStore((s) => s.setPendingShareRevealId);

  const breadcrumbItems = useMemo(() => {
    const isSharePath = location.pathname.startsWith("/drive/share");

    if (isSharePath) {
      return [{ id: "share", name: t("page.drive.share_drive"), parentId: null }, ...ancestors];
    }

    if (ancestors[0]?.parentId === null) {
      return [{ ...ancestors[0], name: t("page.drive.title") }, ...ancestors.slice(1)];
    }

    return [
      { id: session?.drive.rootId ?? "", name: t("page.drive.title"), parentId: null },
      ...ancestors,
    ];
  }, [ancestors, location.pathname, session?.drive.rootId, t]);

  const current = useMemo(() => {
    if (breadcrumbItems.length === 0) return undefined;
    return breadcrumbItems[breadcrumbItems.length - 1];
  }, [breadcrumbItems]);

  const queueRevealForDestination = useCallback(
    (destinationId: string) => {
      const destinationIndex = breadcrumbItems.findIndex((item) => item.id === destinationId);
      const childOnCurrentPath =
        destinationIndex >= 0 ? breadcrumbItems[destinationIndex + 1] : undefined;

      if (location.pathname.startsWith("/drive/share")) {
        setPendingShareRevealId(childOnCurrentPath?.id ?? null);
      } else {
        setPendingDriveRevealId(childOnCurrentPath?.id ?? null);
      }
    },
    [breadcrumbItems, location.pathname, setPendingDriveRevealId, setPendingShareRevealId],
  );

  return (
    <div className="mr-2 flex h-full min-w-0 flex-1 flex-row items-center overflow-hidden">
      {(location.pathname.startsWith("/drive/drive") ||
        current?.parentId ||
        (location.pathname.startsWith("/drive/share") && !current?.parentId)) &&
        breadcrumbItems.length > 1 && (
          <Button
            size="icon"
            variant="ghost"
            className="mr-1 shrink-0"
            onClick={(e) => {
              e.currentTarget.blur();

              const isSharePath = location.pathname.startsWith("/drive/share");
              const parentId = current?.parentId
                ? current.parentId
                : isSharePath
                  ? "share"
                  : (session?.drive.rootId ?? "");

              if (!parentId) return;

              queueRevealForDestination(parentId);

              void navi({
                to: isSharePath ? "/drive/share/$id" : "/drive/drive/$id",
                params: { id: parentId },
              });
            }}
          >
            <ChevronLeftIcon size={20} />
          </Button>
        )}

      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              className="flex max-w-fit min-w-0 flex-row items-center overflow-hidden px-2"
              onClick={(e) => {
                e.currentTarget.blur();
              }}
            />
          }
        >
          <FolderIcon className="mr-2 h-4 w-4 shrink-0" />
          <span className="min-w-0 truncate text-left">{current?.name}</span>
          <ChevronDownIcon className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="max-w-[400px]" finalFocus={false}>
          {[...breadcrumbItems].reverse().map((ancestor) => (
            <DropdownMenuItem
              key={ancestor.id}
              onClick={() => {
                queueRevealForDestination(ancestor.id);
                void navi({
                  to: location.pathname.startsWith("/drive/share")
                    ? "/drive/share/$id"
                    : "/drive/drive/$id",
                  params: { id: ancestor.id },
                });
              }}
              className="flex items-center justify-between"
            >
              <span className="mr-4 flex-1 truncate">{ancestor.name}</span>
              {ancestor.id === current?.id && <CheckIcon className="h-4 w-4 shrink-0" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

export function AkashaHeadButtons({ currentId }: { currentId?: string }) {
  const { t } = useTranslation();
  const dialog = useDialogStore();
  const { selectedItems } = useSelectionStore();
  const navi = useNavigate();
  const { session } = useAuth();

  const layout = useViewStore((s) => s.layout);
  const setLayout = useViewStore((s) => s.setLayout);
  const searchInDirQuery = useViewStore((s) => s.searchInDirQuery);
  const setSearchInDirQuery = useViewStore((s) => s.setSearchInDirQuery);
  const includeSubdirs = useViewStore((s) => s.includeSubdirs);
  const setIncludeSubdirs = useViewStore((s) => s.setIncludeSubdirs);
  const setFocusSearchInputState = useViewStore((s) => s.setFocusSearchInputState);
  const setImportOverlay = useViewStore((s) => s.setImportOverlay);

  const canSearchSubdirs = !!currentId && currentId !== "share";
  const searchPlaceholder =
    canSearchSubdirs && includeSubdirs
      ? t("page.drive.head_buttons.search_in_subdirs_placeholder")
      : t("page.drive.head_buttons.search_in_dir_placeholder");

  const handleDownload = () => {
    if (selectedItems.length === 0) return;
    void downloadItems(selectedItems);
  };

  const handleImportClick = async () => {
    if (currentId) {
      setImportOverlay({ url: "" });
      return;
    }
    const rootId = session?.drive.rootId;
    if (!rootId) return;
    await navi({ to: "/drive/drive/$id", params: { id: rootId } });
    setImportOverlay({ url: "" });
  };

  return (
    <div className="flex shrink-0 flex-row items-center justify-end gap-2">
      <div className="relative flex shrink-0 items-center">
        <SearchIcon className="absolute left-2 size-4 text-muted-foreground" />
        <Input
          id="drive-search-input"
          className="h-9 w-50 pl-7 dark:bg-transparent"
          placeholder={searchPlaceholder}
          value={searchInDirQuery}
          onChange={(e) => setSearchInDirQuery(e.target.value)}
          onFocus={() => setFocusSearchInputState(true)}
          onBlur={() => setFocusSearchInputState(false)}
        />
        {canSearchSubdirs && (
          <Button
            variant="ghost"
            size="icon"
            aria-pressed={includeSubdirs}
            aria-label={t("page.drive.head_buttons.search_include_subdirs")}
            title={t("page.drive.head_buttons.search_include_subdirs")}
            className={cn(
              "ml-1 h-9 w-9 shrink-0",
              includeSubdirs && "bg-muted text-primary",
              !includeSubdirs && "text-muted-foreground",
            )}
            onClick={() => setIncludeSubdirs(!includeSubdirs)}
          >
            <FolderTree size={20} />
          </Button>
        )}
      </div>

      <div className="flex shrink-0 flex-row items-center gap-1">
        <Button
          variant="ghost"
          size="icon"
          className="shrink-0"
          aria-label={t("page.drive.head_buttons.import")}
          title={t("page.drive.head_buttons.import")}
          onClick={() => {
            void handleImportClick();
          }}
        >
          <LinkIcon size={20} />
        </Button>

        <Button
          variant="ghost"
          size="icon"
          className="shrink-0"
          onClick={() => {
            if (layout === "grid") {
              setLayout("list");
            } else {
              setLayout("grid");
            }
          }}
        >
          {layout === "grid" ? (
            <ListIcon size={20} />
          ) : layout === "list" ? (
            <LayoutGridIcon size={20} />
          ) : null}
        </Button>

        <DropdownMenu>
          <DropdownMenuTrigger className={buttonVariants({ variant: "ghost", size: "icon" })}>
            <DownloadIcon size={20} />
          </DropdownMenuTrigger>
          <DropdownMenuContent finalFocus={false}>
            <DropdownMenuItem onClick={handleDownload}>
              <DownloadIcon className="mr-2 h-4 w-4" />
              {t("page.drive.head_buttons.dropdown_menu.download")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <DropdownMenu>
          <DropdownMenuTrigger className={buttonVariants({ variant: "ghost" })}>
            {t("page.drive.head_buttons.dropdown_menu.make_new.title")}
          </DropdownMenuTrigger>
          <DropdownMenuContent finalFocus={false}>
            <DropdownMenuItem
              className="cursor-pointer gap-3"
              onClick={() => dialog.setOpen("createDirDialog", true)}
            >
              <FolderIcon size={20} />
              {t("page.drive.head_buttons.dropdown_menu.make_new.new_dir")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem className="cursor-pointer gap-3">
                <UploadIcon size={20} />
                {t("page.drive.head_buttons.dropdown_menu.make_new.upload_dir")}
              </DropdownMenuItem>
              <DropdownMenuItem className="cursor-pointer gap-3">
                <UploadIcon size={20} />
                {t("page.drive.head_buttons.dropdown_menu.make_new.upload_file")}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}

export function ListHead() {
  const sortType = useViewStore((s) => s.sortType);
  const setSortType = useViewStore((s) => s.setSortType);
  const { t } = useTranslation();

  const handleSort = (field: "NAME" | "SIZE" | "DATE") => {
    if (!sortType.startsWith(field)) {
      const defaultDirection = field === "NAME" ? "ASC" : "DESC";
      setSortType(`${field}:${defaultDirection}`);
    } else {
      const currentDirection = sortType.split(":")[1];
      const nextDirection = currentDirection === "DESC" ? "ASC" : "DESC";
      setSortType(`${field}:${nextDirection}`);
    }
  };

  return (
    <thead className="sticky top-0 bg-background text-sm">
      <tr className="h-8">
        <th className="w-full pl-3 text-left align-middle font-normal">
          <button
            className="flex w-full flex-row items-center justify-start"
            onClick={() => handleSort("NAME")}
          >
            <div
              className={cn(
                "flex flex-row items-center gap-2",
                sortType.startsWith("NAME") ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="dragselect-start-disallowed whitespace-nowrap">
                {t("page.drive.list_head.name")}
              </p>
              {sortType === "NAME:DESC" && <ArrowDownIcon size="16" />}
              {sortType === "NAME:ASC" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </th>

        <th className="w-[1%] px-2 align-middle font-normal whitespace-nowrap">
          <button
            className="flex w-full flex-row items-center justify-end"
            onClick={() => handleSort("SIZE")}
          >
            <div
              className={cn(
                "flex flex-row items-center justify-end gap-2",
                sortType.startsWith("SIZE") ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="dragselect-start-disallowed whitespace-nowrap">
                {t("page.drive.list_head.size")}
              </p>
              {sortType === "SIZE:DESC" && <ArrowDownIcon size="16" />}
              {sortType === "SIZE:ASC" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </th>

        <th className="w-[1%] pr-3 align-middle font-normal whitespace-nowrap">
          <button
            className="flex w-full flex-row items-center justify-end"
            onClick={() => handleSort("DATE")}
          >
            <div
              className={cn(
                "flex flex-row items-center justify-end gap-2",
                sortType.startsWith("DATE") ? "text-primary" : "text-muted-foreground",
              )}
            >
              <p className="dragselect-start-disallowed whitespace-nowrap">
                {t("page.drive.list_head.date")}
              </p>
              {sortType === "DATE:DESC" && <ArrowDownIcon size="16" />}
              {sortType === "DATE:ASC" && <ArrowUpIcon size="16" />}
            </div>
          </button>
        </th>
      </tr>
    </thead>
  );
}

interface ContentMenuProps {
  sortedContents: Content[];
  isFetching: boolean;
  itemId: string;
}

export function ContentMenuList(props: ContentMenuProps) {
  const { sortedContents, isFetching, itemId } = props;
  const {
    selection,
    currentDragOver,
    handleItemClick,
    handleItemRightClick,
    handleClickOutside,
    getDoubleClickHandler,
  } = useContentMenu(sortedContents);
  const location = useLocation();
  const navi = useNavigate();
  const dialog = useDialogStore();

  return (
    <>
      <table className="w-full table-auto border-collapse">
        <ListHead />
        <tbody>
          {sortedContents.map((item, idx) => (
            <tr
              key={item.id}
              data-uuid={item.id}
              className={cn(
                "sorted-contents cursor-pointer border-b border-transparent hover:bg-black/10 hover:dark:bg-white/10",
                selection.selectedItems.some((selected) => selected.id === item.id) &&
                  "bg-black/10 dark:bg-white/10",
                currentDragOver?.id === item.id && "bg-black/10 dark:bg-white/10",
              )}
              draggable="true"
              onClick={(e) => handleItemClick(item, idx, e)}
              onDoubleClick={getDoubleClickHandler(item, (id) =>
                navi({
                  to: location.pathname.startsWith("/drive/share")
                    ? "/drive/share/$id"
                    : "/drive/drive/$id",
                  params: { id },
                }),
              )}
              onContextMenu={(e) => handleItemRightClick(e, item)}
            >
              <td className="w-full max-w-0 p-2 pl-3 text-left align-middle">
                <div className="flex flex-row items-center gap-3">
                  <div className="flex size-11 shrink-0 items-center justify-center text-muted-foreground">
                    {isFetching && itemId === item.id ? (
                      <LoaderIcon className="animate-spin" size="20" />
                    ) : item.isDir && !item.preview ? (
                      <FolderIcon className="h-full w-full text-yellow-400" />
                    ) : item.preview ? (
                      <PreviewLightbox
                        className="size-12 cursor-zoom-in overflow-hidden rounded-md"
                        thumbnailSrc={
                          item.preview.video?.default ||
                          item.preview.img?.thumbnail ||
                          item.preview.img!.default
                        }
                        fullSrc={item.preview.video?.default || item.preview.img!.default}
                        isVideo={!!item.preview.video?.default}
                        alt={item.name}
                        onOpenChange={(v) => dialog.setOpen("previewDialog", v)}
                      />
                    ) : item.mimeType?.startsWith("text") ? (
                      <FileTextIcon className="h-full w-full text-blue-400" />
                    ) : (
                      <FileIcon className="h-full w-full" />
                    )}
                  </div>
                  <span className="block w-full truncate text-left">{item.name}</span>
                </div>
              </td>

              <td className="w-[1%] p-2 text-right align-middle text-sm whitespace-nowrap text-muted-foreground">
                {formatSize(Number(item.size))}
              </td>
              <td className="w-[1%] p-2 pr-3 text-right align-middle text-sm whitespace-nowrap text-muted-foreground">
                {formatDate(item.updatedAt, i18n.language)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <div
        className="min-h-full grow"
        onClick={handleClickOutside}
        onContextMenu={handleClickOutside}
      ></div>
    </>
  );
}

export function ContentMenuGrid(props: ContentMenuProps) {
  const { sortedContents, isFetching } = props;
  const location = useLocation();
  const { id } = useParams({
    from: location.pathname.startsWith("/drive/share") ? "/drive/share/$id" : "/drive/drive/$id",
  });
  const { selection, handleItemClick, handleItemRightClick, getDoubleClickHandler } =
    useContentMenu(sortedContents);
  const navi = useNavigate();

  return (
    <div className="grid grid-cols-2 gap-4 p-4 pr-4 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
      {sortedContents.map((item, idx) => (
        <div
          key={item.id}
          data-uuid={item.id}
          className={cn(
            "sorted-contents cursor-pointer rounded-sm border p-2 hover:bg-secondary",
            selection.selectedItems.some((selected) => selected.id === item.id) && "bg-secondary",
          )}
          draggable="true"
          onClick={(e) => handleItemClick(item, idx, e)}
          onDoubleClick={getDoubleClickHandler(item, (id) =>
            navi({
              to: location.pathname.startsWith("/drive/share")
                ? "/drive/share/$id"
                : "/drive/drive/$id",
              params: { id },
            }),
          )}
          onContextMenu={(e) => handleItemRightClick(e, item)}
        >
          <div className="relative flex aspect-square items-center justify-center">
            {isFetching && id === item.id ? (
              <LoaderIcon className="animate-spin" size="32" />
            ) : item.isDir && !item.preview ? (
              <FolderIcon className="p-4 text-yellow-400" size="100" />
            ) : item.preview?.video ? (
              <video
                src={item.preview.video.default}
                className="relative h-full w-full object-contain"
                draggable="false"
                muted
                autoPlay
                loop
                controls={false}
              />
            ) : item.preview?.img ? (
              <img
                className="relative h-full w-full object-contain"
                src={item.preview.img.cover || item.preview.img.default}
                alt={item.name}
                loading="lazy"
              />
            ) : (
              <FileIcon className="text-blue-400" size="32" />
            )}

            <div className="absolute bottom-0 left-1/2 flex w-full -translate-x-1/2 flex-row items-center justify-center">
              <div className="flex h-full flex-row items-center justify-center gap-2 rounded-full bg-zinc-100 px-2 py-0.75 dark:bg-zinc-900">
                <p className="dragselect-start-disallowed line-clamp-1 text-sm break-all text-ellipsis text-primary">
                  {item.name}
                </p>
              </div>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

interface HandlerProviderProps<T> {
  queryData: UseQueryResult<T>;
  children: React.ReactNode;
  sortedContents: Content[];
  currentId: string;
}

type DriveContent = { children?: Content[]; parent?: { id: string; parentId: string | null } };

export function HandlerProvider<T>(props: HandlerProviderProps<T>) {
  const { children, sortedContents, queryData, currentId } = props;
  const data = queryData.data as DriveContent | undefined;
  const navi = useNavigate();
  const location = useLocation();
  const dialog = useDialogStore();
  const { selectedItems, setSelectedItems, setLastSelectedIdx, setCopyOrCuts } =
    useSelectionStore();
  const isfocusSearchInput = useViewStore((s) => s.isfocusSearchInput);
  const importOverlay = useViewStore((s) => s.importOverlay);
  const setSearchInDirQuery = useViewStore((s) => s.setSearchInDirQuery);
  const pendingDriveRevealId = useViewStore((s) => s.pendingDriveRevealId);
  const setPendingDriveRevealId = useViewStore((s) => s.setPendingDriveRevealId);
  const pendingShareRevealId = useViewStore((s) => s.pendingShareRevealId);
  const setPendingShareRevealId = useViewStore((s) => s.setPendingShareRevealId);
  const { handleCut, handleCopy, handlePaste } = useDriveClipboardActions(currentId);

  const searchBuffer = useRef("");
  const searchTimeout = useRef<number | undefined>(undefined);

  const scrollItemIntoCenter = useCallback(
    (itemId: string, behavior: ScrollBehavior = "smooth") => {
      const element = document.querySelector<HTMLElement>(`[data-uuid="${itemId}"]`);
      if (element) {
        element.scrollIntoView({ behavior, block: "center" });
      }
    },
    [],
  );

  const resetSearchBuffer = () => {
    searchBuffer.current = "";
    if (searchTimeout.current) {
      clearTimeout(searchTimeout.current);
      searchTimeout.current = undefined;
    }
  };

  const handleKeyDown = useCallback(
    async (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "r") return;

      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "f") {
        e.preventDefault();
        if (!dialog.anyDialogOpen()) {
          const searchInput = document.getElementById(
            "drive-search-input",
          ) as HTMLInputElement | null;
          if (searchInput) {
            searchInput.focus();
            searchInput.select();
          }
        }
        return;
      }

      if (isfocusSearchInput && e.key === "ArrowDown") {
        if (dialog.anyDialogOpen()) return;

        e.preventDefault();
        const searchInput = document.getElementById(
          "drive-search-input",
        ) as HTMLInputElement | null;
        if (searchInput) {
          searchInput.blur();
        }

        if (sortedContents.length > 0) {
          setSelectedItems([sortedContents[0]]);
          setLastSelectedIdx(0);
          scrollItemIntoCenter(sortedContents[0].id);
        }
        return;
      }

      if (isfocusSearchInput && e.key === "Escape") {
        e.preventDefault();
        const searchInput = document.getElementById(
          "drive-search-input",
        ) as HTMLInputElement | null;
        if (searchInput) {
          searchInput.blur();
        }
        setSearchInDirQuery("");
        return;
      }

      if (isfocusSearchInput) return;
      if (dialog.anyDialogOpen()) return;
      if (importOverlay) return;

      const currentIndex = selectedItems.length
        ? sortedContents.findIndex((item) => item.id === selectedItems[0]?.id)
        : -1;

      if (e.key === "Enter") {
        e.preventDefault();

        if (selectedItems.length !== 1) return;
        const currentItem = currentIndex !== -1 ? sortedContents[currentIndex] : undefined;
        if (!currentItem) return;

        if (currentItem.isDir) {
          void navi({
            to: location.pathname.startsWith("/drive/share")
              ? "/drive/share/$id"
              : "/drive/drive/$id",
            params: { id: currentItem.id },
          });
          return;
        }

        await downloadItems([currentItem]);
        return;
      }

      if (e.key === "F2") {
        e.preventDefault();

        if (!selectedItems || selectedItems.length === 0 || selectedItems.length > 1) return;

        dialog.setOpen("renameDialog", true);
        return;
      }

      if (e.key === "ArrowDown") {
        e.preventDefault();

        if (e.ctrlKey || e.metaKey) {
          if (selectedItems.length !== 1) return;
          if (currentIndex !== -1 && sortedContents[currentIndex]?.isDir) {
            void navi({
              to: location.pathname.startsWith("/drive/share")
                ? "/drive/share/$id"
                : "/drive/drive/$id",
              params: { id: sortedContents[currentIndex].id },
            });
          }
        } else {
          const nextIndex = Math.min(currentIndex + 1, sortedContents.length - 1);
          setSelectedItems([sortedContents[nextIndex]]);
          setLastSelectedIdx(nextIndex);
          scrollItemIntoCenter(sortedContents[nextIndex].id);
        }
      }

      if (e.key === "ArrowUp") {
        e.preventDefault();

        if (e.ctrlKey || e.metaKey) {
          if (data?.parent) {
            const isSharePath = location.pathname.startsWith("/drive/share");
            const parentId =
              isSharePath && data.parent.parentId === null ? "share" : data.parent.id;

            if (isSharePath) {
              setPendingShareRevealId(currentId);
            } else {
              setPendingDriveRevealId(currentId);
            }

            void navi({
              to: isSharePath ? "/drive/share/$id" : "/drive/drive/$id",
              params: {
                id: parentId,
              },
            });
          } else {
            toast.warning("상위 폴더가 없습니다.");
          }
        } else {
          const prevIndex = Math.max(currentIndex - 1, 0);
          setSelectedItems([sortedContents[prevIndex]]);
          setLastSelectedIdx(prevIndex);
          scrollItemIntoCenter(sortedContents[prevIndex].id);
        }
      }

      if (/^[a-zA-Z0-9]$/.test(e.key) && !(e.ctrlKey || e.metaKey)) {
        e.preventDefault();

        const pressedKey = e.key.toLowerCase();

        searchBuffer.current += pressedKey;

        if (searchTimeout.current) {
          clearTimeout(searchTimeout.current);
        }
        searchTimeout.current = window.setTimeout(() => {
          resetSearchBuffer();
        }, 500);

        const firstMatchedItem = sortedContents.find((item) =>
          item.name.toLowerCase().startsWith(searchBuffer.current),
        );

        if (firstMatchedItem) {
          setSelectedItems([firstMatchedItem]);
          setLastSelectedIdx(sortedContents.indexOf(firstMatchedItem));
          scrollItemIntoCenter(firstMatchedItem.id);
        }
      }

      if (e.key === "Escape") {
        e.preventDefault();

        setSelectedItems([]);
        setLastSelectedIdx(null);
        setCopyOrCuts(null, []);
      }

      if ((e.ctrlKey || e.metaKey) && e.key === "a") {
        e.preventDefault();
        if (data?.children) {
          setSelectedItems(data.children);
        }
      }

      if ((e.ctrlKey || e.metaKey) && e.key === "c") {
        e.preventDefault();
        handleCopy();
      }

      if ((e.ctrlKey || e.metaKey) && e.key === "x") {
        e.preventDefault();
        handleCut();
      }

      if ((e.ctrlKey || e.metaKey) && e.key === "v") {
        e.preventDefault();
        handlePaste();
      }
    },
    [
      sortedContents,
      selectedItems,
      isfocusSearchInput,
      importOverlay,
      data,
      navi,
      dialog,
      setSelectedItems,
      setLastSelectedIdx,
      setCopyOrCuts,
      currentId,
      handleCut,
      handleCopy,
      handlePaste,
      location.pathname,
      scrollItemIntoCenter,
      setPendingDriveRevealId,
      setPendingShareRevealId,
    ],
  );

  useEffect(() => {
    const pendingRevealId = location.pathname.startsWith("/drive/share")
      ? pendingShareRevealId
      : pendingDriveRevealId;

    if (!pendingRevealId || sortedContents.length === 0) return;

    const matchedIndex = sortedContents.findIndex((item) => item.id === pendingRevealId);
    if (matchedIndex < 0) return;

    setSelectedItems([sortedContents[matchedIndex]]);
    setLastSelectedIdx(matchedIndex);

    requestAnimationFrame(() => {
      scrollItemIntoCenter(pendingRevealId, "auto");
    });

    if (location.pathname.startsWith("/drive/share")) {
      setPendingShareRevealId(null);
    } else {
      setPendingDriveRevealId(null);
    }
  }, [
    location.pathname,
    pendingDriveRevealId,
    pendingShareRevealId,
    scrollItemIntoCenter,
    setLastSelectedIdx,
    setPendingDriveRevealId,
    setPendingShareRevealId,
    setSelectedItems,
    sortedContents,
  ]);

  useEffect(() => {
    window.addEventListener("keydown", handleKeyDown);

    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [handleKeyDown]);

  return <>{children}</>;
}

export function AkashaSkeleton() {
  const layout = useViewStore((s) => s.layout);

  if (layout === "list") {
    return Array.from({ length: getRandInt(3, 12) }, (_, idx) => (
      <div key={idx} className={cn("flex flex-row items-center gap-4 px-3 py-2")}>
        <div className="flex flex-row items-center gap-2">
          <div className="flex size-12 p-0.5 text-muted-foreground">
            <div className="flex h-full w-full items-center justify-center">
              <Skeleton className="size-full rounded-lg" />
            </div>
          </div>
        </div>

        <div className="flex w-full min-w-0 flex-row items-center gap-2">
          <div className="min-w-0 grow">
            <Skeleton className="h-5" style={{ width: getRandInt(80, 250) }} />
          </div>
        </div>

        <div className="flex flex-row items-center gap-2">
          <div className="min-w-0 text-sm text-muted-foreground">
            <Skeleton className="h-5" style={{ width: getRandInt(45, 65) }} />
          </div>
        </div>

        <div className="text-right text-sm text-nowrap text-muted-foreground">
          <Skeleton className="h-5" style={{ width: getRandInt(145, 155) }} />
        </div>
      </div>
    ));
  }

  return <></>;
}
