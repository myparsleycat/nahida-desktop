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

export function DeleteGameDialog() {
  const isOpen = useModStore((s) => s.isDeleteGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsDeleteGameDialogOpen);
  const selectedGame = useModStore((s) => s.selectedGame);

  const { deleteGameMutation } = useGameMutations();

  return (
    <AlertDialog open={isOpen} onOpenChange={setIsOpen}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>게임 삭제</AlertDialogTitle>
          <AlertDialogDescription>
            정말로 "{selectedGame}" 게임을 삭제하시겠습니까? 이 작업은 되돌릴 수 없으며, 해당 게임의
            프리셋 정보도 함께 삭제됩니다.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>취소</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            onClick={() => {
              if (selectedGame) {
                deleteGameMutation.mutate(selectedGame);
              }
            }}
          >
            삭제
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
