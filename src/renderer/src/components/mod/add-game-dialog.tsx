import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Plus, FolderOpen } from "lucide-react";
import { toast } from "sonner";

interface AddGameDialogProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  newGameName: string;
  newGamePath: string;
  onGameNameChange: (name: string) => void;
  onGamePathChange: (path: string) => void;
  onBrowseFolder: () => void;
  onAddGame: (name: string, path: string) => void;
}

export function AddGameDialog({
  isOpen,
  onOpenChange,
  newGameName,
  newGamePath,
  onGameNameChange,
  onGamePathChange,
  onBrowseFolder,
  onAddGame,
}: AddGameDialogProps) {
  const handleAdd = () => {
    if (!newGameName.trim()) {
      toast.error("게임 이름을 입력해주세요.");
      return;
    }
    if (!newGamePath.trim()) {
      toast.error("모드 폴더 경로를 선택해주세요.");
      return;
    }
    onAddGame(newGameName, newGamePath);
  };

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogTrigger asChild>
        <Button variant="outline" size="icon">
          <Plus className="size-4" />
        </Button>
      </DialogTrigger>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>게임 추가</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Input
              placeholder="게임 이름 (예: 원공노)"
              value={newGameName}
              onChange={(e) => onGameNameChange(e.target.value)}
            />
          </div>
          <div className="flex gap-2">
            <Input placeholder="모드 폴더 경로" value={newGamePath} readOnly />
            <Button variant="outline" size="icon" onClick={onBrowseFolder}>
              <FolderOpen className="size-4" />
            </Button>
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">취소</Button>
          </DialogClose>
          <Button onClick={handleAdd}>추가</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
