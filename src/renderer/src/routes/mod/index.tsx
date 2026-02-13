import { DownloadConfirmationOverlay } from "@renderer/components/download-confirmation-overlay";
import { ContentHeader } from "@renderer/components/mod/content-header";
import { DeleteGameDialog } from "@renderer/components/mod/delete-game-dialog";
import { ModGrid } from "@renderer/components/mod/mod-grid";
import { PresetManagementDialog } from "@renderer/components/mod/preset-management-dialog";
import ModSidebar from "@renderer/components/mod/sidebar";
import { Titlebar } from "@renderer/components/titlebar";
import { useCharacters, useGames } from "@renderer/hooks/use-mod-data";
import { useModDragDrop } from "@renderer/hooks/use-mod-drag-drop";
import {
  useDownloadCompletionHandler,
  useModRefreshOnFocus,
  useModWatcherEvents,
} from "@renderer/hooks/use-mod-events";
import { useModShortcuts } from "@renderer/hooks/use-mod-shortcuts";
import { useModStore } from "@renderer/store/mod";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/mod/")({
  component: RouteComponent,
});

function RouteComponent() {
  const { t } = useTranslation();
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
  useModShortcuts();

  const { isDragging, handleDragEnter, handleDragLeave, handleDragOver, handleDrop } =
    useModDragDrop(selectedGroupData?.path, queryClient, selectedGame || "");

  const isInitialized = useRef(false);
  useEffect(() => {
    const initGame = async () => {
      try {
        const focusedGame = await window.api.invoke("mod:getPreviousFocusedGame");
        if (focusedGame && games.find((g) => g.game === focusedGame)) {
          setSelectedGame(focusedGame);
          return;
        }

        if (!selectedGame) {
          const lastGame = await window.api.invoke("mod:getLastGame");
          if (lastGame && games.find((g) => g.game === lastGame)) {
            setSelectedGame(lastGame);
          }
        }
      } catch (error) {
        console.error("Failed to initialize game selection", error);
      }
    };

    if (games.length > 0 && !isInitialized.current) {
      isInitialized.current = true;
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
      <Titlebar title={{ text: t("page.mod.title"), position: "center" }} />

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
                <p className="text-2xl font-bold">
                  {t("page.mod.dad_section.title", { name: selectedGroup?.name })}
                </p>
                <p className="text-sm text-muted-foreground mt-2">
                  {t("page.mod.dad_section.description")}
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
