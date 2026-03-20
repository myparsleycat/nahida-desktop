import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { Label } from "@renderer/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { useModStore } from "@renderer/store/mod";
import type { GameConfig } from "@shared/types.gen";
import { FolderOpen, Trash2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const NO_IMPORTER_VALUE = "__none__";

interface EditGameDialogProps {
  enabledImporters: Array<{ key: string }>;
  onPickFolder: () => Promise<string | null>;
  onUpdateGame: (game: string, updates: { modFolderPath: string; importer: string | null }) => void;
  onDeleteGameClick: (game: string) => void;
}

export function openEditGameDialog(
  game: GameConfig,
  setters: {
    setEditingGame: (game: GameConfig) => void;
    setEditGamePath: (path: string) => void;
    setEditGameImporter: (importer: string | null) => void;
    setIsEditGameDialogOpen: (open: boolean) => void;
  },
) {
  setters.setEditingGame(game);
  setters.setEditGamePath(game.modFolderPath);
  setters.setEditGameImporter(game.importer);
  setters.setIsEditGameDialogOpen(true);
}

export function EditGameDialog({
  enabledImporters,
  onPickFolder,
  onUpdateGame,
  onDeleteGameClick,
}: EditGameDialogProps) {
  const { t } = useTranslation();
  const isOpen = useModStore((s) => s.isEditGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsEditGameDialogOpen);
  const editingGame = useModStore((s) => s.editingGame);
  const setEditingGame = useModStore((s) => s.setEditingGame);
  const editGamePath = useModStore((s) => s.editGamePath);
  const setEditGamePath = useModStore((s) => s.setEditGamePath);
  const editGameImporter = useModStore((s) => s.editGameImporter);
  const setEditGameImporter = useModStore((s) => s.setEditGameImporter);

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open) {
      setEditingGame(null);
      setEditGamePath("");
      setEditGameImporter(null);
    }
  };

  const handlePickFolder = async () => {
    const path = await onPickFolder();
    if (path) {
      setEditGamePath(path);
    }
  };

  const handleSave = () => {
    if (!editingGame) {
      return;
    }

    if (!editGamePath.trim()) {
      toast.error(t("page.mod.dialog.add-game.#.1"));
      return;
    }

    onUpdateGame(editingGame.game, {
      modFolderPath: editGamePath,
      importer: editGameImporter,
    });
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
      <DialogContent className="w-100">
        <DialogHeader>
          <DialogTitle>{editingGame?.game}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
            <Label>{t("page.mod.dialog.edit-game.importer_label")}</Label>
            <Select
              value={editGameImporter ?? NO_IMPORTER_VALUE}
              onValueChange={(value) =>
                setEditGameImporter(value === NO_IMPORTER_VALUE ? null : value)
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder={t("g.select")} />
              </SelectTrigger>
              <SelectContent aria-describedby={undefined} position="popper">
                <SelectItem value={NO_IMPORTER_VALUE}>
                  {t("page.mod.dialog.edit-game.no_importer")}
                </SelectItem>
                {enabledImporters.map((importer) => (
                  <SelectItem key={importer.key} value={importer.key}>
                    {importer.key}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label>{t("page.mod.dialog.edit-game.path_label")}</Label>
            <div className="flex gap-2">
              <Input
                placeholder={t("page.mod.dialog.add-game.path_input_placeholder")}
                value={editGamePath}
                readOnly
              />
              <Button variant="outline" size="icon" onClick={handlePickFolder}>
                <FolderOpen className="size-4" />
              </Button>
            </div>
          </div>
          <div className="flex justify-end items-center">
            <Button
              variant="outline"
              disabled={!editingGame}
              onClick={() => {
                if (editingGame) {
                  onDeleteGameClick(editingGame.game);
                }
              }}
            >
              <Trash2Icon className="size-4 text-destructive" />
              {t("page.mod.dialog.delete-game.title")}
            </Button>
          </div>
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">{t("g.cancel")}</Button>
          </DialogClose>
          <Button onClick={handleSave}>{t("g.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
