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
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Field, FieldError } from "@renderer/components/ui/field";
import { Input } from "@renderer/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useGames } from "@renderer/hooks/use-mod-data";
import { useSidebarLayoutSetting } from "@renderer/hooks/use-settings";
import { setSetting } from "@renderer/lib/settings";
import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { toErrorMessage } from "@shared/utils";
import { useForm } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  ArrowDownIcon,
  ArrowUpDownIcon,
  ArrowUpIcon,
  EllipsisIcon,
  FolderPlus,
  FolderXIcon,
  LayoutGridIcon,
  ListIcon,
  Loader2Icon,
  Search,
} from "lucide-react";
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

function getGroupName(groupPath: string) {
  const separatorIndex = Math.max(groupPath.lastIndexOf("\\"), groupPath.lastIndexOf("/"));
  if (separatorIndex < 0) {
    return groupPath;
  }

  return groupPath.slice(separatorIndex + 1);
}

function getParentGroup(
  parentGroupPath: string | null,
  groupsByPath: Map<string, FolderGroup>,
  itemRefs: Map<string, { element: HTMLElement; group: FolderGroup }>,
) {
  if (!parentGroupPath) {
    return null;
  }

  return (
    groupsByPath.get(parentGroupPath) ??
    itemRefs.get(parentGroupPath)?.group ?? {
      name: getGroupName(parentGroupPath),
      path: parentGroupPath,
      mods: [],
    }
  );
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
  const markUserSelectedDuringDownload = useModStore((s) => s.markUserSelectedDuringDownload);
  const selectedGame = useModStore((s) => s.selectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const { data: games = [] } = useGames();
  const selectedGameConfig = games.find((game) => game.game === selectedGame);
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const [searchTerm, setSearchTerm] = useState("");
  const sortKey = useModStore((s) => s.folderSortKey);
  const setSortKey = useModStore((s) => s.setFolderSortKey);
  const sortDirection = useModStore((s) => s.folderSortDirection);
  const setSortDirection = useModStore((s) => s.setFolderSortDirection);
  const [hideEmptyGroups, setHideEmptyGroups] = useState(false);
  const [createFolderTarget, setCreateFolderTarget] = useState<FolderGroup | null>(null);
  const [pendingPreviewDrop, setPendingPreviewDrop] = useState<{
    group: FolderGroup;
    file: File;
  } | null>(null);
  const [previewCacheKey, setPreviewCacheKey] = useState(0);
  const itemRefs = useRef<Map<string, { element: HTMLElement; group: FolderGroup }>>(new Map());
  const groupsByPathRef = useRef<Map<string, FolderGroup>>(new Map());
  const scrollToPathRef = useRef<((path: string) => void) | null>(null);
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  const showSkeleton = useDelayedSkeleton(isLoading);
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();
  const { data: sidebarLayout = "row" } = useSidebarLayoutSetting();
  const nextSidebarLayout = sidebarLayout === "grid" ? "row" : "grid";
  const nextSidebarLayoutLabel = t(`page.setting.mod.layout.sidebar.modes.${nextSidebarLayout}`);
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
      const message = toErrorMessage(error);
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
      markUserSelectedDuringDownload();

      if (searchTerm) {
        if (resetSearch) {
          setSearchTerm("");
        }

        setTimeout(() => {
          scrollToPathRef.current?.(group.path);
        }, 100);
      }
    },
    [searchTerm, setSelectedGroup, markUserSelectedDuringDownload],
  );

  const handleSearchChange = useCallback(
    (value: string) => {
      setSearchTerm(value);
      if (value.trim()) {
        markUserSelectedDuringDownload();
      }
    },
    [markUserSelectedDuringDownload],
  );

  const handleVisibleRowsChange = useCallback((rows: { path: string; group: FolderGroup }[]) => {
    groupsByPathRef.current = new Map(rows.map((row) => [row.path, row.group]));
  }, []);

  useEffect(() => {
    let timer: NodeJS.Timeout;
    const normalizedSearch = searchTerm.trim().toLowerCase();
    if (normalizedSearch) {
      timer = setTimeout(() => {
        const matches =
          sidebarLayout === "row"
            ? Array.from(groupsByPathRef.current.values()).filter((group) =>
                group.name.toLowerCase().includes(normalizedSearch),
              )
            : Array.from(itemRefs.current.values())
                .map((item) => item.group)
                .filter((group) => group.name.toLowerCase().includes(normalizedSearch));

        if (matches.length === 1) {
          handleSelect(matches[0], false);
        }
      }, 300);
    }
    return () => clearTimeout(timer);
  }, [searchTerm, handleSelect, sidebarLayout]);

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
    [queryClient, selectedGame, selectedGroup],
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
      const parentGroup = getParentGroup(
        parentGroupPath,
        groupsByPathRef.current,
        itemRefs.current,
      );

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
          toast.error(toErrorMessage(error));
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
    sortKey,
    sortDirection,
    hideEmptyGroups,
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
        <div className="flex h-12 items-center gap-1 p-2">
          <div className="relative min-w-0 flex-1">
            <Search className="absolute top-1/2 right-2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              id="character-search-input"
              className="h-8 pr-8 text-sm"
              placeholder={t("g.search")}
              value={searchTerm}
              onChange={(e) => handleSearchChange(e.target.value)}
            />
          </div>
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger
                render={
                  <DropdownMenuTrigger
                    render={
                      <Button
                        type="button"
                        size="icon"
                        variant="ghost"
                        aria-label={t("page.mod.character-sidebar.more-options")}
                      />
                    }
                  />
                }
              >
                <EllipsisIcon />
              </TooltipTrigger>
              <TooltipContent side="bottom">
                {t("page.mod.character-sidebar.more-options")}
              </TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end" className="w-56" finalFocus={false}>
              <DropdownMenuItem
                disabled={!selectedGameConfig?.modFolderPath}
                onClick={() => {
                  if (!selectedGameConfig?.modFolderPath) {
                    return;
                  }

                  handleCreateFolderOpen({
                    name: selectedGameConfig.game,
                    path: selectedGameConfig.modFolderPath,
                    mods: [],
                  });
                }}
              >
                <FolderPlus />
                {t("page.mod.character-sidebar.create-folder")}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <ArrowUpDownIcon />
                  {t("page.mod.character-sidebar.sort.label")}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-48">
                  <DropdownMenuRadioGroup value={sortKey}>
                    <DropdownMenuLabel>
                      {t("page.mod.character-sidebar.sort.field")}
                    </DropdownMenuLabel>
                    {(["name", "mod-count", "enabled-mod-count"] as const).map((value) => (
                      <DropdownMenuRadioItem
                        key={value}
                        value={value}
                        onClick={() => setSortKey(value)}
                      >
                        {t(`page.mod.character-sidebar.sort.${value}`)}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                  <DropdownMenuSeparator />
                  <DropdownMenuRadioGroup value={sortDirection}>
                    <DropdownMenuLabel>
                      {t("page.mod.character-sidebar.sort.direction")}
                    </DropdownMenuLabel>
                    <DropdownMenuRadioItem
                      value="ascending"
                      onClick={() => setSortDirection("ascending")}
                    >
                      <ArrowUpIcon />
                      {t("page.mod.character-sidebar.sort.ascending")}
                    </DropdownMenuRadioItem>
                    <DropdownMenuRadioItem
                      value="descending"
                      onClick={() => setSortDirection("descending")}
                    >
                      <ArrowDownIcon />
                      {t("page.mod.character-sidebar.sort.descending")}
                    </DropdownMenuRadioItem>
                  </DropdownMenuRadioGroup>
                </DropdownMenuSubContent>
              </DropdownMenuSub>
              <DropdownMenuSeparator />
              <DropdownMenuCheckboxItem
                checked={hideEmptyGroups}
                onCheckedChange={() => setHideEmptyGroups((value) => !value)}
              >
                <FolderXIcon />
                {t("page.mod.character-sidebar.hide-empty-folders")}
              </DropdownMenuCheckboxItem>
              <DropdownMenuItem
                onClick={() => {
                  setSetting("mod.sidebarLayout", nextSidebarLayout).catch((error) => {
                    toast.error(toErrorMessage(error));
                  });
                }}
              >
                {sidebarLayout === "grid" ? <ListIcon /> : <LayoutGridIcon />}
                {t("page.mod.character-sidebar.toggle-layout", { mode: nextSidebarLayoutLabel })}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        <ScrollArea className="flex-1 overflow-hidden" viewportRef={setViewport}>
          {sidebarLayout === "grid" ? (
            <CharacterSidebarGrid {...contentProps} />
          ) : (
            <CharacterSidebarRow
              {...contentProps}
              viewport={viewport}
              scrollToPathRef={scrollToPathRef}
              onVisibleRowsChange={handleVisibleRowsChange}
            />
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
