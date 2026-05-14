import { DownloadConfirmationOverlay } from "@renderer/components/download-confirmation-overlay";
import { ContentHeader } from "@renderer/components/mod/content-header";
import { CustomDownloadDialog } from "@renderer/components/mod/custom-download-dialog";
import { DeleteGameDialog } from "@renderer/components/mod/delete-game-dialog";
import { ModGrid } from "@renderer/components/mod/mod-grid";
import { ModList } from "@renderer/components/mod/mod-list";
import { PresetManagementDialog } from "@renderer/components/mod/preset-management-dialog";
import ModSidebar from "@renderer/components/mod/sidebar";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Label } from "@renderer/components/ui/label";
import { WindowsOnlyRoute } from "@renderer/components/windows-only-route";
import { useCharacters, useGames } from "@renderer/hooks/use-mod-data";
import { useModDragDrop } from "@renderer/hooks/use-mod-drag-drop";
import {
  useDownloadCompletionHandler,
  useModRefreshOnFocus,
  useModWatcherEvents,
} from "@renderer/hooks/use-mod-events";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { modStore, useModStore } from "@renderer/store/mod";
import type { ResolvedArchiveExtractPathMode } from "@shared/mod";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/mod/")({
  component: RouteComponent,
});

function RouteComponent() {
  return (
    <WindowsOnlyRoute fallbackTo="/transfer">
      <ModRouteContent />
    </WindowsOnlyRoute>
  );
}

