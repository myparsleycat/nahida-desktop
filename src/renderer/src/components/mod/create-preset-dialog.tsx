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
import { Plus } from "lucide-react";
import { toast } from "sonner";
import { useModStore } from "@renderer/store/mod";

interface CreatePresetDialogProps {
  onCreatePreset: () => void;
  disabled?: boolean;
}

export function CreatePresetDialog({ onCreatePreset, disabled = false }: CreatePresetDialogProps) {
  const isOpen = useModStore((s) => s.isPresetDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsPresetDialogOpen);
  const newPresetName = useModStore((s) => s.newPresetName);
  const setNewPresetName = useModStore((s) => s.setNewPresetName);

  const handleCreate = () => {
    if (!newPresetName.trim()) {
      toast.error("프리셋 이름을 입력해주세요.");
      return;
    }
    onCreatePreset();
  };

  return (
    <Dialog open={isOpen} onOpenChange={setIsOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="icon" disabled={disabled}>
          <Plus className="size-4" />
        </Button>
      </DialogTrigger>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>새 프리셋 생성</DialogTitle>
        </DialogHeader>
        <div>
          <Input
            placeholder="프리셋 이름"
            value={newPresetName}
            onChange={(e) => setNewPresetName(e.target.value)}
          />
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">취소</Button>
          </DialogClose>
          <Button onClick={handleCreate}>프리셋 생성</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
