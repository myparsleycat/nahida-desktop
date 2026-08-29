import { Mod } from "@bindings/mod";
import { XXMI } from "@bindings/xxmi";
import { Button } from "@renderer/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@renderer/components/ui/select";
import { useGimiDCRLaunch } from "@renderer/hooks/use-gimi-dcr-launch";
import { useEnabledImporters, usePresets } from "@renderer/hooks/use-mod-data";
import { useModStore } from "@renderer/store/mod";
import { isNteImporter } from "@shared/mod";
import type { GameConfig } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useQuery } from "@tanstack/react-query";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { PencilIcon, PlayIcon } from "lucide-react";
import { memo, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { AddGameDialog } from "./add-game-dialog";
import { CreatePresetDialog } from "./create-preset-dialog";
import { EditGameDialog, openEditGameDialog } from "./edit-game-dialog";
import { NteLaunchDialog } from "./nte-launch-dialog";

interface GamePresetSelectorProps {
  games: GameConfig[];
  onDeleteGameClick: (game: string) => void;
  onPickFolder: () => Promise<string | null>;
  isAddingGame: boolean;
  isUpdatingGame: boolean;
  onAddGame: (
    name: string,
    path: string,
    importer: string | null,
    linkedModFolderPath?: string | null,
    gameInstallPath?: string | null,
    gameExecutablePath?: string | null,
  ) => void;
  onUpdateGame: (
    game: string,
    updates: {
      modFolderPath: string;
      importer: string | null;
      linkedModFolderPath: string | null;
      gameInstallPath: string | null;
      gameExecutablePath: string | null;
    },
  ) => void;
  onReorderGames: (games: string[]) => void;
}

export const GamePresetSelector = memo(function GamePresetSelector({
  games,
  onDeleteGameClick,
  onPickFolder,
  isAddingGame,
  isUpdatingGame,
  onAddGame,
  onUpdateGame,
  onReorderGames,
}: GamePresetSelectorProps) {
  const { t } = useTranslation();
  const location = useLocation();
  const navi = useNavigate();
  const selectedGame = useModStore((s) => s.selectedGame);
  const setSelectedGame = useModStore((s) => s.setSelectedGame);
  const markUserSelectedDuringDownload = useModStore((s) => s.markUserSelectedDuringDownload);
  const selectedPreset = useModStore((s) => s.selectedPreset);
  const setSelectedPreset = useModStore((s) => s.setSelectedPreset);
  const setIsSelectedPresetDialogOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);
  const isSelectedPresetDialogOpen = useModStore((s) => s.isSelectedPresetDialogOpen);
  const setEditingGame = useModStore((s) => s.setEditingGame);
  const setIsEditGameDialogOpen = useModStore((s) => s.setIsEditGameDialogOpen);
  const setIsNteLaunchDialogOpen = useModStore((s) => s.setIsNteLaunchDialogOpen);

  const { data: presets = [] } = usePresets(selectedGame);
  const { data: enabledImporters = [] } = useEnabledImporters();
  const { startImporter, gimiDCRDialog } = useGimiDCRLaunch();
  const { data: xxmiData } = useQuery({
    queryKey: ["xxmi:getXXMIData"],
    queryFn: () => XXMI.GetXXMIData(),
  });
  const selectedGameConfig = games.find((game) => game.game === selectedGame);
  const selectedImporter = selectedGameConfig?.importer ?? null;

  useEffect(() => {
    if (!isSelectedPresetDialogOpen) {
      setSelectedPreset(null);
    }
  }, [isSelectedPresetDialogOpen, setSelectedPreset]);

  const handleGameSelect = async (game: string) => {
    markUserSelectedDuringDownload();
    setSelectedGame(game);
    await Mod.SetLastGame(game);
  };

  const handleEditGameClick = (game: GameConfig) => {
    openEditGameDialog(game, {
      setEditingGame,
      setIsEditGameDialogOpen,
    });
  };

  const handlePlayClick = async () => {
    if (isNteImporter(selectedImporter)) {
      if (selectedGameConfig?.nteLauncherPath) {
        await Mod.StartNteLauncher(selectedGame).catch((error) => {
          const errorMessage = toErrorMessage(error);

          if (errorMessage.includes("NTE_LAUNCHER_PATH_NOT_FOUND")) {
            toast.error(t("page.mod.hooks.use-mod-mutations.start-nte-launcher.not-found"));
            setIsNteLaunchDialogOpen(true);
            return;
          }

          toast.error(
            errorMessage || t("page.mod.hooks.use-mod-mutations.start-nte-launcher.failed"),
          );
        });
        return;
      }

      setIsNteLaunchDialogOpen(true);
      return;
    }

    if (!xxmiData?.xxmiPath) {
      toast.info(t("page.mod.dialog.add-game.xxmi_path_required"));
      void navi({ to: "/setting/xxmi" });
      return;
    }

    if (!selectedImporter) {
      toast.warning(t("page.mod.play.no_importer"));
      return;
    }

    await startImporter(selectedImporter).catch((err) => {
      toast.error(toErrorMessage(err));
    });
  };

  return (
    <div className="flex w-full flex-col items-center justify-center space-y-3 border-t p-2">
      {location.pathname.startsWith("/mod") && (
        <div className="flex w-full space-x-1">
          {games.length > 0 && (
            <Button variant="outline" size="icon" onClickPromise={handlePlayClick}>
              <PlayIcon className="size-4" />
            </Button>
          )}
          <Select
            value={selectedGame || ""}
            onValueChange={(v) => {
              if (v === null) return;
              void handleGameSelect(v);
            }}
          >
            <SelectTrigger className="w-full" disabled={games.length < 1}>
              <SelectValue placeholder={games.length > 0 ? "Select a Game" : "No games"} />
            </SelectTrigger>
            <SelectContent finalFocus={false} aria-describedby={undefined}>
              <SelectGroup>
                <SelectLabel>{games.length > 0 ? "Games" : "No games"}</SelectLabel>
                {games.map((game, idx) => (
                  <div
                    key={idx.toString()}
                    className="group flex w-full items-center justify-between"
                  >
                    <SelectItem key={game.game} value={game.game}>
                      {game.game}
                    </SelectItem>

                    <div className="w-0 overflow-hidden transition-all group-hover:w-10">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="opacity-0 transition-opacity group-hover:opacity-100"
                        onClick={() => {
                          handleEditGameClick(game);
                        }}
                      >
                        <PencilIcon className="size-4" />
                      </Button>
                    </div>
                  </div>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>

          <AddGameDialog
            isAddingGame={isAddingGame}
            onPickFolder={onPickFolder}
            onAddGame={onAddGame}
          />
        </div>
      )}

      <div className="flex w-full space-x-1">
        <Select
          value={selectedPreset?.id || ""}
          items={presets.map((preset) => ({
            value: preset.id,
            label: preset.isLegacy
              ? `${preset.name} (${t("page.mod.dialog.preset-management.legacy-badge")})`
              : preset.name,
          }))}
          onValueChange={(id) => {
            if (!id) return;
            const preset = presets.find((p) => p.id === id);
            if (preset) {
              setSelectedPreset(preset);
              setIsSelectedPresetDialogOpen(true);
            }
          }}
        >
          <SelectTrigger className="w-full" disabled={presets.length < 1}>
            <SelectValue placeholder={presets.length > 0 ? "Select a preset" : "No presets"} />
          </SelectTrigger>
          <SelectContent finalFocus={false} aria-describedby={undefined}>
            <SelectGroup>
              <SelectLabel>{presets.length > 0 ? "Presets" : "No presets"}</SelectLabel>
              {presets.map((preset) => (
                <SelectItem key={preset.id} value={preset.id}>
                  {preset.isLegacy
                    ? `${preset.name} (${t("page.mod.dialog.preset-management.legacy-badge")})`
                    : preset.name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <CreatePresetDialog disabled={!selectedGame} />
      </div>

      <EditGameDialog
        games={games}
        enabledImporters={enabledImporters ?? []}
        isUpdatingGame={isUpdatingGame}
        onPickFolder={onPickFolder}
        onUpdateGame={onUpdateGame}
        onDeleteGameClick={onDeleteGameClick}
        onReorderGames={onReorderGames}
      />

      <NteLaunchDialog />
      {gimiDCRDialog}
    </div>
  );
});
