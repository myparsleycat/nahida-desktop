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
import { Skeleton } from "@renderer/components/ui/skeleton";
import { useAuth } from "@renderer/hooks/use-auth";
import { useDriveClipboardActions } from "@renderer/hooks/use-drive-clipboard";
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
import { PreviewModal } from "./preview-modal";

export interface Ancestor {
  id: string;
  parentId?: string | null;
  name: string;
  depth: number;
}

interface AkashaBreadcrumbProps {
  itemId: string;
  ancestors: Ancestor[];
}

export function AkashaBreadcrumb(props: AkashaBreadcrumbProps) {
  const { itemId, ancestors } = props;
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
      { id: session?.drive.rootId!, name: t("page.drive.title"), parentId: null },
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
    <div className="flex-1 flex flex-row items-center h-full min-w-0 overflow-hidden mr-2">
      {(location.pathname.startsWith("/drive/drive") ||
        current?.parentId ||
        (location.pathname.startsWith("/drive/share") && !current?.parentId)) &&
        breadcrumbItems.length > 1 && (
          <Button
            size="icon"
            variant="ghost"
            className="shrink-0 mr-1"
            onClick={(e) => {
              e.currentTarget.blur();

              const isSharePath = location.pathname.startsWith("/drive/share");
              const parentId = current?.parentId
                ? current.parentId
                : isSharePath
                  ? "share"
                  : session?.drive.rootId!;

              queueRevealForDestination(parentId);

              navi({
                to: isSharePath ? "/drive/share/$id" : "/drive/drive/$id",
                params: { id: parentId },
              });
            }}
          >
            <ChevronLeftIcon size={20} />
          </Button>
        )}

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            className="min-w-0 max-w-fit flex flex-row items-center px-2 overflow-hidden"
            onClick={(e) => {
              e.currentTarget.blur();
            }}
          >
            <FolderIcon className="mr-2 h-4 w-4 shrink-0" />
            <span className="truncate text-left min-w-0">{current?.name}</span>
            <ChevronDownIcon className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="max-w-[400px]"
          onCloseAutoFocus={(e) => e.preventDefault()}
        >
          {[...breadcrumbItems].reverse().map((ancestor) => (
            <DropdownMenuItem
              key={ancestor.id}
              onClick={() => {
                queueRevealForDestination(ancestor.id);
                navi({
                  to: location.pathname.startsWith("/drive/share")
                    ? "/drive/share/$id"
                    : "/drive/drive/$id",
                  params: { id: ancestor.id },
                });
              }}
              className="flex justify-between items-center"
            >
              <span className="truncate flex-1 mr-4">{ancestor.name}</span>
              {ancestor.id === current?.id && <CheckIcon className="h-4 w-4 shrink-0" />}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

export function AkashaHeadButtons() {
  const { t } = useTranslation();
  const dialog = useDialogStore();

  const layout = useViewStore((s) => s.layout);
  const setLayout = useViewStore((s) => s.setLayout);
  const searchInDirQuery = useViewStore((s) => s.searchInDirQuery);
  const setSearchInDirQuery = useViewStore((s) => s.setSearchInDirQuery);
  const setFocusSearchInputState = useViewStore((s) => s.setFocusSearchInputState);

  const handleDownload = () => {};

  return (
    <div className="shrink-0 flex flex-row justify-end items-center gap-2">
      <div className="relative flex items-center shrink-0">
        <SearchIcon className="size-4 absolute left-2 text-muted-foreground" />
        <Input
          id="drive-search-input"
          className="pl-7 w-50 h-9 dark:bg-transparent"
          placeholder={t("page.drive.head_buttons.search_in_dir_placeholder")}
          value={searchInDirQuery}
          onChange={(e) => setSearchInDirQuery(e.target.value)}
          onFocus={() => setFocusSearchInputState(true)}
          onBlur={() => setFocusSearchInputState(false)}
        />
      </div>

      <div className="flex flex-row items-center gap-1 shrink-0">
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
          <DropdownMenuContent onCloseAutoFocus={(e) => e.preventDefault()}>
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
          <DropdownMenuContent onCloseAutoFocus={(e) => e.preventDefault()}>
            <DropdownMenuItem
              className="gap-3 cursor-pointer"
              onClick={() => dialog.setOpen("createDirDialog", true)}
            >
              <FolderIcon size={20} />
              {t("page.drive.head_buttons.dropdown_menu.make_new.new_dir")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem className="gap-3 cursor-pointer">
                <UploadIcon size={20} />
                {t("page.drive.head_buttons.dropdown_menu.make_new.upload_dir")}
              </DropdownMenuItem>
              <DropdownMenuItem className="gap-3 cursor-pointer">
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

function ListHead() {
  const sortType = useViewStore((s) => s.sortType);
  const setSortType = useViewStore((s) => s.setSortType);
  const { t } = useTranslation();

  const handleSortButtonClick = (
    e: React.MouseEvent<HTMLButtonElement>,
    field: "NAME" | "SIZE" | "DATE",
  ) => {
    handleSort(field);
    e.currentTarget.blur();
  };

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
        <th className="pl-3 font-normal text-left align-middle w-full">
          <button
            className="flex flex-row items-center w-full justify-start"
            onClick={(e) => handleSortButtonClick(e, "NAME")}
          >
            <div
              className={cn(
                "flex flex-row gap-2 items-center",
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

        <th className="px-2 font-normal align-middle whitespace-nowrap w-[1%]">
          <button
            className="flex flex-row items-center w-full justify-end"
            onClick={(e) => handleSortButtonClick(e, "SIZE")}
          >
            <div
              className={cn(
                "flex flex-row gap-2 items-center justify-end",
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

        <th className="pr-3 font-normal align-middle whitespace-nowrap w-[1%]">
          <button
            className="flex flex-row items-center w-full justify-end"
            onClick={(e) => handleSortButtonClick(e, "DATE")}
          >
            <div
              className={cn(
                "flex flex-row gap-2 items-center justify-end",
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

  return (
    <>
      <table className="w-full border-collapse table-auto">
        <ListHead />
        <tbody>
          {sortedContents.map((item, idx) => (
            <tr
              key={item.id}
              data-uuid={item.id}
              className={cn(
                "sorted-contents hover:bg-black/10 hover:dark:bg-white/10 cursor-pointer border-b border-transparent",
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
              <td className="p-2 pl-3 align-middle text-left  w-full max-w-0">
                <div className="flex flex-row items-center gap-3">
                  <div className="size-11 flex items-center justify-center text-muted-foreground shrink-0">
                    {isFetching && itemId === item.id ? (
                      <LoaderIcon className="animate-spin" size="20" />
                    ) : item.isDir && !item.preview ? (
                      <FolderIcon className="text-yellow-400 w-full h-full" />
                    ) : item.preview ? (
                      <PreviewModal
                        className="w-12"
                        preview={item.preview}
                        alt={item.name}
                        type="list"
                      />
                    ) : item.mimeType?.startsWith("text") ? (
                      <FileTextIcon className="text-blue-400 w-full h-full" />
                    ) : (
                      <FileIcon className="w-full h-full" />
                    )}
                  </div>
                  <span className="truncate block w-full text-left">{item.name}</span>
                </div>
              </td>

              <td className="p-2 align-middle text-sm text-muted-foreground whitespace-nowrap text-right w-[1%]">
                {formatSize(Number(item.size))}
              </td>
              <td className="p-2 pr-3 align-middle text-sm text-muted-foreground whitespace-nowrap text-right w-[1%]">
                {formatDate(item.updatedAt, i18n.language)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <div
        className="grow min-h-full"
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
    <div className="p-4 grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-4 pr-4">
      {sortedContents.map((item, idx) => (
        <div
          key={item.id}
          data-uuid={item.id}
          className={cn(
            "sorted-contents border rounded-sm p-2 hover:bg-secondary cursor-pointer",
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
          <div className="relative flex justify-center items-center aspect-square">
            {isFetching && id === item.id ? (
              <LoaderIcon className="animate-spin" size="32" />
            ) : item.isDir && !item.preview ? (
              <FolderIcon className="text-yellow-400 p-4" size="100" />
            ) : item.preview?.video ? (
              <video
                src={item.preview.video.default}
                className="relative object-contain w-full h-full"
                draggable="false"
                muted
                autoPlay
                loop
                controls={false}
              />
            ) : item.preview?.img ? (
              <img
                className="relative object-contain w-full h-full"
                src={item.preview.img.cover || item.preview.img.default}
                alt={item.name}
                loading="lazy"
              />
            ) : (
              <FileIcon className="text-blue-400" size="32" />
            )}

            <div className="absolute flex flex-row justify-center items-center bottom-0 left-1/2 -translate-x-1/2 w-full">
              <div className="flex flex-row h-full items-center rounded-full px-2 py-0.75 justify-center gap-2 bg-zinc-100 dark:bg-zinc-900">
                <p className="dragselect-start-disallowed line-clamp-1 text-ellipsis break-all text-sm text-primary">
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

interface HandlerProviderProps {
  queryData: any;
  children: React.ReactNode;
  sortedContents: Content[];
  currentId: string;
}

export function HandlerProvider(props: HandlerProviderProps) {
  const { children, sortedContents, queryData, currentId } = props;
  const navi = useNavigate();
  const location = useLocation();
  const dialog = useDialogStore();
  const { selectedItems, setSelectedItems, setLastSelectedIdx, setCopyOrCuts } =
    useSelectionStore();
  const isfocusSearchInput = useViewStore((s) => s.isfocusSearchInput);
  const setSearchInDirQuery = useViewStore((s) => s.setSearchInDirQuery);
  const pendingDriveRevealId = useViewStore((s) => s.pendingDriveRevealId);
  const setPendingDriveRevealId = useViewStore((s) => s.setPendingDriveRevealId);
  const pendingShareRevealId = useViewStore((s) => s.pendingShareRevealId);
  const setPendingShareRevealId = useViewStore((s) => s.setPendingShareRevealId);
  const { handleCut, handlePaste } = useDriveClipboardActions(currentId);

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

      const currentIndex = selectedItems.length
        ? sortedContents.findIndex((item) => item.id === selectedItems[0]?.id)
        : -1;

      if (e.key === "Enter") {
        e.preventDefault();

        if (selectedItems.length !== 1) return;
        const currentItem = currentIndex !== -1 ? sortedContents[currentIndex] : undefined;
        if (!currentItem) return;

        if (currentItem.isDir) {
          navi({
            to: location.pathname.startsWith("/drive/share")
              ? "/drive/share/$id"
              : "/drive/drive/$id",
            params: { id: currentItem.id },
          });
          return;
        }

        await window.api.invoke("drive:fn:startDownload", {
          id: currentItem.id,
          suggestedName: currentItem.name,
        });
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
            navi({
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
          if (queryData.data?.parent) {
            const isSharePath = location.pathname.startsWith("/drive/share");
            const parentId =
              isSharePath && queryData.data.parent.parentId === null
                ? "share"
                : queryData.data.parent.id;

            if (isSharePath) {
              setPendingShareRevealId(currentId);
            } else {
              setPendingDriveRevealId(currentId);
            }

            navi({
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
        if (queryData.data?.children) {
          setSelectedItems(queryData.data.children);
        }
      }

      if ((e.ctrlKey || e.metaKey) && e.key === "c") {
        e.preventDefault();
        if (selectedItems.length >= 1) {
          toast.warning("복사는 지원하지 않습니다");
        }
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
      queryData.data,
      navi,
      dialog,
      setSelectedItems,
      setLastSelectedIdx,
      setCopyOrCuts,
      currentId,
      handleCut,
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
      <div key={idx} className={cn("flex flex-row items-center px-3 py-2 gap-4")}>
        <div className="flex flex-row items-center gap-2">
          <div className="size-12 flex text-muted-foreground p-0.5">
            <div className="w-full h-full flex items-center justify-center">
              <Skeleton className="size-full rounded-lg" />
            </div>
          </div>
        </div>

        <div className="flex flex-row items-center gap-2 w-full min-w-0">
          <div className="grow min-w-0">
            <Skeleton className="h-5" style={{ width: getRandInt(80, 250) }} />
          </div>
        </div>

        <div className="flex flex-row items-center gap-2">
          <div className="min-w-0 text-sm text-muted-foreground">
            <Skeleton className="h-5" style={{ width: getRandInt(45, 65) }} />
          </div>
        </div>

        <div className="text-right text-sm text-muted-foreground text-nowrap">
          <Skeleton className="h-5" style={{ width: getRandInt(145, 155) }} />
        </div>
      </div>
    ));
  }

  return <></>;
}
