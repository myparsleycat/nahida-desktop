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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import {
  useModTogglePersistPresets,
  useModTogglePersistSnapshot,
} from "@renderer/hooks/use-mod-data";
import { useModTogglePersistPresetMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import type {
  ModTogglePersistPreset,
  ModTogglePersistPresetItem,
  ModTogglePersistState,
} from "@shared/types.gen";
import { useQuery } from "@tanstack/react-query";
import { BookmarkIcon, LoaderIcon, PlusIcon, Trash2Icon } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

function PersistStateList({
  items,
}: {
  items: Array<ModTogglePersistState | ModTogglePersistPresetItem>;
}) {
  return (
    <ScrollArea className="h-72 rounded-md border">
      <div className="space-y-2 p-3">
        {items.map((item, index) => {
          const value = "currentValue" in item ? item.currentValue : item.value;
          const isMissing = "isMissing" in item ? item.isMissing : false;

          return (
            <div
              key={`${item.iniRelativePath}-${item.sectionName}-${item.variable}-${index.toString()}`}
              className="rounded-md border bg-muted/40 p-2 text-sm"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <p className="truncate font-medium">{item.iniName}</p>
                  <p className="text-xs text-muted-foreground">
                    {item.sectionName} / ${item.variable}
                  </p>
                </div>
                <div
                  className={`shrink-0 rounded px-2 py-1 text-xs ${
                    isMissing ? "bg-destructive/10 text-destructive" : "bg-primary/10 text-primary"
                  }`}
                >
                  {isMissing ? "N/A" : value}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </ScrollArea>
  );
}

interface ModTogglePersistPresetMenuProps {
  mod: ModInfo;
}

export function ModTogglePersistPresetMenu({ mod }: ModTogglePersistPresetMenuProps) {
  const { t } = useTranslation();
  const selectedGame = useModStore((s) => s.selectedGame);
  const hasToggleKeys = useMemo(
    () => mod.inis.some((ini) => ini.toggleKeys.length > 0),
    [mod.inis],
  );

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [isManageOpen, setIsManageOpen] = useState(false);
  const [newPresetName, setNewPresetName] = useState("");
  const [selectedPreset, setSelectedPreset] = useState<ModTogglePersistPreset | null>(null);
  const [editingName, setEditingName] = useState("");

  const { data: isPersistEnabled = false, isPending: isPersistSettingPending } = useQuery({
    queryKey: ["setting:xxmi:getPersistToggles"],
    queryFn: () => window.api.invoke("setting:xxmi:getPersistToggles"),
  });

  const { data: presets = [] } = useModTogglePersistPresets(selectedGame, mod.path, hasToggleKeys);
  const snapshotQuery = useModTogglePersistSnapshot(
    selectedGame,
    mod.path,
    hasToggleKeys && (isCreateOpen || isManageOpen),
  );

  const { createPresetMutation, updatePresetMutation, applyPresetMutation, deletePresetMutation } =
    useModTogglePersistPresetMutations(selectedGame, mod.path);

  const savableStates = useMemo(
    () =>
      (snapshotQuery.data ?? []).filter((item) => !item.isMissing && item.currentValue !== null),
    [snapshotQuery.data],
  );
  const missingStateCount = (snapshotQuery.data ?? []).filter((item) => item.isMissing).length;

  const getSnapshotErrorMessage = (error: Error) => error.message;

  useEffect(() => {
    if (!isManageOpen) {
      setSelectedPreset(null);
      setEditingName("");
      return;
    }

    if (selectedPreset) {
      setEditingName(selectedPreset.name);
    }
  }, [isManageOpen, selectedPreset]);

  if (!hasToggleKeys) {
    return null;
  }

  const handleCreateOpenChange = (open: boolean) => {
    setIsCreateOpen(open);
    if (open) {
      snapshotQuery.refetch();
      return;
    }

    if (!createPresetMutation.isPending) {
      setNewPresetName("");
    }
  };

  const handleCreate = () => {
    if (!newPresetName.trim()) {
      toast.warning(t("page.mod.toggle-persist-preset.name-required"));
      return;
    }

    if (!isPersistEnabled) {
      toast.warning(t("page.mod.toggle-persist-preset.persist-required"));
      return;
    }

    createPresetMutation.mutate(newPresetName, {
      onSuccess: () => {
        setIsCreateOpen(false);
        setNewPresetName("");
      },
    });
  };

  const handleManageOpenChange = (open: boolean) => {
    setIsManageOpen(open);
    if (open) {
      snapshotQuery.refetch();
    }
  };

  const handleUpdate = () => {
    if (!selectedPreset) {
      return;
    }

    if (!editingName.trim()) {
      toast.warning(t("page.mod.toggle-persist-preset.name-required"));
      return;
    }

    if (!isPersistEnabled) {
      toast.warning(t("page.mod.toggle-persist-preset.persist-required"));
      return;
    }

    updatePresetMutation.mutate(
      { presetId: selectedPreset.id, name: editingName },
      {
        onSuccess: (preset) => {
          setSelectedPreset(preset);
          setEditingName(preset.name);
        },
      },
    );
  };

  const handleApply = () => {
    if (!selectedPreset) {
      return;
    }

    applyPresetMutation.mutate(selectedPreset.id, {
      onSuccess: () => {
        setIsManageOpen(false);
      },
    });
  };

  const handleDelete = () => {
    if (!selectedPreset) {
      return;
    }

    deletePresetMutation.mutate(selectedPreset.id, {
      onSuccess: () => {
        setIsManageOpen(false);
      },
    });
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="size-7 hover:bg-accent/20"
            aria-label={t("page.mod.toggle-persist-preset.button")}
            onClick={(e) => e.stopPropagation()}
          >
            <BookmarkIcon className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          onClick={(e) => e.stopPropagation()}
          onCloseAutoFocus={(e) => e.preventDefault()}
          className="w-56"
        >
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t("page.mod.toggle-persist-preset.title")}</DropdownMenuLabel>
            <DropdownMenuItem
              disabled={isPersistSettingPending || !isPersistEnabled}
              onSelect={() => handleCreateOpenChange(true)}
            >
              <PlusIcon className="mr-2 size-4" />
              {t("page.mod.toggle-persist-preset.add")}
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            {presets.map((preset) => (
              <DropdownMenuItem
                key={preset.id}
                onSelect={() => {
                  setSelectedPreset(preset);
                  setEditingName(preset.name);
                  setIsManageOpen(true);
                }}
              >
                {preset.name}
              </DropdownMenuItem>
            ))}
            {presets.length === 0 && (
              <DropdownMenuItem disabled>
                {t("page.mod.toggle-persist-preset.empty")}
              </DropdownMenuItem>
            )}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      <Dialog open={isCreateOpen} onOpenChange={handleCreateOpenChange}>
        <DialogContent onClick={(e) => e.stopPropagation()} className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("page.mod.toggle-persist-preset.create-title")}</DialogTitle>
            <DialogDescription>
              {t("page.mod.toggle-persist-preset.create-description")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            {!isPersistEnabled && (
              <div className="rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-300">
                {t("page.mod.toggle-persist-preset.persist-required")}
              </div>
            )}
            <Input
              placeholder={t("page.mod.toggle-persist-preset.name-placeholder")}
              value={newPresetName}
              onChange={(e) => setNewPresetName(e.target.value)}
            />
            {snapshotQuery.isPending ? (
              <div className="flex h-40 items-center justify-center rounded-md border text-sm text-muted-foreground">
                <LoaderIcon className="mr-2 size-4 animate-spin" />
                {t("page.mod.toggle-persist-preset.loading")}
              </div>
            ) : snapshotQuery.isError ? (
              <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
                {getSnapshotErrorMessage(snapshotQuery.error as Error)}
              </div>
            ) : (
              <>
                <PersistStateList items={snapshotQuery.data ?? []} />
                {missingStateCount > 0 && (
                  <p className="text-xs text-muted-foreground">
                    {t("page.mod.toggle-persist-preset.partial", { count: missingStateCount })}
                  </p>
                )}
              </>
            )}
          </div>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">{t("g.cancel")}</Button>
            </DialogClose>
            <Button
              onClick={handleCreate}
              disabled={
                createPresetMutation.isPending ||
                snapshotQuery.isPending ||
                !isPersistEnabled ||
                savableStates.length === 0
              }
            >
              {createPresetMutation.isPending && <LoaderIcon className="size-4 animate-spin" />}
              {t("g.save")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={isManageOpen} onOpenChange={handleManageOpenChange}>
        <DialogContent onClick={(e) => e.stopPropagation()} className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("page.mod.toggle-persist-preset.manage-title")}</DialogTitle>
            <DialogDescription>
              {t("page.mod.toggle-persist-preset.manage-description")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <Input value={editingName} onChange={(e) => setEditingName(e.target.value)} />
            <PersistStateList items={selectedPreset?.items ?? []} />
            <p className="text-xs text-muted-foreground">
              {t("page.mod.toggle-persist-preset.update-hint")}
            </p>
          </div>
          <DialogFooter className="flex-wrap justify-between gap-2">
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deletePresetMutation.isPending || !selectedPreset}
            >
              {deletePresetMutation.isPending ? (
                <LoaderIcon className="size-4 animate-spin" />
              ) : (
                <Trash2Icon className="size-4" />
              )}
              {t("g.delete")}
            </Button>
            <div className="flex gap-2">
              <DialogClose asChild>
                <Button variant="outline">{t("g.cancel")}</Button>
              </DialogClose>
              <Button
                variant="outline"
                onClick={handleUpdate}
                disabled={updatePresetMutation.isPending || !selectedPreset || !isPersistEnabled}
              >
                {updatePresetMutation.isPending && <LoaderIcon className="size-4 animate-spin" />}
                {t("page.mod.toggle-persist-preset.update")}
              </Button>
              <Button
                onClick={handleApply}
                disabled={applyPresetMutation.isPending || !selectedPreset}
              >
                {applyPresetMutation.isPending && <LoaderIcon className="size-4 animate-spin" />}
                {t("g.apply")}
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
