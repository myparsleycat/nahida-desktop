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
import { useGames, useModGroups, usePresets } from "@renderer/hooks/use-mod-data";
import {
  useGameMutations,
  useModMutations,
  usePresetMutations,
} from "@renderer/hooks/use-mod-mutations";
import { useFilteredMods } from "@renderer/hooks/use-filtered-mods";
import { useModRefreshOnFocus, useDownloadCompletionHandler } from "@renderer/hooks/use-mod-events";
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

  const [isDeleteGameDialogOpen, setIsDeleteGameDialogOpen] = useState(false);

  const { data: games = [] } = useGames();
  const { data: groups = [], isLoading: isGroupsLoading } = useModGroups(selectedGame);
  const { data: presets = [] } = usePresets(selectedGame);

  const { addGameMutation, deleteGameMutation } = useGameMutations(
    selectedGame,
    setSelectedGame,
    setNewGameName,
    setNewGamePath,
    setIsAddGameDialogOpen,
  );

  const { toggleModMutation, updateToggleKeyMutation } = useModMutations(
    selectedGame,
    selectedGroup,
    groups,
  );

  const { createPresetMutation, applyPresetMutation, deletePresetMutation } = usePresetMutations(
    selectedGame,
    newPresetName,
    setNewPresetName,
    setIsPresetDialogOpen,
    setIsSelectedPresetDialogOpen,
    setSelectedPreset,
  );

  const currentMods = useFilteredMods(
    groups.find((g) => g.name === selectedGroup)?.mods || [],
    searchQuery,
  );

  useModRefreshOnFocus(selectedGame, queryClient);
  useDownloadCompletionHandler(selectedGame, queryClient);

  const selectedGroupData = groups.find((g) => g.name === selectedGroup);
  const {
    isDragging,
    handleDragEnter,
    handleDragLeave,
    handleDragOver,
    handleDrop,
    handleFilesDrop,
  } = useModDragDrop(selectedGroupData?.path, queryClient, selectedGame || "");

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
    if (groups.length > 0) {
      if (!selectedGroup || !groups.find((g) => g.name === selectedGroup)) {
        setSelectedGroup(groups[0].name);
      }
    } else {
      setSelectedGroup(null);
    }
  }, [groups, selectedGroup, setSelectedGroup]);

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
        "pathSelector:selectModManagerPath",
        downloadMode.downloadId,
        selectedGroupData.path,
      );

      setDownloadMode(null);
    } catch (error) {
      toast.error("경로 선택에 실패했습니다.");
      Logger.error(error, "Route:Mod:handleDownloadConfirm");
    }
  };

  const handleDownloadCancel = async () => {
    if (!downloadMode) return;

    try {
      await window.api.invoke("pathSelector:cancel", downloadMode.downloadId);
      setDownloadMode(null);
    } catch (error) {
      Logger.error(error, "Route:Mod:handleDownloadCancel");
    }
  };

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
    <>
      <Titlebar title={{ text: "모드", position: "center" }} />

      <div className="flex-1 flex overflow-hidden h-full">
        <div className="border-r h-full flex flex-col w-64">
          <div className="flex-1 overflow-y-auto h-full">
            <CharacterSidebar
              groups={groups}
              isLoading={isGroupsLoading}
              onModDrop={handleFilesDrop}
            />
          </div>

          <GamePresetSelector
            games={games}
            selectedGame={selectedGame}
            onGameSelect={handleGameSelect}
            onDeleteGameClick={() => setIsDeleteGameDialogOpen(true)}
            isAddGameDialogOpen={isAddGameDialogOpen}
            onAddGameDialogOpenChange={setIsAddGameDialogOpen}
            newGameName={newGameName}
            newGamePath={newGamePath}
            onNewGameNameChange={setNewGameName}
            onNewGamePathChange={setNewGamePath}
            onBrowseFolder={handleBrowseFolder}
            onAddGame={(name, path) => addGameMutation.mutate({ name, path })}
            presets={presets}
            selectedPreset={selectedPreset}
            onPresetSelect={(preset) => {
              setSelectedPreset(preset);
              setIsSelectedPresetDialogOpen(true);
            }}
            isPresetDialogOpen={isPresetDialogOpen}
            onPresetDialogOpenChange={setIsPresetDialogOpen}
            newPresetName={newPresetName}
            onNewPresetNameChange={setNewPresetName}
            onCreatePreset={() => createPresetMutation.mutate()}
          />
        </div>

        <div
          className="flex-1 flex flex-col overflow-hidden relative"
          onDragEnter={handleDragEnter}
          onDragLeave={handleDragLeave}
          onDragOver={handleDragOver}
          onDrop={handleDrop}
        >
          <ContentHeader
            groupName={selectedGroup || ""}
            groupPath={groups.find((g) => g.name === selectedGroup)?.path}
          />

          <ModGrid
            mods={currentMods}
            isLoading={isGroupsLoading}
            onToggle={(m) => toggleModMutation.mutate(m)}
            onToggleKeyUpdate={handleToggleKeyUpdate}
            groupPath={selectedGroupData?.path}
            game={selectedGame || ""}
            isDragging={isDragging}
          />

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

      <PresetManagementDialog
        isOpen={isSelectedPresetDialogOpen}
        onOpenChange={setIsSelectedPresetDialogOpen}
        selectedPreset={selectedPreset}
        onApplyPreset={(presetId) => applyPresetMutation.mutate(presetId)}
        onDeletePreset={(presetId) => deletePresetMutation.mutate(presetId)}
      />

      <DeleteGameDialog
        isOpen={isDeleteGameDialogOpen}
        onOpenChange={setIsDeleteGameDialogOpen}
        selectedGame={selectedGame}
        onDeleteGame={(game) => deleteGameMutation.mutate(game)}
      />
    </>
  );
}
