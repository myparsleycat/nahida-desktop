import { CharacterSidebar } from "@renderer/components/mod/character-sidebar";
import { ContentHeader } from "@renderer/components/mod/content-header";
import { DownloadConfirmationOverlay } from "@renderer/components/mod/download-confirmation-overlay";
import { GamePresetSelector } from "@renderer/components/mod/game-preset-selector";
import { ModGrid } from "@renderer/components/mod/mod-grid";
import { DeleteGameDialog } from "@renderer/components/mod/delete-game-dialog";
import { PresetManagementDialog } from "@renderer/components/mod/preset-management-dialog";
import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { toast } from "sonner";
import { Logger } from "@renderer/lib/logger";

import { useModStore } from "@renderer/store/mod";
import { useGames, useCharacters, useModGroup, usePresets } from "@renderer/hooks/use-mod-data";
import {
  useGameMutations,
  useModMutations,
  usePresetMutations,
} from "@renderer/hooks/use-mod-mutations";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import {
  useModRefreshOnFocus,
  useDownloadCompletionHandler,
  useModWatcherEvents,
} from "@renderer/hooks/use-mod-events";
import { useModDragDrop } from "@renderer/hooks/use-mod-drag-drop";

export const Route = createFileRoute("/mod/")({
  component: RouteComponent,
});

function RouteComponent() {
  const { queryClient } = Route.useRouteContext();

  const selectedGame = useModStore((s) => s.selectedGame);
  const setSelectedGame = useModStore((s) => s.setSelectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const setNewGamePath = useModStore((s) => s.setNewGamePath);
  const downloadMode = useModStore((s) => s.downloadMode);
  const setIsDeleteGameDialogOpen = useModStore((s) => s.setIsDeleteGameDialogOpen);

  const { data: games = [] } = useGames();
  const { data: characters = [] } = useCharacters(selectedGame);
  const selectedGroupData = characters.find((g) => g.name === selectedGroup);

  const { addGameMutation } = useGameMutations();

  const handleFilesDrop = useModDragDrop(
    selectedGroupData?.path,
    queryClient,
    selectedGame || "",
  ).handleFilesDrop;

  useModRefreshOnFocus(selectedGame, queryClient);
  useDownloadCompletionHandler(selectedGame, queryClient);
  useModWatcherEvents(selectedGame, selectedGroupData?.path, queryClient);

  const { isDragging, handleDragEnter, handleDragLeave, handleDragOver, handleDrop } =
    useModDragDrop(selectedGroupData?.path, queryClient, selectedGame || "");

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
  }, [games, selectedGame, setSelectedGame]);

  useEffect(() => {
    if (characters.length > 0) {
      if (!selectedGroup || !characters.find((g) => g.name === selectedGroup)) {
        setSelectedGroup(characters[0].name);
      }
    } else {
      setSelectedGroup(null);
    }
  }, [characters, selectedGroup, setSelectedGroup]);

  useEffect(() => {
    if (selectedGame) {
      window.api.invoke("mod:watchGame", selectedGame);
    }
  }, [selectedGame]);

  useEffect(() => {
    if (selectedGroupData?.path) {
      window.api.invoke("mod:watchCharacter", selectedGroupData.path);
    }
  }, [selectedGroupData?.path]);

  const handleBrowseFolder = async () => {
    const path = await window.api.invoke("mod:pickFolder");
    if (path) {
      setNewGamePath(path);
    }
  };

  return (
    <>
      <Titlebar title={{ text: "모드", position: "center" }} />

      <div className="flex-1 flex overflow-hidden h-full">
        <div className="border-r h-full flex flex-col w-64">
          <div className="flex-1 overflow-y-auto h-full">
            <CharacterSidebar
              groups={characters}
              isLoading={characters.length === 0 && !!selectedGame}
              onModDrop={handleFilesDrop}
            />
          </div>

          <GamePresetSelector
            games={games}
            onDeleteGameClick={() => setIsDeleteGameDialogOpen(true)}
            onBrowseFolder={handleBrowseFolder}
            onAddGame={(name, path) => addGameMutation.mutate({ name, path })}
          />
        </div>

        <div
          className="flex-1 flex flex-col overflow-hidden relative"
          onDragEnter={handleDragEnter}
          onDragLeave={handleDragLeave}
          onDragOver={handleDragOver}
          onDrop={handleDrop}
        >
          <ContentHeader />

          <ModGrid isDragging={isDragging} />

          {isDragging && (
            <div className="absolute flex-1 h-full inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm border-2 border-dashed border-primary">
              <div className="text-center">
                <p className="text-2xl font-bold">모드 그리드에 드롭</p>
                <p className="text-sm text-muted-foreground mt-2">
                  압축 파일은 자동으로 압축 해제됩니다
                </p>
              </div>
            </div>
          )}

          {downloadMode && <DownloadConfirmationOverlay />}
        </div>
      </div>

      <PresetManagementDialog />

      <DeleteGameDialog />
    </>
  );
}
