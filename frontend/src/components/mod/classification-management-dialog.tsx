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
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Field, FieldLabel } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useClassifications } from "@renderer/hooks/use-mod-data";
import { useClassificationMutations } from "@renderer/hooks/use-mod-mutations";
import { cn } from "@renderer/lib/utils";
import { useModStore } from "@renderer/store/mod";
import type { Classification } from "@shared/types";
import { ArrowDownIcon, ArrowUpIcon, Loader2Icon, PlusIcon, TrashIcon } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

interface ClassificationDraft {
  id: string | null;
  name: string;
  // `key` stays stable while a group is renamed or moved; `id` is empty until the group is saved.
  groups: { key: string; id: string; name: string }[];
}

function toDraft(classification: Classification | null): ClassificationDraft {
  return {
    id: classification?.id ?? null,
    name: classification?.name ?? "",
    groups: (classification?.groups ?? []).map((group) => ({ ...group, key: group.id })),
  };
}

interface ClassificationManagementDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function ClassificationManagementDialog({
  open,
  onOpenChange,
}: ClassificationManagementDialogProps) {
  const { t } = useTranslation();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t("page.mod.dialog.classification.title")}</DialogTitle>
          <DialogDescription>{t("page.mod.dialog.classification.description")}</DialogDescription>
        </DialogHeader>
        {open && <ClassificationEditor />}
      </DialogContent>
    </Dialog>
  );
}

function ClassificationEditor() {
  const { t } = useTranslation();
  const selectedGame = useModStore((s) => s.selectedGame);
  const { data: classifications = [] } = useClassifications(selectedGame);
  const { saveClassificationMutation, deleteClassificationMutation } = useClassificationMutations();
  const [draft, setDraft] = useState(() =>
    toDraft(classifications.find((item) => item.active) ?? classifications[0] ?? null),
  );
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);
  const isPending = saveClassificationMutation.isPending || deleteClassificationMutation.isPending;
  const canSave =
    draft.name.trim().length > 0 && draft.groups.every((group) => group.name.trim().length > 0);

  const moveGroup = (index: number, direction: -1 | 1) => {
    const groups = [...draft.groups];
    const [moved] = groups.splice(index, 1);
    groups.splice(index + direction, 0, moved);
    setDraft({ ...draft, groups });
  };

  const handleSave = () =>
    saveClassificationMutation.mutate(
      {
        id: draft.id,
        name: draft.name,
        groups: draft.groups.map((group) => ({ id: group.id, name: group.name })),
      },
      {
        onSuccess: (saved) => setDraft(toDraft({ ...saved, groups: saved.groups ?? [] })),
      },
    );

  const handleDelete = () => {
    if (!draft.id) {
      return;
    }

    const deletedId = draft.id;
    deleteClassificationMutation.mutate(deletedId, {
      onSuccess: () =>
        setDraft(toDraft(classifications.find((item) => item.id !== deletedId) ?? null)),
    });
  };

  return (
    <>
      <div className="flex min-h-72 gap-4">
        <div className="flex w-40 shrink-0 flex-col gap-1">
          <ScrollArea className="max-h-72">
            <div className="flex flex-col gap-1">
              {classifications.map((classification) => (
                <Button
                  key={classification.id}
                  type="button"
                  variant="ghost"
                  disabled={isPending}
                  className={cn("justify-start", draft.id === classification.id && "bg-accent")}
                  onClick={() => setDraft(toDraft(classification))}
                >
                  <span className="truncate">{classification.name}</span>
                </Button>
              ))}
            </div>
          </ScrollArea>
          <Button
            type="button"
            variant="outline"
            disabled={isPending}
            className={cn(draft.id === null && "border-primary")}
            onClick={() => setDraft(toDraft(null))}
          >
            <PlusIcon className="size-4" />
            {t("page.mod.dialog.classification.new")}
          </Button>
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <Field>
            <FieldLabel>{t("page.mod.dialog.classification.name-label")}</FieldLabel>
            <Input
              value={draft.name}
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
              placeholder={t("page.mod.dialog.classification.name-placeholder")}
              maxLength={64}
              disabled={isPending}
            />
          </Field>

          <Field>
            <FieldLabel>{t("page.mod.dialog.classification.groups-label")}</FieldLabel>
            <ScrollArea className="max-h-56">
              <div className="flex flex-col gap-2">
                {draft.groups.map((group, index) => (
                  <div key={group.key} className="flex gap-1">
                    <Input
                      value={group.name}
                      onChange={(e) =>
                        setDraft({
                          ...draft,
                          groups: draft.groups.map((item) =>
                            item.key === group.key ? { ...item, name: e.target.value } : item,
                          ),
                        })
                      }
                      placeholder={t("page.mod.dialog.classification.group-placeholder")}
                      maxLength={64}
                      disabled={isPending}
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t("page.mod.dialog.classification.move-up")}
                      disabled={isPending || index === 0}
                      onClick={() => moveGroup(index, -1)}
                    >
                      <ArrowUpIcon className="size-4" />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t("page.mod.dialog.classification.move-down")}
                      disabled={isPending || index === draft.groups.length - 1}
                      onClick={() => moveGroup(index, 1)}
                    >
                      <ArrowDownIcon className="size-4" />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={t("g.delete")}
                      disabled={isPending}
                      onClick={() =>
                        setDraft({
                          ...draft,
                          groups: draft.groups.filter((item) => item.key !== group.key),
                        })
                      }
                    >
                      <TrashIcon className="size-4" />
                    </Button>
                  </div>
                ))}
              </div>
            </ScrollArea>
            <Button
              type="button"
              variant="outline"
              disabled={isPending}
              onClick={() =>
                setDraft({
                  ...draft,
                  groups: [...draft.groups, { key: crypto.randomUUID(), id: "", name: "" }],
                })
              }
            >
              <PlusIcon className="size-4" />
              {t("page.mod.dialog.classification.add-group")}
            </Button>
          </Field>
        </div>
      </div>

      <DialogFooter className="flex justify-between">
        <Button
          type="button"
          variant="destructive"
          disabled={isPending || draft.id === null}
          onClick={() => setIsDeleteConfirmOpen(true)}
        >
          {t("g.delete")}
        </Button>
        <div className="flex gap-2">
          <DialogClose render={<Button type="button" variant="outline" />}>
            {t("g.close")}
          </DialogClose>
          <Button type="button" disabled={isPending || !canSave} onClick={handleSave}>
            {saveClassificationMutation.isPending && (
              <Loader2Icon className="size-4 animate-spin" />
            )}
            {t("g.save")}
          </Button>
        </div>
      </DialogFooter>

      <AlertDialog open={isDeleteConfirmOpen} onOpenChange={setIsDeleteConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.classification.delete-title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.classification.delete-description", { name: draft.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>{t("g.delete")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
