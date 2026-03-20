import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { useGameMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import { useTranslation } from "react-i18next";

export function DeleteGameDialog() {
  const { t } = useTranslation();
  const isOpen = useModStore((s) => s.isDeleteGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsDeleteGameDialogOpen);
  const deletingGame = useModStore((s) => s.deletingGame);
  const setDeletingGame = useModStore((s) => s.setDeletingGame);

  const { deleteGameMutation } = useGameMutations();

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open) {
      setDeletingGame(null);
    }
  };

  return (
    <AlertDialog open={isOpen} onOpenChange={handleOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("page.mod.dialog.delete-game.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("page.mod.dialog.delete-game.description", { name: deletingGame ?? "" })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={() => {
              if (deletingGame) {
                deleteGameMutation.mutate(deletingGame);
              }
            }}
          >
            {t("g.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
