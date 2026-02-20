import { Input } from "@renderer/components/ui/input";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";
import { useModStore } from "@renderer/store/mod";
import type { FolderGroup } from "@renderer/types/mod";
import { Search } from "lucide-react";
import { memo, useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ScrollArea } from "../ui/scroll-area";
import { CharacterSidebarItem, CharacterSidebarItemSkeleton } from "./character-sidebar-item";

interface CharacterSidebarProps {
  groups: FolderGroup[];
  isLoading?: boolean;
  onModDrop: (files: File[], groupPath: string, options?: { allowImages?: boolean }) => void;
}

function useSubGroups(group: FolderGroup, shouldFetch: boolean) {
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
  }, [shouldFetch, group.path]);

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
}: {
  group: FolderGroup;
  itemRefs: React.MutableRefObject<Map<string, { element: HTMLButtonElement; group: FolderGroup }>>;
  onItemClick: (group: FolderGroup, e: React.MouseEvent) => void;
  onItemDrop: (group: FolderGroup, files: File[]) => void;
  onCollapseSelf?: () => void;
  depth: number;
  searchTerm: string;
}) {
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const expandedGroups = useModStore((s) => s.expandedGroups);
  const toggleExpandedGroup = useModStore((s) => s.toggleExpandedGroup);
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const persistentGroups = useModStore((s) => s.persistentGroups);

  const isExpanded = expandedGroups.has(group.path);
  const isPersistent = persistentGroups.has(group.path);

  const shouldFetchSubGroups = isExpanded || (!!searchTerm && isPersistent);
  const { subGroups } = useSubGroups(group, shouldFetchSubGroups);

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

  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const [searchTerm, setSearchTerm] = useState("");
  const itemRefs = useRef<Map<string, { element: HTMLButtonElement; group: FolderGroup }>>(
    new Map(),
  );
  const showSkeleton = useDelayedSkeleton(isLoading);

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

  return (
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
                />
              ))}
        </div>
      </ScrollArea>
    </div>
  );
});
