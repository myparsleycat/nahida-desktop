import { CharacterSidebar } from "@renderer/components/mod/character-sidebar";
import { GamePresetSelector } from "@renderer/components/mod/game-preset-selector";
import { useModStore } from "@renderer/store/mod";
import { useGames, useCharacters } from "@renderer/hooks/use-mod-data";
import { useGameMutations } from "@renderer/hooks/use-mod-mutations";
import { useModDragDrop } from "@renderer/hooks/use-mod-drag-drop";
import { useRouteContext } from "@tanstack/react-router";
import { useCallback } from "react";

export default function ModSidebar() {
  const { queryClient } = useRouteContext({ from: "/mod/" });

  const selectedGame = useModStore((s) => s.selectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setNewGamePath = useModStore((s) => s.setNewGamePath);
  const setIsDeleteGameDialogOpen = useModStore((s) => s.setIsDeleteGameDialogOpen);

  const { data: games = [] } = useGames();
  const { data: characters = [], isPlaceholderData, isPending } = useCharacters(selectedGame);

  const { handleFilesDrop } = useModDragDrop(selectedGroup?.path, queryClient, selectedGame || "");

  const handleBrowseFolder = useCallback(async () => {
    const path = await window.api.invoke("mod:pickFolder");
    if (path) {
      setNewGamePath(path);
    }
  }, [setNewGamePath]);

  const { addGameMutation } = useGameMutations();

  const handleDeleteGameClick = useCallback(() => {
    setIsDeleteGameDialogOpen(true);
  }, [setIsDeleteGameDialogOpen]);

  const handleAddGame = useCallback(
    (name: string, path: string) => {
      addGameMutation.mutate({ name, path });
    },
    [addGameMutation],
  );

  return (
    <div className="border-r h-full flex flex-col w-64 z-20">
      <div className="flex-1 overflow-y-auto h-full">
        <CharacterSidebar
          groups={characters}
          isLoading={isPending || isPlaceholderData}
          onModDrop={handleFilesDrop}
        />
      </div>

      <GamePresetSelector
        games={games}
        onDeleteGameClick={handleDeleteGameClick}
        onBrowseFolder={handleBrowseFolder}
        onAddGame={handleAddGame}
      />
    </div>
  );
}
