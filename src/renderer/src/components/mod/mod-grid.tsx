import { useEffect, useRef } from "react";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Skeleton } from "@renderer/components/ui/skeleton";
import { ModCard } from "./mod-card";
import type { ModInfo } from "@shared/types";
import { useModStore } from "@renderer/store/mod";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import { useCharacters, useModGroup } from "@renderer/hooks/use-mod-data";
import { toast } from "sonner";

interface ModGridProps {
  isDragging?: boolean;
}

export function ModGrid({ isDragging }: ModGridProps) {
  const selectedGame = useModStore((s) => s.selectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const searchQuery = useModStore((s) => s.searchQuery);
  const scrollAreaRef = useRef<HTMLDivElement>(null);

  const { data: characters = [] } = useCharacters(selectedGame);
  const selectedGroupPath = characters.find((c) => c.name === selectedGroup)?.path;
  const { data: activeGroup } = useModGroup(selectedGroupPath);

  const { toggleModMutation, updateToggleKeyMutation } = useModMutations(selectedGroup, characters);

  const mods = useFilteredMods(activeGroup?.mods || [], searchQuery);
  const isLoading = !activeGroup && !!selectedGroup;

  useEffect(() => {
    if (scrollAreaRef.current) {
      const viewport = scrollAreaRef.current.querySelector("[data-radix-scroll-area-viewport]");
      if (viewport) {
        viewport.scrollTop = 0;
      }
    }
  }, [selectedGroup]);

  const handleToggleKeyUpdate = (
    modPath: string,
    iniFileName: string,
    sectionName: string,
    variable: string,
    value: string,
  ) => {
    if (!value.trim()) {
      toast.error("값을 입력해주세요.");
      return;
    }
    updateToggleKeyMutation.mutate({
      modPath,
      iniFileName,
      sectionName,
      variable,
      value,
    });
  };

  return (
    <ScrollArea ref={scrollAreaRef} className="flex-1 overflow-y-auto">
      <div className="relative">
        <div className="gap-3 p-3 grid grid-cols-1 min-[1000px]:grid-cols-2 min-[1500px]:grid-cols-3 min-[2000px]:grid-cols-4 min-[2500px]:grid-cols-5 min-[3000px]:grid-cols-6 min-[3500px]:grid-cols-7">
          {isLoading
            ? Array.from({ length: 12 }).map((_, index) => (
                <div key={index} className="flex flex-col space-y-3 rounded-lg border p-4">
                  <Skeleton className="h-48 w-full rounded-md" />
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-3/4" />
                    <Skeleton className="h-4 w-1/2" />
                  </div>
                  <div className="flex gap-2">
                    <Skeleton className="h-9 flex-1" />
                    <Skeleton className="h-9 w-9" />
                  </div>
                </div>
              ))
            : mods.map((mod) => (
                <ModCard
                  key={mod.path}
                  mod={mod}
                  onToggle={(m) => toggleModMutation.mutate(m)}
                  onToggleKeyUpdate={handleToggleKeyUpdate}
                />
              ))}
        </div>
      </div>
    </ScrollArea>
  );
}
