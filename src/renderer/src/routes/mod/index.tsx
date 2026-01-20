import { CharacterSidebar } from "@renderer/components/mod/character-sidebar";
import { ContentHeader } from "@renderer/components/mod/content-header";
import { ModCard } from "@renderer/components/mod/mod-card";
import { DownloadConfirmationOverlay } from "@renderer/components/mod/download-confirmation-overlay";
import { Titlebar } from "@renderer/components/titlebar";
import { Button } from "@renderer/components/ui/button";
import { Skeleton } from "@renderer/components/ui/skeleton";
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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { createFileRoute } from "@tanstack/react-router";
import { Plus, FolderOpen } from "lucide-react";
import { useState, useEffect, useMemo } from "react";
import type { FolderGroup, Preset, ModInfo, GameConfig } from "@shared/types";
import { useQuery, useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { disassemble, getChoseong } from "es-hangul";
import { getSearchScore } from "@renderer/lib/sejong";
import { Logger } from "@renderer/lib/logger";

import { useModStore } from "@renderer/store/mod";

export const Route = createFileRoute("/mod/")({
  component: RouteComponent,
});

function RouteComponent() {
  const { queryClient } = Route.useRouteContext();

  const selectedGame = useModStore((s) => s.selectedGame);
  const setSelectedGame = useModStore((s) => s.setSelectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const selectedPreset = useModStore((s) => s.selectedPreset);
  const setSelectedPreset = useModStore((s) => s.setSelectedPreset);
  const newPresetName = useModStore((s) => s.newPresetName);
  const setNewPresetName = useModStore((s) => s.setNewPresetName);
  const isPresetDialogOpen = useModStore((s) => s.isPresetDialogOpen);
  const setIsPresetDialogOpen = useModStore((s) => s.setIsPresetDialogOpen);
  const isSelectedPresetDialogOpen = useModStore((s) => s.isSelectedPresetDialogOpen);
  const setIsSelectedPresetDialogOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);

  const isAddGameDialogOpen = useModStore((s) => s.isAddGameDialogOpen);
  const setIsAddGameDialogOpen = useModStore((s) => s.setIsAddGameDialogOpen);
  const newGameName = useModStore((s) => s.newGameName);
  const setNewGameName = useModStore((s) => s.setNewGameName);
  const newGamePath = useModStore((s) => s.newGamePath);
  const setNewGamePath = useModStore((s) => s.setNewGamePath);

  const downloadMode = useModStore((s) => s.downloadMode);
  const setDownloadMode = useModStore((s) => s.setDownloadMode);

  const searchQuery = useModStore((s) => s.searchQuery);
  const setSearchQuery = useModStore((s) => s.setSearchQuery);

  const { data: games = [] } = useQuery<GameConfig[]>({
    queryKey: ["games"],
    queryFn: () => window.api.invoke("mod:getGames"),
  });

  const { data: groups = [], isLoading: isGroupsLoading } = useQuery<FolderGroup[]>({
    queryKey: ["mods", selectedGame],
    queryFn: () => window.api.invoke("mod:list", selectedGame),
    enabled: !!selectedGame,
  });

  const { data: presets = [] } = useQuery<Preset[]>({
    queryKey: ["presets", selectedGame],
    queryFn: () => window.api.invoke("mod:getPresets", selectedGame),
    enabled: !!selectedGame,
  });

  useEffect(() => {
    const initGame = async () => {
      const lastGame = await window.api.invoke("mod:getLastGame");
      if (lastGame && games.find((g) => g.game === lastGame)) {
        setSelectedGame(lastGame);
      }
    };
    if (games.length > 0 && !selectedGame) {
      initGame();
    }
  }, [games]);

  useEffect(() => {
    if (groups.length > 0) {
      if (!selectedGroup || !groups.find((g) => g.name === selectedGroup)) {
        setSelectedGroup(groups[0].name);
      }
    } else {
      setSelectedGroup(null);
    }
  }, [groups, selectedGroup]);

  useEffect(() => {
    const handleFocus = () => {
      if (selectedGame) {
        queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] });
      }
    };

    window.addEventListener("focus", handleFocus);
    return () => {
      window.removeEventListener("focus", handleFocus);
    };
  }, [selectedGame, queryClient]);

  useEffect(() => {
    const unsubscribe = window.api.on("download:completed", (data) => {
      if (selectedGame) {
        queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] });
        toast.success(`"${data.name}" 다운로드가 완료되었습니다.`);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [selectedGame, queryClient]);

  const currentMods = useMemo(() => {
    const mods = groups.find((g) => g.name === selectedGroup)?.mods || [];

    const scoredMods = mods.map((m) => {
      if (!searchQuery) return { mod: m, score: 0 };
      const query = searchQuery.toLowerCase();
      const lowerName = m.name.toLowerCase();
      const cachedData = {
        lowerName,
        jamo: disassemble(lowerName),
        chosung: getChoseong(lowerName),
      };
      return {
        mod: m,
        score: getSearchScore(m.name, query, cachedData),
      };
    });

    const filtered = searchQuery ? scoredMods.filter((sm) => sm.score > 0) : scoredMods;

    return filtered
      .sort((a, b) => {
        if (a.mod.isEnabled !== b.mod.isEnabled) {
          return a.mod.isEnabled ? -1 : 1;
        }
        if (searchQuery && a.score !== b.score) {
          return b.score - a.score;
        }
        return a.mod.name.localeCompare(b.mod.name);
      })
      .map((sm) => sm.mod);
  }, [groups, selectedGroup, searchQuery]);

  const updateLocalGroupCache = (refreshedGroup: FolderGroup) => {
    queryClient.setQueryData<FolderGroup[]>(["mods", selectedGame], (oldGroups) => {
      if (!oldGroups) return [];
      return oldGroups.map((g) => (g.name === refreshedGroup.name ? refreshedGroup : g));
    });
  };

  const addGameMutation = useMutation({
    mutationFn: ({ name, path }: { name: string; path: string }) =>
      window.api.invoke("mod:addGame", name, path),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["games"] });
      setNewGameName("");
      setNewGamePath("");
      setIsAddGameDialogOpen(false);
      toast.success("게임이 추가되었습니다.");
    },
  });

  const toggleModMutation = useMutation({
    mutationFn: async (mod: ModInfo) => {
      try {
        await window.api.invoke("mod:toggle", mod.path);
        const currentGroupPath = groups.find((g) => g.name === selectedGroup)?.path;
        if (currentGroupPath) {
          const refreshedGroup = (await window.api.invoke(
            "mod:scanGroup",
            currentGroupPath,
          )) as FolderGroup;
          return refreshedGroup;
        }
        return null;
      } catch (error: any) {
        const errorMessage = error.message || "";
        if (errorMessage.includes("ALREADY_EXISTS")) {
          const folderName = errorMessage.split("ALREADY_EXISTS:")[1] || "알 수 없는";
          toast.error(`이미 "${folderName}" 폴더가 존재합니다.`);
        } else {
          toast.error("모드 상태 변경에 실패했습니다.");
        }
        throw error;
      }
    },
    onSuccess: (refreshedGroup) => {
      if (refreshedGroup) {
        updateLocalGroupCache(refreshedGroup);
      }
    },
  });

  const updateToggleKeyMutation = useMutation({
    mutationFn: async (params: {
      modPath: string;
      iniFileName: string;
      sectionName: string;
      variable: string;
      value: string;
    }) => {
      await window.api.invoke(
        "mod:updateToggleKey",
        params.modPath,
        params.iniFileName,
        params.sectionName,
        params.variable,
        params.value,
      );
      const currentGroupPath = groups.find((g) => g.name === selectedGroup)?.path;
      if (currentGroupPath) {
        const refreshedGroup = (await window.api.invoke(
          "mod:scanGroup",
          currentGroupPath,
        )) as FolderGroup;
        return refreshedGroup;
      }
      return null;
    },
    onSuccess: (refreshedGroup) => {
      if (refreshedGroup) {
        updateLocalGroupCache(refreshedGroup);
      }
    },
  });

  const createPresetMutation = useMutation({
    mutationFn: () => window.api.invoke("mod:createPreset", selectedGame, newPresetName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["presets", selectedGame] });
      setNewPresetName("");
      setIsPresetDialogOpen(false);
      toast.success("프리셋이 추가되었습니다.");
    },
  });

  const applyPresetMutation = useMutation({
    mutationFn: (presetId: string) => window.api.invoke("mod:applyPreset", presetId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["mods", selectedGame] });
      setIsSelectedPresetDialogOpen(false);
      toast.success("프리셋이 적용되었습니다.");
    },
  });

  const deletePresetMutation = useMutation({
    mutationFn: (presetId: string) => window.api.invoke("mod:deletePreset", presetId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["presets", selectedGame] });
      setSelectedPreset(null);
      setIsSelectedPresetDialogOpen(false);
      toast.success("프리셋이 삭제되었습니다.");
    },
  });

  const handleGameSelect = async (game: string) => {
    setSelectedGame(game);
    await window.api.invoke("mod:setLastGame", game);
  };

  const handleBrowseFolder = async () => {
    const path = await window.api.invoke("mod:pickFolder");
    if (path) {
      setNewGamePath(path);
    }
  };

  const handleDownloadConfirm = async () => {
    if (!downloadMode || !selectedGroup) return;

    const selectedGroupData = groups.find((g) => g.name === selectedGroup);
    if (!selectedGroupData) return;

    try {
      await window.api.invoke(
        "drive:fn:startDownloadWithPath",
        downloadMode.downloadId,
        selectedGroupData.path,
      );
      toast.success("다운로드가 시작되었습니다.");
      setDownloadMode(null);
    } catch (error) {
      toast.error("다운로드 시작에 실패했습니다.");
      Logger.error(error, "Route:Mod:handleDownloadConfirm");
    }
  };

  const handleDownloadCancel = async () => {
    if (!downloadMode) return;

    try {
      await window.api.invoke("drive:fn:cancelPendingDownload", downloadMode.downloadId);
      setDownloadMode(null);
    } catch (error) {
      Logger.error(error, "Route:Mod:handleDownloadCancel");
    }
  };

  return (
    <>
      <Titlebar title={{ text: "모드", position: "center" }} />

      <div className="flex-1 flex overflow-hidden h-full">
        <div className="border-r h-full flex flex-col w-64">
          <div className="flex-1 overflow-y-auto h-full">
            <CharacterSidebar groups={groups} isLoading={isGroupsLoading} />
          </div>

          <div className="flex flex-col items-center justify-center w-full p-2 border-t space-y-3">
            <div className="flex w-full space-x-1">
              <Select value={selectedGame || undefined} onValueChange={handleGameSelect}>
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Select a Game" />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectGroup>
                    <SelectLabel>Games</SelectLabel>
                    {games.map((game) => (
                      <SelectItem key={game.game} value={game.game}>
                        {game.game}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>

              <Dialog open={isAddGameDialogOpen} onOpenChange={setIsAddGameDialogOpen}>
                <DialogTrigger asChild>
                  <Button variant="outline" size="icon">
                    <Plus className="size-4" />
                  </Button>
                </DialogTrigger>
                <DialogContent className="w-100">
                  <DialogHeader>
                    <DialogTitle>게임 추가</DialogTitle>
                  </DialogHeader>
                  <div className="space-y-4 py-4">
                    <div className="space-y-2">
                      <Input
                        placeholder="게임 이름 (예: 원공노)"
                        value={newGameName}
                        onChange={(e) => setNewGameName(e.target.value)}
                      />
                    </div>
                    <div className="flex gap-2">
                      <Input placeholder="모드 폴더 경로" value={newGamePath} readOnly />
                      <Button variant="outline" size="icon" onClick={handleBrowseFolder}>
                        <FolderOpen className="size-4" />
                      </Button>
                    </div>
                  </div>
                  <DialogFooter>
                    <DialogClose asChild>
                      <Button variant="outline">취소</Button>
                    </DialogClose>
                    <Button
                      onClick={() =>
                        addGameMutation.mutate({ name: newGameName, path: newGamePath })
                      }
                    >
                      추가
                    </Button>
                  </DialogFooter>
                </DialogContent>
              </Dialog>
            </div>

            <div className="flex w-full space-x-1">
              <Select
                value={selectedPreset?.id}
                onValueChange={(id) => {
                  const preset = presets.find((p) => p.id === id);
                  if (preset) {
                    setSelectedPreset(preset);
                    setIsSelectedPresetDialogOpen(true);
                  }
                }}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="Select a Preset" />
                </SelectTrigger>
                <SelectContent position="popper">
                  <SelectGroup>
                    <SelectLabel>Preset</SelectLabel>
                    {presets.map((preset) => (
                      <SelectItem key={preset.id} value={preset.id}>
                        {preset.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>

              <Dialog open={isPresetDialogOpen} onOpenChange={setIsPresetDialogOpen}>
                <DialogTrigger asChild>
                  <Button variant="outline" size="icon" disabled={!selectedGame}>
                    <Plus className="size-4" />
                  </Button>
                </DialogTrigger>
                <DialogContent className="w-100">
                  <DialogHeader>
                    <DialogTitle>새 프리셋 생성</DialogTitle>
                  </DialogHeader>
                  <div>
                    <Input
                      placeholder="프리셋 이름"
                      value={newPresetName}
                      onChange={(e) => setNewPresetName(e.target.value)}
                    />
                  </div>
                  <DialogFooter>
                    <DialogClose asChild>
                      <Button variant="outline">취소</Button>
                    </DialogClose>
                    <Button onClick={() => createPresetMutation.mutate()}>프리셋 생성</Button>
                  </DialogFooter>
                </DialogContent>
              </Dialog>
            </div>
          </div>
        </div>

        <div className="flex-1 flex flex-col overflow-hidden relative">
          <ContentHeader
            groupName={selectedGroup || ""}
            groupPath={groups.find((g) => g.name === selectedGroup)?.path}
          />

          <ScrollArea className="flex-1 overflow-y-auto">
            <div className="">
              <div className="gap-3 p-3 grid grid-cols-1 min-[1000px]:grid-cols-2 min-[1500px]:grid-cols-3 min-[2000px]:grid-cols-4 min-[2500px]:grid-cols-5 min-[3000px]:grid-cols-6 min-[3500px]:grid-cols-7">
                {isGroupsLoading
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
                  : currentMods.map((mod) => (
                      <ModCard
                        key={mod.path}
                        mod={mod}
                        onToggle={(m) => toggleModMutation.mutate(m)}
                        onToggleKeyUpdate={(modPath, iniFileName, sectionName, variable, value) =>
                          updateToggleKeyMutation.mutate({
                            modPath,
                            iniFileName,
                            sectionName,
                            variable,
                            value,
                          })
                        }
                      />
                    ))}
              </div>
            </div>
          </ScrollArea>

          {downloadMode && (
            <DownloadConfirmationOverlay
              selectedPath={groups.find((g) => g.name === selectedGroup)?.path || null}
              selectedGroupName={selectedGroup}
              suggestedName={downloadMode.suggestedName}
              onConfirm={handleDownloadConfirm}
              onCancel={handleDownloadCancel}
            />
          )}
        </div>
      </div>

      <Dialog open={isSelectedPresetDialogOpen} onOpenChange={setIsSelectedPresetDialogOpen}>
        <DialogContent className="w-100">
          <DialogHeader>
            <DialogTitle>{selectedPreset?.name}</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">
              이 프리셋에는 {selectedPreset?.mods.length || 0}개의 모드가 저장되어 있습니다.
            </p>
          </div>
          <DialogFooter className="flex justify-between">
            <Button
              variant="destructive"
              onClick={() => selectedPreset && deletePresetMutation.mutate(selectedPreset.id)}
            >
              삭제
            </Button>
            <div className="flex gap-2">
              <DialogClose asChild>
                <Button variant="outline">취소</Button>
              </DialogClose>
              <Button
                onClick={() => selectedPreset && applyPresetMutation.mutate(selectedPreset.id)}
              >
                적용
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
