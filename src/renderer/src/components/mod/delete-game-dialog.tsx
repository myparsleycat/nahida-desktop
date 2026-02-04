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
import { useModStore } from "@renderer/store/mod";
import { useGameMutations } from "@renderer/hooks/use-mod-mutations";
import { useTranslation } from "react-i18next";

export function DeleteGameDialog() {
  const { t } = useTranslation();
  const isOpen = useModStore((s) => s.isDeleteGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsDeleteGameDialogOpen);
  const selectedGame = useModStore((s) => s.selectedGame);

  const { deleteGameMutation } = useGameMutations();

  return (
    <AlertDialog open={isOpen} onOpenChange={setIsOpen}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("page.mod.dialog.delete-game.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("page.mod.dialog.delete-game.description", { name: selectedGame })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            onClick={() => {
              if (selectedGame) {
                deleteGameMutation.mutate(selectedGame);
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
