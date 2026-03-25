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
import { useLocation, useNavigate, useRouteContext } from "@tanstack/react-router";
import {
  DownloadIcon,
  FolderIcon,
  MousePointer2Icon,
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
  const { selectedItems, setSelectedItems } = useSelectionStore();
  const dialog = useDialogStore();
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const location = useLocation();
  const isSharePath = location.pathname.startsWith("/drive/share");
  //   const { id: itemId } = useParams({
  //     from: isSharePath ? "/drive/share/$id" : "/drive/drive/$id",
  //   });
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
        <ContextMenuItem className="gap-x-2" variant="destructive" onClick={handleTrashBtn}>
          <Trash2Icon size={18} />
          {t("page.drive.context_menu.trash")}
        </ContextMenuItem>
      ) : (
        <>
          <ContextMenuItem
            className="gap-x-2"
            variant="destructive"
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
