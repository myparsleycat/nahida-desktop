import { Alert, AlertDescription } from "@renderer/components/ui/alert";
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
import { Label } from "@renderer/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { useModStore } from "@renderer/store/mod";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { FolderOpen, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

const NO_IMPORTER_VALUE = "__none__";

interface AddGameDialogProps {
  onPickFolder: () => Promise<string | null>;
  onAddGame: (name: string, path: string, importer: string | null) => void;
}

export function AddGameDialog({ onPickFolder, onAddGame }: AddGameDialogProps) {
  const { t } = useTranslation();
  const navi = useNavigate();

  const isOpen = useModStore((s) => s.isAddGameDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsAddGameDialogOpen);
  const newGameName = useModStore((s) => s.newGameName);
  const setNewGameName = useModStore((s) => s.setNewGameName);
  const newGamePath = useModStore((s) => s.newGamePath);
  const setNewGamePath = useModStore((s) => s.setNewGamePath);
  const newGameImporter = useModStore((s) => s.newGameImporter);
  const setNewGameImporter = useModStore((s) => s.setNewGameImporter);
  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => window.api.invoke("xxmi:getXXMIData"),
  });

  const enabledImporters = xxmiData?.enabledImporters ?? [];
  const isXXMIConfigured = !!xxmiData?.xxmiPath;

  const handleAdd = () => {
    if (!newGameName.trim()) {
      toast.warning(t("page.mod.dialog.add-game.#.0"));
      return;
    }
    if (!newGamePath.trim()) {
      toast.warning(t("page.mod.dialog.add-game.#.1"));
      return;
    }
    onAddGame(newGameName, newGamePath, newGameImporter);
  };

  const handlePickFolder = async () => {
    const path = await onPickFolder();
    if (path) {
      setNewGamePath(path);
    }
  };

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open) {
      setNewGameImporter(null);
    }
  };

  const handleOpenXXMISettings = () => {
    handleOpenChange(false);
    navi({ to: "/setting/xxmi" });
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleOpenChange}>
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
            <Label htmlFor="name">{t("page.mod.dialog.add-game.name_input_placeholder")}</Label>
            <div className="space-y-2">
              <Input
                id="name"
                value={newGameName}
                onChange={(e) => setNewGameName(e.target.value)}
              />
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="path">{t("page.mod.dialog.add-game.path_input_placeholder")}</Label>
            <div className="flex gap-2">
              <Input id="path" value={newGamePath} readOnly hideFocusRing />
              <Button variant="outline" size="icon" onClick={handlePickFolder}>
                <FolderOpen className="size-4" />
              </Button>
            </div>
          </div>

          <div className="space-y-2">
            <Label>{t("page.mod.dialog.edit-game.importer_label")}</Label>
            <Select
              value={newGameImporter ?? NO_IMPORTER_VALUE}
              onValueChange={(value) =>
                setNewGameImporter(value === NO_IMPORTER_VALUE ? null : value)
              }
            >
              <SelectTrigger className="w-full">
                <SelectValue placeholder={t("g.select")} />
              </SelectTrigger>
              <SelectContent aria-describedby={undefined} position="popper">
                <SelectGroup>
                  <SelectItem value={NO_IMPORTER_VALUE}>
                    {t("page.mod.dialog.edit-game.no_importer")}
                  </SelectItem>
                  {enabledImporters.map((importer) => (
                    <SelectItem key={importer.key} value={importer.key}>
                      {importer.key}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {!isXXMIConfigured && (
              <Alert>
                <AlertDescription>
                  <div className="flex flex-col gap-3">
                    <span>{t("page.mod.dialog.add-game.xxmi_path_required")}</span>
                    <Button variant="outline" className="w-fit" onClick={handleOpenXXMISettings}>
                      {t("page.mod.dialog.add-game.open_xxmi_settings")}
                    </Button>
                  </div>
                </AlertDescription>
              </Alert>
            )}
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
