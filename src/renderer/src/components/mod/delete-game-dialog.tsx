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

interface DeleteGameDialogProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  selectedGame: string | null;
  onDeleteGame: (game: string) => void;
}

export function DeleteGameDialog({
  isOpen,
  onOpenChange,
  selectedGame,
  onDeleteGame,
}: DeleteGameDialogProps) {
  return (
    <AlertDialog open={isOpen} onOpenChange={onOpenChange}>
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
                onDeleteGame(selectedGame);
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
