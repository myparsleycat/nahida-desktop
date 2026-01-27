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
import { LoaderIcon, Plus } from "lucide-react";
import { toast } from "sonner";
import { useModStore } from "@renderer/store/mod";
import { usePresetMutations } from "@renderer/hooks/use-mod-mutations";

interface CreatePresetDialogProps {
  disabled?: boolean;
}

export function CreatePresetDialog({ disabled = false }: CreatePresetDialogProps) {
  const isOpen = useModStore((s) => s.isPresetDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsPresetDialogOpen);
  const newPresetName = useModStore((s) => s.newPresetName);
  const setNewPresetName = useModStore((s) => s.setNewPresetName);

  const { createPresetMutation } = usePresetMutations();

  const handleCreate = () => {
    if (!newPresetName.trim()) {
      toast.error("프리셋 이름을 입력해주세요.");
      return;
    }
    createPresetMutation.mutate();
  };

  return (
    <Dialog open={isOpen} onOpenChange={setIsOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="icon" disabled={disabled}>
          <Plus className="size-4" />
        </Button>
      </DialogTrigger>
      <DialogContent className="w-100" aria-describedby={undefined}>
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
          <Button onClick={handleCreate} disabled={createPresetMutation.isPending}>
            {createPresetMutation.isPending && <LoaderIcon className="animate-spin size-4" />}
            프리셋 생성
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
