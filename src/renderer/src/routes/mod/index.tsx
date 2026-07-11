import { DownloadConfirmationOverlay } from "@renderer/components/download-confirmation-overlay";
import { ContentHeader } from "@renderer/components/mod/content-header";
import { CustomDownloadDialog } from "@renderer/components/mod/custom-download-dialog";
import { DeleteGameDialog } from "@renderer/components/mod/delete-game-dialog";
import { ModFixRunnerDialogs } from "@renderer/components/mod/mod-fix-runner-dialogs";
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
import { useCharacters, useGames } from "@renderer/hooks/use-mod-data";
import { useModDragDrop } from "@renderer/hooks/use-mod-drag-drop";
import {
  useDownloadCompletionHandler,
  useModRefreshOnFocus,
  useModWatcherEvents,
} from "@renderer/hooks/use-mod-events";
import { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { useSettings } from "@renderer/hooks/use-settings";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { modStore, useModStore } from "@renderer/store/mod";
import { findGameByImporter, type ResolvedArchiveExtractPathMode } from "@shared/mod";
import type { FolderGroup } from "@shared/types";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/mod/")({
  component: RouteComponent,
});

const downloadTargetSettingsConfig = {
  enabled: "mod.autoResolveDownloadTarget",
  sources: "mod.autoResolveDownloadTargetSources",
} as const;

function RouteComponent() {
  return <ModRouteContent />;
}

