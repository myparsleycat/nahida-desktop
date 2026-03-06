import { Button, buttonVariants } from "@renderer/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
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
import i18n from "@renderer/lib/i18n";
import { cn } from "@renderer/lib/utils";
import {
  useContentMenu,
  useDialogStore,
  useSelectionStore,
  useViewStore,
} from "@renderer/store/drive";
import type { Content } from "@shared/types.gen";
import { formatDate, formatSize, getRandInt } from "@shared/utils";
import { useMutation } from "@tanstack/react-query";
import { useLocation, useNavigate, useParams, useRouteContext } from "@tanstack/react-router";
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
  MousePointer2Icon,
  SearchIcon,
  SquarePenIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react";
import type React from "react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import validator from "validator";
import { PreviewModal } from "./preview-modal";

export interface Ancestor {
  id: string;
  parentId: string | null;
  name: string;
  depth: number;
}

interface AkashaBreadcrumbProps {
  itemId: string;
  ancestors: Ancestor[];
}

export function AkashaBreadcrumb(props: AkashaBreadcrumbProps) {
  const { itemId, ancestors } = props;
  const navi = useNavigate();
  const { t } = useTranslation();
  const location = useLocation();

  const breadcrumbItems = useMemo(() => {
    const isSharePath = location.pathname.startsWith("/drive/share");

    const rootItem = isSharePath
      ? { id: "share", name: t("page.drive.share_drive"), parentId: null }
      : { id: "root", name: t("page.drive.title"), parentId: null };

    return [rootItem, ...ancestors];
  }, [ancestors, t, location.pathname]);

  const current = useMemo(() => {
    if (breadcrumbItems.length === 0) return undefined;
    return breadcrumbItems[breadcrumbItems.length - 1];
  }, [breadcrumbItems]);

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
            onClick={() => {
              const isSharePath = location.pathname.startsWith("/drive/share");
              const parentId = current?.parentId
                ? current.parentId
                : isSharePath
                  ? "share"
                  : "root";

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

function ContextMenuContentSnippet() {
  const { selectedItems, setSelectedItems } = useSelectionStore();
  const dialog = useDialogStore();
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const location = useLocation();
  const isSharePath = location.pathname.startsWith("/drive/share");
  const { id: itemId } = useParams({
    from: isSharePath ? "/drive/share/$id" : "/drive/drive/$id",
  });
  const navi = useNavigate();

  const trashMutation = useMutation({
    mutationKey: ["akasha", "drive", "trash"],
    mutationFn: async ({ items }: { items: Content[] }) => {
      const ids = items.map((item) => item.id);
      await window.api.invoke("drive:delete:items", ids, "trash");
      return;
    },
  });

  const handleTrashBtn = async (e: React.MouseEvent) => {
    return trashMutation
      .mutateAsync({ items: selectedItems })
      .then(async () => {
        toast.success(`${selectedItems.length}개의 파일 및 디렉토리가 휴지통으로 이동되었습니다`);
        setSelectedItems([]);
        await queryClient.invalidateQueries();
      })
      .catch((err: string) => {
        toast.error(err);
      });
  };

  return selectedItems && selectedItems.length !== 0 ? (
    <>
      {selectedItems.length === 1 && (
        <>
          {selectedItems[0].isDir && (
            <>
              <ContextMenuItem
                className="gap-x-2"
                onClick={() => {
                  navi({
                    to: location.pathname.startsWith("/drive/share")
                      ? "/drive/share/$id"
                      : "/drive/drive/$id",
                    params: { id: selectedItems[0].id },
                  });
                }}
              >
                <MousePointer2Icon size={18} />
                {t("page.drive.context_menu.open")}
              </ContextMenuItem>
              <ContextMenuSeparator />
            </>
          )}

          {/* {selectedItems[0].mimeType?.startsWith("text") ||
            (selectedItems[0].mimeType?.startsWith("image") && (
              <ContextMenuItem
                className="cugap-x-2"
                onClick={() => {
                  // TODO: 미리보기
                }}
              >
                <EyeIcon size={18} />
                {t("drive.ui.context_menu.preview")}
              </ContextMenuItem>
            ))} */}

          <ContextMenuItem
            className="gap-x-2"
            onClick={() =>
              window.api.invoke("drive:fn:startDownload", {
                id: selectedItems[0].id,
                suggestedName: selectedItems[0].name,
              })
            }
          >
            <DownloadIcon size={18} />
            {t("page.drive.context_menu.download")}
          </ContextMenuItem>
          <ContextMenuSeparator />

          {/* <ContextMenuItem
            className="gap-x-2"
            onClick={() => {
              if (selectedItems[0]) {
                dialog.setOpen("shareDialog", true, {
                  id: selectedItems[0].id,
                });
              } else {
                toast.warning("선택된 항목이 없습니다");
              }
            }}
          >
            <Share2Icon size={18} />
            {t("drive.ui.context_menu.share")}
          </ContextMenuItem>

          <ContextMenuItem
            className="gap-x-2"
            onClick={() =>
              dialog.setOpen("notiDialog", true, {
                id: selectedItems[0].id,
              })
            }
          >
            <BellIcon />
            알림
          </ContextMenuItem>

          <ContextMenuSeparator /> */}

          <ContextMenuItem
            className="gap-x-2"
            onClick={() =>
              dialog.setOpen("renameDialog", true, {
                id: selectedItems[0].id,
              })
            }
          >
            <SquarePenIcon size={18} />
            {t("page.drive.context_menu.rename")}
          </ContextMenuItem>

          <ContextMenuSeparator />

          {/* <ContextMenuItem
            className="gap-x-2"
            onClick={() => window.api.invoke("util:copyStr", selectedItems[0].id)}
          >
            <CopyIcon size={18} />
            {t("drive.ui.context_menu.copy_id")}
          </ContextMenuItem>

          <ContextMenuSeparator /> */}
        </>
      )}

      {isSharePath ? (
        <ContextMenuItem className="gap-x-2 text-red-500" onClick={handleTrashBtn}>
          <Trash2Icon size={18} />
          {t("page.drive.context_menu.trash")}
        </ContextMenuItem>
      ) : (
        <>
          <ContextMenuItem
            className="gap-x-2 text-red-500"
            onClick={() => dialog.setOpen("deleteItemsDialog", true)}
          >
            <Trash2Icon size={18} />
            {t("page.drive.context_menu.delete")}
          </ContextMenuItem>
        </>
      )}
    </>
  ) : (
    <ContextMenuItem
      className="cursor-pointer gap-x-2"
      onClick={() => dialog.setOpen("createDirDialog", true)}
    >
      <FolderIcon size={18} />
      {t("page.drive.context_menu.new_dir")}
    </ContextMenuItem>
  );
}

interface ContextMenuProviderProps {
  children: React.ReactNode;
}

export function ContextMenuProvider(props: ContextMenuProviderProps) {
  const { children } = props;
  const { selection } = useContentMenu();

  const handleEmptyAreaClick = (e: React.MouseEvent) => {
    const target = e.target as HTMLElement;
    if (!target.closest(".sorted-contents")) {
      selection.setSelectedItems([]);
      selection.setLastSelectedIdx(null);
    }
  };

  return (
    <ContextMenu>
      <ContextMenuTrigger
        className="overflow-y-auto overflow-x-hidden flex-1"
        onClick={handleEmptyAreaClick}
        onContextMenu={handleEmptyAreaClick}
      >
        {children}
      </ContextMenuTrigger>

      <ContextMenuContent className="flex-1">
        <ContextMenuContentSnippet />
      </ContextMenuContent>
    </ContextMenu>
  );
}

interface ContentMenuProps {
  sortedContents: Content[];
  isFetching: boolean;
  itemId: string;
}

function ListHead() {
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
        <th className="pl-3 font-normal text-left align-middle w-full">
          <button
            className="flex flex-row items-center w-full justify-start"
            onClick={() => handleSort("NAME")}
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
            onClick={() => handleSort("SIZE")}
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
            onClick={() => handleSort("DATE")}
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
  const { queryClient } = useRouteContext({ from: "__root__" });
  const { children, sortedContents, queryData, currentId } = props;
  const navi = useNavigate();
  const dialog = useDialogStore();
  const { selectedItems, setSelectedItems, setLastSelectedIdx, copyOrCuts, setCopyOrCuts } =
    useSelectionStore();
  const isfocusSearchInput = useViewStore((s) => s.isfocusSearchInput);

  const searchBuffer = useRef("");
  const searchTimeout = useRef<number | undefined>(undefined);

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

      if (isfocusSearchInput) return;
      if (dialog.anyDialogOpen()) return;

      const currentIndex = selectedItems.length
        ? sortedContents.findIndex((item) => item.id === selectedItems[0]?.id)
        : -1;

      if (e.key === "F2") {
        e.preventDefault();

        if (!selectedItems || selectedItems.length === 0 || selectedItems.length > 1) return;

        dialog.setOpen("renameDialog", true);
        return;
      }

      if (e.key === "ArrowDown") {
        e.preventDefault();

        if (e.ctrlKey || e.metaKey) {
          if (currentIndex !== -1 && sortedContents[currentIndex]?.isDir) {
            navi({
              to: "/drive/drive/$id",
              params: { id: sortedContents[currentIndex].id },
            });
          }
        } else {
          const nextIndex = Math.min(currentIndex + 1, sortedContents.length - 1);
          setSelectedItems([sortedContents[nextIndex]]);
          setLastSelectedIdx(nextIndex);

          const element = document.getElementById(sortedContents[nextIndex]?.id);
          if (element) {
            element.scrollIntoView({ behavior: "smooth", block: "center" });
          }
        }
      }

      if (e.key === "ArrowUp") {
        e.preventDefault();

        if (e.ctrlKey || e.metaKey) {
          if (queryData.data?.parent) {
            const isUUID = validator.isUUID(queryData.data.parent.id);
            navi({
              to: "/drive/drive/$id",
              params: {
                id: isUUID ? "root" : queryData.data.parent.id,
              },
            });
          } else {
            toast.warning("상위 폴더가 없습니다.");
          }
        } else {
          const prevIndex = Math.max(currentIndex - 1, 0);
          setSelectedItems([sortedContents[prevIndex]]);
          setLastSelectedIdx(prevIndex);

          const element = document.getElementById(sortedContents[prevIndex]?.id);
          if (element) {
            element.scrollIntoView({ behavior: "smooth", block: "center" });
          }
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

          const element = document.querySelector(`[data-uuid="${firstMatchedItem.id}"]`);
          if (element) {
            element.scrollIntoView({ behavior: "smooth", block: "center" });
          }
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
        setCopyOrCuts("cut", selectedItems);
        if (selectedItems.length === 1) {
          toast.info(`"${selectedItems[0].name}"이(가) 잘라내기 상태로 설정되었습니다`);
        } else if (selectedItems.length > 1) {
          toast.info(
            `"${copyOrCuts.items[0].name}"외 ${
              copyOrCuts.items.length - 1
            }개가 잘라내기 상태로 설정되었습니다.`,
          );
        }
      }

      if ((e.ctrlKey || e.metaKey) && e.key === "v") {
        e.preventDefault();
        if (copyOrCuts.action && copyOrCuts.items.length > 0) {
          if (copyOrCuts.action === "cut") {
            const itemsToMove = [...copyOrCuts.items];

            setCopyOrCuts(null, []);

            const promise = window.api.invoke("drive:fn:moveMany", {
              ids: itemsToMove.map((item) => item.id),
              destId: currentId,
            });

            toast.promise(promise, {
              loading: "File moving...",
              success: () => {
                queryClient.invalidateQueries({
                  queryKey: ["drive", "drive", currentId],
                });
                queryClient.invalidateQueries({
                  queryKey: ["drive", "share", currentId],
                });
                return "File moved successfully";
              },
              error: (err: any) => `File moving failed: ${err.message}`,
            });
          }
        }
      }
    },
    [
      sortedContents,
      selectedItems,
      isfocusSearchInput,
      copyOrCuts,
      queryData.data,
      navi,
      dialog,
      setSelectedItems,
      setLastSelectedIdx,
      setCopyOrCuts,
    ],
  );

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
