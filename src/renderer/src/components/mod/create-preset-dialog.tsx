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
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Textarea } from "@renderer/components/ui/textarea";
import { usePresetMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import type { PresetCreateConflict } from "@shared/types.gen";
import { LoaderIcon, Plus } from "lucide-react";
import { type MouseEvent, useState } from "react";
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
  const newPresetDescription = useModStore((s) => s.newPresetDescription);
  const setNewPresetDescription = useModStore((s) => s.setNewPresetDescription);
  const [conflicts, setConflicts] = useState<PresetCreateConflict[]>([]);
  const [isConflictDialogOpen, setIsConflictDialogOpen] = useState(false);
  const [isResolvingConflicts, setIsResolvingConflicts] = useState(false);

  const { createPresetMutation, getPresetCreateConflicts } = usePresetMutations();

  const getErrorMessage = (error: unknown) => (error instanceof Error ? error.message : "");
  const isPresetConflictError = (error: unknown) => {
    const errorMessage = getErrorMessage(error);
    return (
      errorMessage.includes("PRESET_CONFLICTS_EXIST") ||
      errorMessage.includes("PRESET_CONFLICT_RESOLUTION_FAILED")
    );
  };

  const handleCreate = async () => {
    try {
      if (!newPresetName.trim()) {
        toast.warning(t("page.mod.dialog.add-preset.#.0"));
        return;
      }

      const createConflicts = await getPresetCreateConflicts();
      if (createConflicts.length > 0) {
        setConflicts(createConflicts);
        setIsConflictDialogOpen(true);
        return;
      }

      await createPresetMutation.mutateAsync({});
    } catch (error) {
      if (!getErrorMessage(error).includes("PRESET_CONFLICTS_EXIST")) {
        throw error;
      }

      const createConflicts = await getPresetCreateConflicts();
      setConflicts(createConflicts);
      setIsConflictDialogOpen(true);
    }
  };

  const handleResolveAndCreate = async (event: MouseEvent<HTMLButtonElement>) => {
    try {
      event.preventDefault();
      setIsResolvingConflicts(true);
      await createPresetMutation.mutateAsync({ resolveConflicts: true });
      setIsConflictDialogOpen(false);
      setConflicts([]);
    } catch (error) {
      if (!isPresetConflictError(error)) {
        throw error;
      }

      const createConflicts = await getPresetCreateConflicts();
      setConflicts(createConflicts);
    } finally {
      setIsResolvingConflicts(false);
    }
  };

  const handleOpenChange = (open: boolean) => {
    setIsOpen(open);
    if (!open && !createPresetMutation.isPending) {
      setNewPresetName("");
      setNewPresetDescription("");
      setConflicts([]);
      setIsConflictDialogOpen(false);
    }
  };

  const isBusy = createPresetMutation.isPending || isResolvingConflicts;

  return (
    <>
      <Dialog open={isOpen} onOpenChange={handleOpenChange}>
        <DialogTrigger asChild>
          <Button variant="outline" size="icon" disabled={disabled}>
            <Plus className="size-4" />
          </Button>
        </DialogTrigger>
        <DialogContent className="w-100" aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>{t("page.mod.dialog.add-preset.title")}</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <Input
              placeholder={t("g.name")}
              value={newPresetName}
              onChange={(e) => setNewPresetName(e.target.value)}
            />
            <Textarea
              placeholder={t("page.mod.dialog.add-preset.description-placeholder")}
              value={newPresetDescription}
              onChange={(e) => setNewPresetDescription(e.target.value)}
            />
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">{t("g.cancel")}</Button>
            </DialogClose>
            <Button onClick={() => void handleCreate()} disabled={isBusy}>
              {isBusy && <LoaderIcon className="animate-spin size-4" />}
              {t("g.create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={isConflictDialogOpen} onOpenChange={setIsConflictDialogOpen}>
        <AlertDialogContent className="max-w-lg">
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.add-preset.conflict.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.add-preset.conflict.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <ScrollArea>
            <div className="max-h-64 space-y-3 text-sm">
              {conflicts.map((conflict) => (
                <div key={conflict.modKey} className="rounded-md border p-3">
                  <div className="font-medium">
                    {t("page.mod.dialog.add-preset.conflict.mod-key-label")}
                  </div>
                  <div className="text-muted-foreground break-all">{conflict.modKey}</div>
                  <div className="mt-2 space-y-1">
                    {conflict.candidates.map((candidate) => (
                      <div
                        key={candidate.actualPath}
                        className="flex items-start justify-between gap-3"
                      >
                        <span className="break-all">{candidate.relativePath}</span>
                        <span className="text-muted-foreground shrink-0">
                          {candidate.isEnabled
                            ? t("page.mod.dialog.add-preset.conflict.enabled")
                            : t("page.mod.dialog.add-preset.conflict.disabled")}
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </ScrollArea>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isBusy}>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => void handleResolveAndCreate(event)}
              disabled={isBusy}
            >
              {isBusy && <LoaderIcon className="animate-spin size-4" />}
              {t("page.mod.dialog.add-preset.conflict.action")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
