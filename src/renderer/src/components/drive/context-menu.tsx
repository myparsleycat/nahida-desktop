import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
import { useContentMenu, useDialogStore, useSelectionStore } from "@renderer/store/drive";
import { Content } from "@shared/types.gen";
import { useMutation } from "@tanstack/react-query";
import { useLocation, useNavigate, useParams, useRouteContext } from "@tanstack/react-router";
import {
  ClipboardPaste,
  DownloadIcon,
  FolderIcon,
  MousePointer2Icon,
  ScissorsIcon,
  SquarePenIcon,
  Trash2Icon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

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

function ContextMenuContentSnippet() {
  const { selectedItems, setSelectedItems, copyOrCuts, setCopyOrCuts } = useSelectionStore();
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

  const handleTrashBtn = async (_e: React.MouseEvent) => {
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

  const handleCut = () => {
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
  };

  const handelPaste = () => {
    if (copyOrCuts.action && copyOrCuts.items.length > 0) {
      if (copyOrCuts.action === "cut") {
        const itemsToMove = [...copyOrCuts.items];

        setCopyOrCuts(null, []);

        const promise = window.api.invoke("drive:fn:moveMany", {
          ids: itemsToMove.map((item) => item.id),
          destId: itemId,
        });

        toast.promise(promise, {
          loading: "File moving...",
          success: () => {
            queryClient.invalidateQueries({
              queryKey: ["drive", "drive", itemId],
            });
            queryClient.invalidateQueries({
              queryKey: ["drive", "share", itemId],
            });
            return "File moved successfully";
          },
          error: (err) => `File moving failed: ${err.message}`,
        });
      }
    }
  };

  const deleteMenuItem = isSharePath ? (
    <ContextMenuItem className="gap-x-2" variant="destructive" onClick={handleTrashBtn}>
      <Trash2Icon size={18} />
      {t("page.drive.context_menu.trash")}
    </ContextMenuItem>
  ) : (
    <ContextMenuItem
      className="gap-x-2"
      variant="destructive"
      onClick={() => dialog.setOpen("deleteItemsDialog", true)}
    >
      <Trash2Icon size={18} />
      {t("page.drive.context_menu.delete")}
    </ContextMenuItem>
  );

  if (!selectedItems || selectedItems.length === 0) {
    return (
      <>
        <ContextMenuItem
          className="gap-x-2"
          onClick={() => dialog.setOpen("createDirDialog", true)}
        >
          <FolderIcon size={18} />
          {t("page.drive.context_menu.new_dir")}
        </ContextMenuItem>

        {copyOrCuts.items.length > 0 && (
          <>
            <ContextMenuSeparator />

            <ContextMenuItem onClick={handelPaste}>
              <ClipboardPaste size={18} />
              {t("page.drive.context_menu.paste")}
            </ContextMenuItem>
          </>
        )}
      </>
    );
  }

  if (selectedItems.length === 1) {
    return (
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

        <ContextMenuItem className="gpa-x-2" onClick={handleCut}>
          <ScissorsIcon size={18} />
          {t("page.drive.context_menu.cut")}
        </ContextMenuItem>

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

        {deleteMenuItem}
      </>
    );
  }

  return (
    <>
      <ContextMenuItem className="cursor-pointer gap-x-2" onClick={handleCut}>
        <ScissorsIcon size={18} />
        {t("page.drive.context_menu.cut")}
      </ContextMenuItem>

      <ContextMenuSeparator />

      {deleteMenuItem}
    </>
  );
}
