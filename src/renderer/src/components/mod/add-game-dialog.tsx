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
import { useModStore } from "@renderer/store/mod";

interface AddGameDialogProps {
  onBrowseFolder: () => void;
  onAddGame: (name: string, path: string) => void;
}

export function AddGameDialog({ onBrowseFolder, onAddGame }: AddGameDialogProps) {
  const isOpen = useModStore((s) => s.isAddGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsAddGameDialogOpen);
  const newGameName = useModStore((s) => s.newGameName);
  const setNewGameName = useModStore((s) => s.setNewGameName);
  const newGamePath = useModStore((s) => s.newGamePath);

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
    <Dialog open={isOpen} onOpenChange={setIsOpen}>
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
              onChange={(e) => setNewGameName(e.target.value)}
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
