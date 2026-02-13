import { CharacterSidebar } from "@renderer/components/mod/character-sidebar";
import { GamePresetSelector } from "@renderer/components/mod/game-preset-selector";
import { useCharacters, useGames } from "@renderer/hooks/use-mod-data";
import { useModDragDrop } from "@renderer/hooks/use-mod-drag-drop";
import { useGameMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import { useRouteContext } from "@tanstack/react-router";
import { useCallback } from "react";

export default function ModSidebar() {
  const { queryClient } = useRouteContext({ from: "__root__" });

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
  const { mutate: addGame } = addGameMutation;

  const handleDeleteGameClick = useCallback(() => {
    setIsDeleteGameDialogOpen(true);
  }, [setIsDeleteGameDialogOpen]);

  const handleAddGame = useCallback(
    (name: string, path: string) => {
      addGame({ name, path });
    },
    [addGame],
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
