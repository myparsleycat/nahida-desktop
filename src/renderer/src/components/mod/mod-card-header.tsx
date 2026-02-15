import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@renderer/components/ui/alert-dialog";
import { Button } from "@renderer/components/ui/button";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import { FolderIcon, TerminalSquareIcon, TrashIcon } from "lucide-react";
import { memo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface ModCardHeaderProps {
  mod: ModInfo;
  selectedGroupPath?: string;
}

export const ModCardHeader = memo(function ModCardHeader({
  mod,
  selectedGroupPath,
}: ModCardHeaderProps) {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });

  const handleDelete = (e: React.MouseEvent) => {
    e.stopPropagation();
    const promise = window.api.invoke("util:fs:trash", mod.path);
    toast.promise(promise, {
      loading: "휴지통으로 이동 중...",
      success: "삭제 완료",
      error: "삭제 실패",
    });
    promise.then(() => {
      queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
    });
  };

  return (
    <div className="flex items-center justify-between pb-1 relative z-10">
      <span className="text-sm truncate font-semibold">
        {mod.name.replace(/disabled/gi, "").trim()}
      </span>
      <div className="flex items-center gap-1">
        <Button
          variant="ghost"
          size="icon"
          className="size-7 hover:bg-accent/20"
          onClick={(e) => {
            e.stopPropagation();
            window.api.invoke("util:openCmd", mod.path);
          }}
        >
          <TerminalSquareIcon />
        </Button>

        <AlertDialog>
          <AlertDialogTrigger asChild onClick={(e) => e.stopPropagation()}>
            <Button variant="ghost" size="icon" className="size-7 hover:bg-accent/20">
              <TrashIcon />
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent onClick={(e) => e.stopPropagation()}>
            <AlertDialogHeader>
              <AlertDialogTitle>{t("page.mod.dialog.delete-mod.title")}</AlertDialogTitle>
              <AlertDialogDescription>
                {t("page.mod.dialog.delete-mod.description")}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
              <AlertDialogAction onClick={handleDelete}>{t("g.delete")}</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        <Button
          variant="ghost"
          size="icon"
          className="size-7 hover:bg-accent/20"
          onClick={(e) => {
            e.stopPropagation();
            window.api.invoke("util:openPath", mod.path);
          }}
        >
          <FolderIcon />
        </Button>
      </div>
    </div>
  );
});
