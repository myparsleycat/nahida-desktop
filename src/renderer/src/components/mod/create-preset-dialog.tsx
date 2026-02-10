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
import { usePresetMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import { LoaderIcon, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface CreatePresetDialogProps {
  disabled?: boolean;
}

export function CreatePresetDialog({ disabled = false }: CreatePresetDialogProps) {
  const { t } = useTranslation();
  const isOpen = useModStore((s) => s.isPresetDialogOpen);
  const setIsOpen = useModStore((s) => s.setIsPresetDialogOpen);
  const newPresetName = useModStore((s) => s.newPresetName);
  const setNewPresetName = useModStore((s) => s.setNewPresetName);

  const { createPresetMutation } = usePresetMutations();

  const handleCreate = () => {
    if (!newPresetName.trim()) {
      toast.warning(t("page.mod.dialog.add-preset.#.0"));
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
          <DialogTitle>{t("page.mod.dialog.add-preset.title")}</DialogTitle>
        </DialogHeader>
        <div>
          <Input
            placeholder={t("g.name")}
            value={newPresetName}
            onChange={(e) => setNewPresetName(e.target.value)}
          />
        </div>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">{t("g.cancel")}</Button>
          </DialogClose>
          <Button onClick={handleCreate} disabled={createPresetMutation.isPending}>
            {createPresetMutation.isPending && <LoaderIcon className="animate-spin size-4" />}
            {t("g.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
