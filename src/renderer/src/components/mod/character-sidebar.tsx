// oxlint-disable react/no-children-prop
import { ValidateName } from "@renderer/components/akasha/dialogs";
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
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Field, FieldError } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useSidebarLayoutSetting } from "@renderer/hooks/use-settings";
import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { useForm } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2Icon, Search } from "lucide-react";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ScrollArea } from "../ui/scroll-area";
import { CharacterSidebarGrid } from "./character-sidebar-grid";
import { CharacterSidebarRow } from "./character-sidebar-row";
import { hasPreviewFile, isPreviewMediaFile } from "./paste-preview";

function getParentGroupPath(groupPath: string) {
  const separatorIndex = Math.max(groupPath.lastIndexOf("\\"), groupPath.lastIndexOf("/"));
  if (separatorIndex < 0) {
    return null;
  }

  return groupPath.slice(0, separatorIndex);
}

interface CharacterSidebarProps {
  groups: FolderGroup[];
  isLoading?: boolean;
  showWuwaFixer?: boolean;
  onOpenWuwaFixer?: (path: string) => Promise<void>;
}

export const CharacterSidebar = memo(function CharacterSidebar({
  groups,
  isLoading = false,
  showWuwaFixer,
  onOpenWuwaFixer,
}: CharacterSidebarProps) {
  const { t } = useTranslation();
  const createFolderFormId = "character-sidebar-create-folder-form";
  const queryClient = useQueryClient();

  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const selectedGame = useModStore((s) => s.selectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const [searchTerm, setSearchTerm] = useState("");
  const [createFolderTarget, setCreateFolderTarget] = useState<FolderGroup | null>(null);
  const [pendingPreviewDrop, setPendingPreviewDrop] = useState<{
    group: FolderGroup;
    file: File;
  } | null>(null);
  const [previewCacheKey, setPreviewCacheKey] = useState(0);
  const itemRefs = useRef<Map<string, { element: HTMLElement; group: FolderGroup }>>(new Map());
  const showSkeleton = useDelayedSkeleton(isLoading);
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();
  const { data: sidebarLayout = "row" } = useSidebarLayoutSetting();
  const createFolderForm = useForm({
    defaultValues: {
      name: "",
    },
    onSubmit: async ({ value }) => {
      if (!createFolderTarget) {
        return;
      }

      const trimmedName = value.name.trim();
      if (!trimmedName) {
        return;
      }

      const validationMessage = ValidateName(trimmedName);
      if (validationMessage) {
        toast.warning(validationMessage);
        return;
      }

      await createFolderMutation.mutateAsync({
        groupPath: createFolderTarget.path,
        name: trimmedName,
      });
    },
  });

  const createFolderMutation = useMutation({
    mutationFn: async ({ groupPath, name }: { groupPath: string; name: string }) => {
      return await window.api.invoke("util:fs:mkdir", groupPath, name);
    },
    onSuccess: async (_, variables) => {
      setExpandedGroup(variables.groupPath, true);
      setCreateFolderTarget(null);
      createFolderForm.reset();
      toast.success(t("page.mod.dialog.create-folder.#.success", { name: variables.name }));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
        queryClient.invalidateQueries({ queryKey: ["modGroup", variables.groupPath] }),
        queryClient.invalidateQueries({ queryKey: ["subGroups", variables.groupPath] }),
      ]);
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("INVALID_WINDOWS_FILENAME")) {
        toast.error(t("page.mod.dialog.create-folder.#.invalid-name"));
        return;
      }

      if (message.includes("ALREADY_EXISTS")) {
        toast.warning(t("page.mod.dialog.create-folder.#.already-exists"));
        return;
      }

      toast.error(t("page.mod.dialog.create-folder.#.failed"));
    },
  });

  const handleSelect = useCallback(
    (group: FolderGroup, resetSearch: boolean) => {
      setSelectedGroup(group);

      if (searchTerm) {
        if (resetSearch) {
          setSearchTerm("");
        }

        setTimeout(() => {
          const item = itemRefs.current.get(group.path);
          if (item?.element) {
            item.element.scrollIntoView({
              behavior: "auto",
              block: "center",
            });
          }
        }, 100);
      }
    },
    [searchTerm, setSelectedGroup],
  );

  useEffect(() => {
    let timer: NodeJS.Timeout;
    if (searchTerm) {
      timer = setTimeout(() => {
        if (itemRefs.current.size === 1) {
          const match = Array.from(itemRefs.current.values())[0];
          handleSelect(match.group, false);
        }
      }, 300);
    }
    return () => clearTimeout(timer);
  }, [searchTerm, handleSelect]);

  const handleItemClick = useCallback(
    (group: FolderGroup, _e: React.MouseEvent) => {
      handleSelect(group, true);
    },
    [handleSelect],
  );

  const invalidatePreviewQueries = useCallback(
    async (groupPath: string) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
        queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] }),
        queryClient.invalidateQueries({ queryKey: ["subGroups", groupPath] }),
        selectedGroup?.path && selectedGroup.path !== groupPath
          ? queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroup.path] })
          : Promise.resolve(),
      ]);
    },
    [queryClient, selectedGame, selectedGroup?.path],
  );

  const savePreviewFile = useCallback(
    async (group: FolderGroup, file: File) => {
      const filePath = window.webUtils.getPathForFile(file);
      if (!filePath) {
        toast.error(t("page.mod.toast.preview-drop.path-error"));
        return;
      }

      const promise = window.api.invoke(
        "mod:pastePreview",
        group.path,
        filePath,
        "path",
        group.preview,
      );
      toast.promise(promise, {
        loading: t("page.mod.toast.preview-drop.saving"),
        success: t("page.mod.toast.preview-drop.success"),
        error: t("page.mod.toast.preview-drop.error"),
      });

      promise
        .then(() => {
          setPreviewCacheKey((prev) => prev + 1);
          return invalidatePreviewQueries(group.path);
        })
        .catch((error) => {
          console.error(error);
        });
    },
    [invalidatePreviewQueries, t],
  );

  const handleItemDrop = useCallback(
    (group: FolderGroup, files: File[]) => {
      if (files.length !== 1 || !isPreviewMediaFile(files[0])) {
        toast.warning(t("page.mod.toast.preview-drop.unsupported"));
        return;
      }

      const [file] = files;
      if (hasPreviewFile(group.path, group.preview)) {
        setPendingPreviewDrop({ group, file });
        return;
      }

      void savePreviewFile(group, file);
    },
    [savePreviewFile, t],
  );

  const handlePreviewDropConfirm = useCallback(() => {
    if (!pendingPreviewDrop) {
      return;
    }

    const { group, file } = pendingPreviewDrop;
    setPendingPreviewDrop(null);
    void savePreviewFile(group, file);
  }, [pendingPreviewDrop, savePreviewFile]);

  const canAcceptPreviewDrop = useCallback((files: File[]) => {
    return files.length === 1 && isPreviewMediaFile(files[0]);
  }, []);

  const handleCreateFolderOpen = useCallback((group: FolderGroup) => {
    setCreateFolderTarget(group);
    createFolderForm.reset();
  }, []);

  const handleDeleteFolder = useCallback(
    async (group: FolderGroup) => {
      confirmTrash({
        path: group.path,
        title: t("page.mod.dialog.delete-folder.title"),
        description: t("page.mod.dialog.delete-folder.description", { name: group.name }),
        successMessage: t("page.mod.dialog.delete-folder.#.success"),
        errorMessage: t("page.mod.dialog.delete-folder.#.failed"),
        onSuccess: async () => {
          const parentGroupPath = getParentGroupPath(group.path);
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
            queryClient.invalidateQueries({ queryKey: ["modGroup", group.path] }),
            parentGroupPath
              ? queryClient.invalidateQueries({ queryKey: ["subGroups", parentGroupPath] })
              : Promise.resolve(),
          ]);
        },
      });
    },
    [confirmTrash, queryClient, selectedGame, t],
  );

  const handleManualSubGroupChange = useCallback(
    async (group: FolderGroup, enabled: boolean) => {
      const parentGroupPath = getParentGroupPath(group.path);
      const parentGroup = parentGroupPath
        ? (itemRefs.current.get(parentGroupPath)?.group ?? null)
        : null;

      await window.api
        .invoke("mod:setManualSubGroup", group.path, enabled)
        .then(async () => {
          if (!enabled && selectedGroup?.path === group.path) {
            setSelectedGroup(parentGroup);
          }

          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
            queryClient.invalidateQueries({ queryKey: ["manualSubGroups"] }),
            queryClient.invalidateQueries({ queryKey: ["subGroups"] }),
            queryClient.invalidateQueries({ queryKey: ["modGroup", group.path] }),
            parentGroupPath
              ? queryClient.invalidateQueries({ queryKey: ["modGroup", parentGroupPath] })
              : Promise.resolve(),
          ]);
        })
        .then(() => {
          toast.success(
            t(
              enabled
                ? "page.mod.toast.manual-subgroup-success"
                : "page.mod.toast.manual-subgroup-remove-success",
            ),
          );
        })
        .catch((error) => {
          toast.error(error instanceof Error ? error.message : String(error));
        });
    },
    [queryClient, selectedGame, selectedGroup?.path, setSelectedGroup, t],
  );

  const contentProps = {
    groups,
    itemRefs,
    onItemClick: handleItemClick,
    onItemDrop: handleItemDrop,
    canAcceptDrop: canAcceptPreviewDrop,
    searchTerm,
    onCreateFolder: handleCreateFolderOpen,
    onDeleteFolder: handleDeleteFolder,
    onManualSubGroupChange: handleManualSubGroupChange,
    showSkeleton,
    previewCacheKey,
    showWuwaFixer,
    onOpenWuwaFixer,
  };

  return (
    <>
      <div className="flex h-full flex-col">
        <div className="h-12 p-2">
          <div className="relative">
            <Search className="absolute right-2 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="character-search-input"
              className="h-8 pr-8 text-sm"
              placeholder={t("g.search")}
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
            />
          </div>
        </div>

        <ScrollArea className="flex-1 overflow-hidden">
          {sidebarLayout === "grid" ? (
            <CharacterSidebarGrid {...contentProps} />
          ) : (
            <CharacterSidebarRow {...contentProps} />
          )}
        </ScrollArea>
      </div>

      <Dialog
        open={!!createFolderTarget}
        onOpenChange={(open) => {
          if (!open && !createFolderMutation.isPending) {
            setCreateFolderTarget(null);
            createFolderForm.reset();
          }
        }}
      >
        <DialogContent aria-describedby={undefined}>
          <DialogHeader>
            <DialogTitle>{t("page.mod.dialog.create-folder.title")}</DialogTitle>
            <DialogDescription>
              {t("page.mod.dialog.create-folder.description", {
                name: createFolderTarget?.name ?? "",
              })}
            </DialogDescription>
          </DialogHeader>
          <form
            id={createFolderFormId}
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              e.stopPropagation();
              void createFolderForm.handleSubmit();
            }}
          >
            <createFolderForm.Field
              name="name"
              validators={{
                onChange: ({ value }) =>
                  value.trim() ? undefined : t("page.mod.dialog.create-folder.name-placeholder"),
              }}
              children={(field) => (
                <Field>
                  <Input
                    value={field.state.value}
                    onBlur={field.handleBlur}
                    onChange={(e) => field.handleChange(e.target.value)}
                    placeholder={t("page.mod.dialog.create-folder.name-placeholder")}
                    maxLength={255}
                    autoFocus
                    required
                    disabled={createFolderMutation.isPending}
                  />
                  {field.state.meta.isTouched && !field.state.meta.isValid ? (
                    <FieldError>{field.state.meta.errors.join(", ")}</FieldError>
                  ) : null}
                </Field>
              )}
            />
          </form>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setCreateFolderTarget(null);
                createFolderForm.reset();
              }}
              disabled={createFolderMutation.isPending}
            >
              {t("g.cancel")}
            </Button>
            <createFolderForm.Subscribe
              selector={(state) => [state.canSubmit, state.isSubmitting]}
              children={([canSubmit, isSubmitting]) => (
                <Button
                  form={createFolderFormId}
                  type="submit"
                  disabled={!canSubmit || createFolderMutation.isPending || isSubmitting}
                >
                  {createFolderMutation.isPending && (
                    <Loader2Icon className="size-4 animate-spin" />
                  )}
                  {t("g.create")}
                </Button>
              )}
            />
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <AlertDialog
        open={!!pendingPreviewDrop}
        onOpenChange={(open) => {
          if (!open) {
            setPendingPreviewDrop(null);
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.overwrite-preview.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.overwrite-preview.description", {
                name: pendingPreviewDrop?.group.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={handlePreviewDropConfirm}>
              {t("g.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {confirmTrashDialog}
    </>
  );
});