function getParentGroupPath(groupPath: string) {
  const separatorIndex = Math.max(groupPath.lastIndexOf("\\"), groupPath.lastIndexOf("/"));
  if (separatorIndex < 0) return null;
  return groupPath.slice(0, separatorIndex);
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
  const setExpandedGroup = useModStore((s) => s.setExpandedGroup);
  const isCustomDownloadDialogOpen = useModStore((s) => s.isCustomDownloadDialogOpen);
  const setIsCustomDownloadDialogOpen = useModStore((s) => s.setIsCustomDownloadDialogOpen);
  const downloadMode = useModStore((s) => s.downloadMode);
  const userSelectedDuringDownload = useModStore((s) => s.userSelectedDuringDownload);
  const archiveExtractPrompt = useModStore((s) => s.archiveExtractPrompt);
  const setArchiveExtractPrompt = useModStore((s) => s.setArchiveExtractPrompt);
  const viewMode = useModStore((s) => s.viewMode);

  const runner = useModFixRunner();

  const { data: games = [] } = useGames();
  const { data: characters = [] } = useCharacters(selectedGame);
  const { settings: downloadTargetSettings } = useSettings(downloadTargetSettingsConfig);
  const [pendingDownloadTarget, setPendingDownloadTarget] = useState<{
    downloadId: string;
    game: string;
    group: FolderGroup;
  } | null>(null);
  const resolvedDownloadIdsRef = useRef(new Set<string>());

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
        void initExpandedGroups();
        if (modStore.getState().downloadMode) return;

        const focusedGame = await window.api.invoke("mod:getPreviousFocusedGame");
        if (modStore.getState().downloadMode) return;
        if (focusedGame && games.find((g) => g.game === focusedGame)) {
          setSelectedGame(focusedGame);
          return;
        }

        if (!selectedGame) {
          const lastGame = await window.api.invoke("mod:getLastGame");
          if (modStore.getState().downloadMode) return;
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
      void initGame();
    }
  }, [games, selectedGame, setSelectedGame]);

  useEffect(() => {
    if (isInitialized.current) {
      if (games.length > 0 && !games.find((g) => g.game === selectedGame)) {
        const nextGame = games[0].game;
        setSelectedGame(nextGame);
        void window.api.invoke("mod:setLastGame", nextGame);
      } else if (games.length === 0 && selectedGame !== "") {
        setSelectedGame("");
        void window.api.invoke("mod:setLastGame", "");
      }
    }
  }, [games, selectedGame, setSelectedGame]);

  useEffect(() => {
    const shouldAutoResolve =
      downloadTargetSettings.enabled === true &&
      downloadTargetSettings.sources?.includes(downloadMode?.downloadSource ?? "gamebanana");
    if (
      !shouldAutoResolve ||
      (!downloadMode?.suggestedName &&
        !downloadMode?.downloadTargetName &&
        !downloadMode?.downloadImporterKey)
    ) {
      return;
    }
    if (resolvedDownloadIdsRef.current.has(downloadMode.downloadId)) return;
    if (downloadMode.downloadImporterKey && games.length === 0) return;

    const downloadId = downloadMode.downloadId;
    let active = true;

    const resolveTarget = async () => {
      if (!active || modStore.getState().downloadMode?.downloadId !== downloadId) return;

      const currentDownloadMode = modStore.getState().downloadMode;
      if (!currentDownloadMode) return;

      const gameByImporter = currentDownloadMode.downloadImporterKey
        ? (findGameByImporter(games, currentDownloadMode.downloadImporterKey)?.game ?? null)
        : null;

      if (currentDownloadMode.downloadImporterKey && !gameByImporter) return;

      const primary = currentDownloadMode.downloadTargetName;
      const fallback = currentDownloadMode.suggestedName;

      let result = primary
        ? await window.api.invoke("mod:resolveDownloadTarget", primary, gameByImporter ?? undefined)
        : null;

      const stateAfterPrimary = modStore.getState();
      if (
        !active ||
        stateAfterPrimary.downloadMode?.downloadId !== downloadId ||
        stateAfterPrimary.userSelectedDuringDownload
      ) {
        return;
      }

      if (!result && fallback) {
        result = await window.api.invoke(
          "mod:resolveDownloadTarget",
          fallback,
          gameByImporter ?? undefined,
        );
      }

      const stateAfterResolve = modStore.getState();
      if (
        !active ||
        stateAfterResolve.downloadMode?.downloadId !== downloadId ||
        stateAfterResolve.userSelectedDuringDownload
      ) {
        return;
      }

      if (result) {
        const targetGame = gameByImporter ?? result.game;
        setSelectedGame(targetGame);
        void window.api.invoke("mod:setLastGame", targetGame);
        setPendingDownloadTarget({
          downloadId,
          game: targetGame,
          group: result.group,
        });
        resolvedDownloadIdsRef.current.add(downloadId);
        return;
      }

      if (gameByImporter) {
        setSelectedGame(gameByImporter);
        void window.api.invoke("mod:setLastGame", gameByImporter);
      }

      resolvedDownloadIdsRef.current.add(downloadId);
    };

    void resolveTarget().catch((error) => {
      console.error("Failed to resolve download target", error);
    });

    return () => {
      active = false;
    };
  }, [
    downloadMode?.downloadId,
    downloadMode?.suggestedName,
    downloadMode?.downloadTargetName,
    downloadMode?.downloadImporterKey,
    downloadMode?.downloadSource,
    downloadTargetSettings.enabled,
    downloadTargetSettings.sources,
    games,
    setSelectedGame,
  ]);

  useEffect(() => {
    if (!pendingDownloadTarget) return;
    if (downloadMode?.downloadId !== pendingDownloadTarget.downloadId) {
      setPendingDownloadTarget(null);
      return;
    }
    if (selectedGame !== pendingDownloadTarget.game) return;
    if (userSelectedDuringDownload) {
      setPendingDownloadTarget(null);
      return;
    }

    const isTargetAvailable = characters.some(
      (group) =>
        group.path === pendingDownloadTarget.group.path ||
        pendingDownloadTarget.group.path.startsWith(`${group.path}\\`) ||
        pendingDownloadTarget.group.path.startsWith(`${group.path}/`),
    );
    if (!isTargetAvailable) return;

    const parentGroupPath = getParentGroupPath(pendingDownloadTarget.group.path);
    if (parentGroupPath && characters.some((group) => group.path === parentGroupPath)) {
      setExpandedGroup(parentGroupPath, true);
    }

    setSelectedGroup(pendingDownloadTarget.group);
    setPendingDownloadTarget(null);
  }, [
    characters,
    downloadMode?.downloadId,
    pendingDownloadTarget,
    selectedGame,
    setExpandedGroup,
    setSelectedGroup,
    userSelectedDuringDownload,
  ]);

  useEffect(() => {
    if (pendingDownloadTarget) return;
    if (downloadMode) return;

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
  }, [characters, pendingDownloadTarget, selectedGroupPath, setSelectedGroup, downloadMode]);

  useEffect(() => {
    if (selectedGame) {
      void window.api.invoke("mod:watchGame", selectedGame);
    }
  }, [selectedGame]);

  useEffect(() => {
    if (selectedGroupPath) {
      void window.api.invoke("mod:watchCharacter", selectedGroupPath);
    }
  }, [selectedGroupPath]);

  return (
    <>
      <Titlebar title={{ text: t("page.mod.title"), position: "center" }} />

      <div className="flex h-full flex-1 overflow-hidden">
        <ModSidebar
          showWuwaFixer={runner.showWuwaFixer}
          onOpenWuwaFixer={runner.handleOpenWuwaFixer}
        />

        <div
          className="relative flex flex-1 flex-col overflow-hidden"
          onDragEnter={handleDragEnter}
          onDragLeave={handleDragLeave}
          onDragOver={handleDragOver}
          onDrop={handleDrop}
        >
          {selectedGroupPath && (
            <ContentHeader
              showWuwaFixer={runner.showWuwaFixer}
              handleOpenWuwaFixer={runner.handleOpenWuwaFixer}
              isPreparing={runner.isPreparing}
            />
          )}

          <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
            {viewMode === "grid" ? (
              <ModGrid isDragging={isDragging} />
            ) : (
              <ModList isDragging={isDragging} />
            )}

            {isDragging && (
              <div className="absolute inset-0 z-50 flex h-full flex-1 items-center justify-center border-2 border-dashed border-primary bg-background/80 backdrop-blur-sm">
                <div className="text-center">
                  <p className="text-2xl font-bold">
                    {t("page.mod.dad_section.title", { name: selectedGroupName })}
                  </p>
                  <p className="mt-2 text-sm text-muted-foreground">
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
        onOpenChange={(nextOpen, eventDetails) => {
          if (nextOpen) return;
          if (archiveExtractPrompt) {
            const { requestId } = archiveExtractPrompt;
            eventDetails.cancel();
            void resolveDownloadArchiveExtractPrompt(requestId, null).finally(() => {
              clearArchiveExtractPromptIfCurrent(requestId);
            });
            return;
          }
          closeArchiveExtractDialog();
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.extract_archive_path.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.extract_archive_path.description", {
                fileName: fileNameForArchiveExtractDialog ?? "",
              })}
            </AlertDialogDescription>
            <div className="mt-2 w-full space-y-2 text-left text-sm">
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
      <ModFixRunnerDialogs runner={runner} />
    </>
  );
}
