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
import { useTranslation } from "react-i18next";

interface AddGameDialogProps {
  onBrowseFolder: () => void;
  onAddGame: (name: string, path: string) => void;
}

export function AddGameDialog({ onBrowseFolder, onAddGame }: AddGameDialogProps) {
  const { t } = useTranslation();

  const isOpen = useModStore((s) => s.isAddGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsAddGameDialogOpen);
  const newGameName = useModStore((s) => s.newGameName);
  const setNewGameName = useModStore((s) => s.setNewGameName);
  const newGamePath = useModStore((s) => s.newGamePath);

  const handleAdd = () => {
    if (!newGameName.trim()) {
      toast.error(t("page.mod.dialog.add-game.#.0"));
      return;
    }
    if (!newGamePath.trim()) {
      toast.error(t("page.mod.dialog.add-game.#.1"));
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
          <DialogTitle>{t("page.mod.dialog.add-game.title")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Input
              placeholder={t("page.mod.dialog.add-game.name_input_placeholder")}
              value={newGameName}
              onChange={(e) => setNewGameName(e.target.value)}
            />
          </div>
          <div className="flex gap-2">
            <Input
              placeholder={t("page.mod.dialog.add-game.path_input_placeholder")}
              value={newGamePath}
              readOnly
            />
            <Button variant="outline" size="icon" onClick={onBrowseFolder}>
              <FolderOpen className="size-4" />
            </Button>
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">{t("g.cancel")}</Button>
          </DialogClose>
          <Button onClick={handleAdd}>{t("g.add")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
