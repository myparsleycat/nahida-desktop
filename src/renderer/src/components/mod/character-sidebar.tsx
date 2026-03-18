import { ValidateName } from "@renderer/components/akasha/dialogs";
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2Icon, Search } from "lucide-react";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ScrollArea } from "../ui/scroll-area";
import { CharacterSidebarItem, CharacterSidebarItemSkeleton } from "./character-sidebar-item";

interface CharacterSidebarProps {
  groups: FolderGroup[];
  isLoading?: boolean;
  onModDrop: (files: File[], groupPath: string, options?: { allowImages?: boolean }) => void;
}

function useSubGroups(group: FolderGroup, shouldFetch: boolean, refreshKey: number) {
  const [subGroups, setSubGroups] = useState<FolderGroup[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (!shouldFetch) {
      setSubGroups([]);
      return;
    }

    let cancelled = false;
    setIsLoading(true);

    window.api
      .invoke("mod:getSubGroups", group.path)
      .then((result: FolderGroup[]) => {
        if (!cancelled) {
          setSubGroups(result);
          setIsLoading(false);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setSubGroups([]);
          setIsLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [shouldFetch, group.path, refreshKey]);

  return { subGroups, isLoading };
}

const CharacterSidebarItemWithChildren = memo(function CharacterSidebarItemWithChildren({
  group,
  itemRefs,
  onItemClick,
  onItemDrop,
  onCollapseSelf,
  depth,
  searchTerm,
  onCreateFolder,
  onDeleteFolder,
  refreshKey,
}: {
  group: FolderGroup;
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLButtonElement; group: FolderGroup }>>;
  onItemClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onItemDrop: (group: FolderGroup, files: File[]) => void;
  onCollapseSelf?: () => void;
  depth: number;
  searchTerm: string;
  onCreateFolder: (group: FolderGroup) => void;
  onDeleteFolder: (group: FolderGroup) => void;
  refreshKey: number;
}) {
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const expandedGroups = useModStore((s) => s.expandedGroups);
  const toggleExpandedGroup = useModStore((s) => s.toggleExpandedGroup);
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const persistentGroups = useModStore((s) => s.persistentGroups);

  const isExpanded = expandedGroups.has(group.path);
  const isPersistent = persistentGroups.has(group.path);

  const shouldFetchSubGroups = isExpanded || (!!searchTerm && isPersistent);
  const { subGroups } = useSubGroups(group, shouldFetchSubGroups, refreshKey);

  const isSelfMatch = group.name.toLowerCase().includes(searchTerm.toLowerCase());

  const shouldShowParent = !searchTerm || isSelfMatch;
  const showSubGroups = isExpanded || (!!searchTerm && isPersistent);

  const handleChildItemClick = useCallback(
    (clickedGroup: FolderGroup, e: React.MouseEvent) => {
      if (!isExpanded) {
        setExpandedGroup(group.path, true);
      }
      onItemClick(clickedGroup, e);
    },
    [isExpanded, group.path, setExpandedGroup, onItemClick],
  );

  const handleItemClickInternal = useCallback(
    (clickedGroup: FolderGroup, e: React.MouseEvent) => {
      if (e.ctrlKey && onCollapseSelf) {
        onCollapseSelf();
      } else {
        onItemClick(clickedGroup, e);
      }
    },
    [onCollapseSelf, onItemClick],
  );

  if (!shouldShowParent && !showSubGroups) {
    return null;
  }

  return (
    <>
      {shouldShowParent && (
        <CharacterSidebarItem
          itemRefs={itemRefs}
          group={group}
          isSelected={selectedGroup?.path === group.path}
          onClick={handleItemClickInternal}
          onDrop={onItemDrop}
          onCreateFolder={onCreateFolder}
          onDeleteFolder={onDeleteFolder}
          depth={depth}
        />
      )}
      {showSubGroups &&
        subGroups.map((sub) => (
          <CharacterSidebarItemWithChildren
            key={sub.path}
            group={sub}
            itemRefs={itemRefs}
            onItemClick={handleChildItemClick}
            onItemDrop={onItemDrop}
            onCollapseSelf={() => toggleExpandedGroup(group.path)}
            depth={depth + 1}
            searchTerm={searchTerm}
            onCreateFolder={onCreateFolder}
            onDeleteFolder={onDeleteFolder}
            refreshKey={refreshKey}
          />
        ))}
    </>
  );
});

export const CharacterSidebar = memo(function CharacterSidebar({
  groups,
  isLoading = false,
  onModDrop,
}: CharacterSidebarProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const selectedGame = useModStore((s) => s.selectedGame);
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const [searchTerm, setSearchTerm] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  const [createFolderTarget, setCreateFolderTarget] = useState<FolderGroup | null>(null);
  const [newFolderName, setNewFolderName] = useState("");
  const itemRefs = useRef<Map<string, { element: HTMLButtonElement; group: FolderGroup }>>(
    new Map(),
  );
  const showSkeleton = useDelayedSkeleton(isLoading);
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();

  const createFolderMutation = useMutation({
    mutationFn: async ({ groupPath, name }: { groupPath: string; name: string }) => {
      return await window.api.invoke("util:fs:mkdir", groupPath, name);
    },
    onSuccess: async (_, variables) => {
      setExpandedGroup(variables.groupPath, true);
      setRefreshKey((prev) => prev + 1);
      setCreateFolderTarget(null);
      setNewFolderName("");
      toast.success(t("page.mod.dialog.create-folder.#.success", { name: variables.name }));
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
        queryClient.invalidateQueries({ queryKey: ["modGroup", variables.groupPath] }),
      ]);
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("INVALID_WINDOWS_FILENAME")) {
        toast.error(t("page.mod.dialog.create-folder.#.invalid-name"));
      } else if (message.includes("ALREADY_EXISTS")) {
        toast.warning(t("page.mod.dialog.create-folder.#.already-exists"));
      } else {
        toast.error(t("page.mod.dialog.create-folder.#.failed"));
      }
    },
  });

  const handleSelect = useCallback(
    (group: FolderGroup, resetSearch: boolean) => {
      setSelectedGroup(group);

      if (searchTerm) {
        if (resetSearch) setSearchTerm("");
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

  const handleItemDrop = useCallback(
    (group: FolderGroup, files: File[]) => {
      onModDrop(files, group.path, { allowImages: true });
    },
    [onModDrop],
  );

  const handleCreateFolderOpen = useCallback((group: FolderGroup) => {
    setCreateFolderTarget(group);
    setNewFolderName("");
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
          setRefreshKey((prev) => prev + 1);
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] }),
            queryClient.invalidateQueries({ queryKey: ["modGroup", group.path] }),
          ]);
        },
      });
    },
    [confirmTrash, queryClient, selectedGame, t],
  );

  const handleCreateFolderSubmit = useCallback(
    async (e: React.SubmitEvent<HTMLFormElement>) => {
      e.preventDefault();

      if (!createFolderTarget) {
        return;
      }

      const trimmedName = newFolderName.trim();
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
    [createFolderMutation, createFolderTarget, newFolderName],
  );

  return (
    <>
      <div className="flex flex-col h-full">
        <div className="p-2 h-12">
          <div className="relative">
            <Search className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
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
          <div className="flex flex-col">
            {showSkeleton
              ? Array.from({ length: 8 }).map((_, index) => (
                  <CharacterSidebarItemSkeleton key={index.toString()} />
                ))
              : groups.map((group) => (
                  <CharacterSidebarItemWithChildren
                    key={group.path}
                    group={group}
                    itemRefs={itemRefs}
                    onItemClick={handleItemClick}
                    onItemDrop={handleItemDrop}
                    depth={0}
                    searchTerm={searchTerm}
                    onCreateFolder={handleCreateFolderOpen}
                    onDeleteFolder={handleDeleteFolder}
                    refreshKey={refreshKey}
                  />
                ))}
          </div>
        </ScrollArea>
      </div>

      <Dialog
        open={!!createFolderTarget}
        onOpenChange={(open) => {
          if (!open && !createFolderMutation.isPending) {
            setCreateFolderTarget(null);
            setNewFolderName("");
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
          <form className="space-y-4" onSubmit={handleCreateFolderSubmit}>
            <Input
              value={newFolderName}
              onChange={(e) => setNewFolderName(e.target.value)}
              placeholder={t("page.mod.dialog.create-folder.name-placeholder")}
              maxLength={255}
              autoFocus
              required
              disabled={createFolderMutation.isPending}
            />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  setCreateFolderTarget(null);
                  setNewFolderName("");
                }}
                disabled={createFolderMutation.isPending}
              >
                {t("g.cancel")}
              </Button>
              <Button type="submit" disabled={createFolderMutation.isPending}>
                {createFolderMutation.isPending && <Loader2Icon className="size-4 animate-spin" />}
                {t("g.create")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
      {confirmTrashDialog}
    </>
  );
});
