import { CharacterSidebar } from "@renderer/components/mod/character-sidebar";
import { ContentHeader } from "@renderer/components/mod/content-header";
import { DownloadConfirmationOverlay } from "@renderer/components/download-confirmation-overlay";
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
import { useRouteContext } from "@tanstack/react-router";

export default function ModSidebar() {
  const { queryClient } = useRouteContext({ from: "/mod/" });

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

  const handleFilesDrop = useModDragDrop(
    selectedGroupData?.path,
    queryClient,
    selectedGame || "",
  ).handleFilesDrop;

  const handleBrowseFolder = async () => {
    const path = await window.api.invoke("mod:pickFolder");
    if (path) {
      setNewGamePath(path);
    }
  };

  const { addGameMutation } = useGameMutations();

  return (
    <div className="border-r h-full flex flex-col w-64 z-20">
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
  );
}