function ModRouteContent() {
  const { t } = useTranslation();
  const { Titlebar } = useTitlebar();
  const { queryClient } = Route.useRouteContext();

  const selectedGame = useModStore((s) => s.selectedGame);
  const setSelectedGame = useModStore((s) => s.setSelectedGame);
  const selectedGroupName = useModStore((s) => s.selectedGroup?.name);
  const selectedGroupPath = useModStore((s) => s.selectedGroup?.path);
  const setSelectedGroup = useModStore((s) => s.setSelectedGroup);
  const isCustomDownloadDialogOpen = useModStore((s) => s.isCustomDownloadDialogOpen);
  const setIsCustomDownloadDialogOpen = useModStore((s) => s.setIsCustomDownloadDialogOpen);
  const downloadMode = useModStore((s) => s.downloadMode);
  const archiveExtractPrompt = useModStore((s) => s.archiveExtractPrompt);
  const setArchiveExtractPrompt = useModStore((s) => s.setArchiveExtractPrompt);
  const viewMode = useModStore((s) => s.viewMode);

  const { data: games = [] } = useGames();
  const { data: characters = [] } = useCharacters(selectedGame);

  useModRefreshOnFocus(selectedGame, queryClient);
  useDownloadCompletionHandler(selectedGame, selectedGroupPath, queryClient);
  useModWatcherEvents(selectedGame, selectedGroupPath, queryClient);

  const {
    isDragging,
    handleDragEnter,
    handleDragLeave,
    handleDragOver,
    handleDrop,
    archiveExtractDialogFileName,
    confirmArchiveExtractDialog,
    keepArchiveRootDialog,
    closeArchiveExtractDialog,
  } = useModDragDrop(selectedGroupPath, queryClient, selectedGame || "");

  const initExpandedGroups = useModStore((s) => s.initExpandedGroups);

  const resolveDownloadArchiveExtractPrompt = async (
    requestId: string,
    mode: ResolvedArchiveExtractPathMode | null,
  ) => {
    await window.api.invoke("mod:resolveDownloadArchiveExtractPrompt", requestId, mode);
  };

  const clearArchiveExtractPromptIfCurrent = (requestId: string) => {
    if (modStore.getState().archiveExtractPrompt?.requestId === requestId) {
      setArchiveExtractPrompt(null);
    }
  };

  const fileNameForArchiveExtractDialog =
    archiveExtractPrompt?.fileName ?? archiveExtractDialogFileName;

  const isInitialized = useRef(false);
  useEffect(() => {
    const initGame = async () => {
      try {
        initExpandedGroups();

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
    if (isInitialized.current) {
      if (games.length > 0 && !games.find((g) => g.game === selectedGame)) {
        const nextGame = games[0].game;
        setSelectedGame(nextGame);
        window.api.invoke("mod:setLastGame", nextGame);
      } else if (games.length === 0 && selectedGame !== "") {
        setSelectedGame("");
        window.api.invoke("mod:setLastGame", "");
      }
    }
  }, [games, selectedGame, setSelectedGame]);

  useEffect(() => {
    if (characters.length > 0) {
      const isSelectedInTopLevel = selectedGroupPath
        ? characters.some((g) => g.path === selectedGroupPath)
        : false;
      const isSelectedSubOfTopLevel = selectedGroupPath
        ? characters.some(
            (g) =>
              selectedGroupPath.startsWith(`${g.path}\\`) ||
              selectedGroupPath.startsWith(`${g.path}/`),
          )
        : false;

      if (selectedGroupPath && !isSelectedInTopLevel && !isSelectedSubOfTopLevel) {
        setSelectedGroup(null);
      }
    } else {
      setSelectedGroup(null);
    }
  }, [characters, selectedGroupPath, setSelectedGroup]);

  useEffect(() => {
    if (selectedGame) {
      window.api.invoke("mod:watchGame", selectedGame);
    }
  }, [selectedGame]);

  useEffect(() => {
    if (selectedGroupPath) {
      window.api.invoke("mod:watchCharacter", selectedGroupPath);
    }
  }, [selectedGroupPath]);

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
          {selectedGroupPath && <ContentHeader />}

          <div className="relative flex flex-1 min-h-0 flex-col overflow-hidden">
            {viewMode === "grid" ? (
              <ModGrid isDragging={isDragging} />
            ) : (
              <ModList isDragging={isDragging} />
            )}

            {isDragging && (
              <div className="absolute flex-1 h-full inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm border-2 border-dashed border-primary">
                <div className="text-center">
                  <p className="text-2xl font-bold">
                    {t("page.mod.dad_section.title", { name: selectedGroupName })}
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
      </div>

      <PresetManagementDialog />

      <DeleteGameDialog />

      <CustomDownloadDialog
        open={isCustomDownloadDialogOpen}
        onOpenChange={setIsCustomDownloadDialogOpen}
        groupName={selectedGroupName}
        groupPath={selectedGroupPath}
      />

      <AlertDialog
        open={fileNameForArchiveExtractDialog !== null}
        onOpenChange={(open) => {
          if (!open) {
            if (!archiveExtractPrompt) {
              closeArchiveExtractDialog();
            }
          }
        }}
      >
        <AlertDialogContent
          onEscapeKeyDown={(event) => {
            if (archiveExtractPrompt) {
              const { requestId } = archiveExtractPrompt;
              event.preventDefault();
              void resolveDownloadArchiveExtractPrompt(requestId, null).finally(() => {
                clearArchiveExtractPromptIfCurrent(requestId);
              });
              return;
            }
            closeArchiveExtractDialog();
          }}
        >
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.extract_archive_path.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.extract_archive_path.description", {
                fileName: fileNameForArchiveExtractDialog ?? "",
              })}
            </AlertDialogDescription>
            <div className="mt-2 text-left text-sm w-full space-y-2">
              <div className="space-y-1 rounded-md border bg-muted/30 p-3">
                <Label>{t("page.mod.dialog.extract_archive_path.flatten_single_root")}</Label>
                <p>{t("page.mod.dialog.extract_archive_path.flatten_single_root_example")}</p>
              </div>

              <div className="space-y-1 rounded-md border bg-muted/30 p-3">
                <Label>{t("page.mod.dialog.extract_archive_path.keep_archive_root")}</Label>
                <p>{t("page.mod.dialog.extract_archive_path.keep_archive_root_example")}</p>
              </div>
            </div>
          </AlertDialogHeader>
          <AlertDialogFooter className="grid grid-cols-1 gap-2 sm:grid-cols-3">
            <AlertDialogCancel
              onClick={(event) => {
                if (archiveExtractPrompt) {
                  const { requestId } = archiveExtractPrompt;
                  event.preventDefault();
                  void resolveDownloadArchiveExtractPrompt(requestId, null).finally(() => {
                    clearArchiveExtractPromptIfCurrent(requestId);
                  });
                  return;
                }
                closeArchiveExtractDialog();
              }}
            >
              {t("page.mod.dialog.extract_archive_path.cancel")}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(event) => {
                if (archiveExtractPrompt) {
                  const { requestId } = archiveExtractPrompt;
                  event.preventDefault();
                  void resolveDownloadArchiveExtractPrompt(
                    requestId,
                    "flatten_single_root",
                  ).finally(() => {
                    clearArchiveExtractPromptIfCurrent(requestId);
                  });
                  return;
                }
                confirmArchiveExtractDialog();
              }}
            >
              {t("page.mod.dialog.extract_archive_path.flatten_single_root")}
            </AlertDialogAction>
            <AlertDialogAction
              onClick={(event) => {
                if (archiveExtractPrompt) {
                  const { requestId } = archiveExtractPrompt;
                  event.preventDefault();
                  void resolveDownloadArchiveExtractPrompt(requestId, "keep_archive_root").finally(
                    () => {
                      clearArchiveExtractPromptIfCurrent(requestId);
                    },
                  );
                  return;
                }
                keepArchiveRootDialog();
              }}
            >
              {t("page.mod.dialog.extract_archive_path.keep_archive_root")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
