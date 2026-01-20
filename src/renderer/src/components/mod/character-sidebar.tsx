import { Search, HelpCircle, Terminal, Settings, MoreHorizontal, Folder } from "lucide-react";
import { Input } from "@renderer/components/ui/input";
import { cn } from "@renderer/lib/utils";
import { ScrollArea } from "../ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import type { FolderGroup } from "@renderer/types/mod";
import { useRef, useState } from "react";
import { filter } from "es-toolkit/compat";

import { useModStore } from "@renderer/store/mod";

interface CharacterSidebarProps {
  groups: FolderGroup[];
  isLoading?: boolean;
}

export function CharacterSidebar({ groups, isLoading = false }: CharacterSidebarProps) {
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const [searchTerm, setSearchTerm] = useState("");
  const itemRefs = useRef<Map<string, HTMLButtonElement>>(new Map());

  const filteredGroups = filter(groups, (group) =>
    group.name.toLowerCase().includes(searchTerm.toLowerCase()),
  );

  const handleSelect = (groupName: string) => {
    setSelectedGroup(groupName);

    if (searchTerm) {
      setTimeout(() => {
        const element = itemRefs.current.get(groupName);
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
    <div className="bg-[#1a1a1a] flex flex-col h-full border-[#2a2a2a]">
      <div className="p-2 h-12">
        <div className="relative">
          <Search className="absolute right-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
          <Input
            className="bg-[#0a0a0a] border-[#2a2a2a] h-8 pr-8 text-sm"
            placeholder="검색..."
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
          />
        </div>
      </div>

      <ScrollArea className="flex-1 overflow-y-auto">
        <div className="flex flex-col">
          {isLoading
            ? // Skeleton loading state
              Array.from({ length: 8 }).map((_, index) => (
                <div
                  key={index}
                  className="w-full grid items-center gap-3 pl-2 pr-4 py-2"
                  style={{ gridTemplateColumns: "auto 1fr auto" }}
                >
                  <Skeleton className="w-10 h-10 rounded-full" />
                  <Skeleton className="h-4 w-full" />
                  <Skeleton className="h-4 w-8" />
                </div>
              ))
            : filteredGroups.map((group) => (
                <button
                  key={group.name}
                  ref={(el) => {
                    if (el) itemRefs.current.set(group.name, el);
                    else itemRefs.current.delete(group.name);
                  }}
                  onClick={() => handleSelect(group.name)}
                  className={cn(
                    "w-full grid grid-columns-[auto_1fr_auto] items-center gap-3 pl-2 pr-4 py-2 hover:bg-[#2a2a2a]",
                    selectedGroup === group.name && "bg-[#2a2a2a]",
                  )}
                  style={{ gridTemplateColumns: "auto 1fr auto" }}
                >
                  <div className="w-10 h-10 rounded-full bg-[#3a3a3a] flex items-center justify-center shrink-0 overflow-hidden">
                    {group.preview ? (
                      group.preview.toLowerCase().match(/\.(mp4|webm|avi|mkv|mov)$/) ? (
                        <video
                          src={`local://${group.preview}`}
                          className="w-full h-full object-cover"
                          autoPlay
                          loop
                          muted
                          playsInline
                        />
                      ) : (
                        <img
                          src={`local://${group.preview}`}
                          alt={group.name}
                          className="w-full h-full object-cover"
                          decoding="async"
                          loading="lazy"
                        />
                      )
                    ) : (
                      <span className="text-lg font-bold text-center">?</span>
                    )}
                  </div>
                  <span className="text-left text-sm text-foreground truncate min-w-0">
                    {group.name}
                  </span>
                  <span className="text-sm text-muted-foreground shrink-0">
                    {group.mods.length}
                  </span>
                </button>
              ))}
        </div>
      </ScrollArea>
    </div>
  );
}
