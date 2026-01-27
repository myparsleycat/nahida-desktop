import { ContentHeader } from "@renderer/components/mod/content-header";
import { DownloadConfirmationOverlay } from "@renderer/components/download-confirmation-overlay";
import { ModGrid } from "@renderer/components/mod/mod-grid";
import { DeleteGameDialog } from "@renderer/components/mod/delete-game-dialog";
import { PresetManagementDialog } from "@renderer/components/mod/preset-management-dialog";
import { Titlebar } from "@renderer/components/titlebar";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect } from "react";
import { useModStore } from "@renderer/store/mod";
import { useGames, useCharacters } from "@renderer/hooks/use-mod-data";
import {
  useModRefreshOnFocus,
  useDownloadCompletionHandler,
  useModWatcherEvents,
} from "@renderer/hooks/use-mod-events";
import { useModDragDrop } from "@renderer/hooks/use-mod-drag-drop";
import ModSidebar from "@renderer/components/mod/sidebar";

export const Route = createFileRoute("/mod/")({
  component: RouteComponent,
});

function RouteComponent() {
  const { queryClient } = Route.useRouteContext();

  const selectedGame = useModStore((s) => s.selectedGame);
  const setSelectedGame = useModStore((s) => s.setSelectedGame);
  const selectedGroup = useModStore((s) => s.selectedGroup);
  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const downloadMode = useModStore((s) => s.downloadMode);

  const { data: games = [] } = useGames();
  const { data: characters = [] } = useCharacters(selectedGame);
  const selectedGroupData = characters.find((g) => g.name === selectedGroup?.name);

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
      if (!selectedGroup || !characters.find((g) => g.name === selectedGroup.name)) {
        setSelectedGroup(characters[0]);
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

  return (
    <>
      <Titlebar title={{ text: "모드", position: "center" }} />

      <div className="flex-1 flex overflow-hidden h-full">
        <ModSidebar />

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
