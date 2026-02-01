import { Search } from "lucide-react";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "../ui/scroll-area";
import type { FolderGroup } from "@renderer/types/mod";
import { useRef, useState } from "react";
import { filter } from "es-toolkit/compat";

import { useModStore } from "@renderer/store/mod";
import { CharacterSidebarItem, CharacterSidebarItemSkeleton } from "./character-sidebar-item";
import { useDelayedSkeleton } from "@renderer/hooks/use-delayed-skeleton";

interface CharacterSidebarProps {
  groups: FolderGroup[];
  isLoading?: boolean;
  onModDrop: (files: File[], groupPath: string, options?: { allowImages?: boolean }) => void;
}

export function CharacterSidebar({ groups, isLoading = false, onModDrop }: CharacterSidebarProps) {
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const [searchTerm, setSearchTerm] = useState("");
  const itemRefs = useRef<Map<string, HTMLButtonElement>>(new Map());
  const showSkeleton = useDelayedSkeleton(isLoading);

  const filteredGroups = filter(groups, (group) =>
    group.name.toLowerCase().includes(searchTerm.toLowerCase()),
  );

  const handleSelect = (group: FolderGroup) => {
    setSelectedGroup(group);

    if (searchTerm) {
      setTimeout(() => {
        const element = itemRefs.current.get(group.name);
        if (element) {
          element.scrollIntoView({
            behavior: "auto",
            block: "center",
          });
        }
      }, 0);
      setSearchTerm("");
    }
  };

  return (
    <div className="flex flex-col h-full">
      <div className="p-2 h-12">
        <div className="relative">
          <Search className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
          <Input
            className="h-8 pr-8 text-sm"
            placeholder="검색..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
          />
        </div>
      </div>

      <ScrollArea className="flex-1 overflow-y-auto">
        <div className="flex flex-col">
          {showSkeleton
            ? Array.from({ length: 8 }).map((_, index) => (
                <CharacterSidebarItemSkeleton key={index} />
              ))
            : filteredGroups.map((group) => (
                <CharacterSidebarItem
                  key={group.name}
                  ref={(el) => {
                    if (el) itemRefs.current.set(group.name, el);
                    else itemRefs.current.delete(group.name);
                  }}
                  group={group}
                  isSelected={selectedGroup?.name === group.name}
                  onClick={() => handleSelect(group)}
                  onDrop={(files) => onModDrop(files, group.path, { allowImages: true })}
                />
              ))}
        </div>
      </ScrollArea>
    </div>
  );
}
