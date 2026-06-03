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
import { useEnabledImporters, usePresets } from "@renderer/hooks/use-mod-data";
import { useModStore } from "@renderer/store/mod";
import type { GameConfig } from "@shared/types";
import { useLocation } from "@tanstack/react-router";
import { PencilIcon, PlayIcon } from "lucide-react";
import { memo, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { AddGameDialog } from "./add-game-dialog";
import { CreatePresetDialog } from "./create-preset-dialog";
import { EditGameDialog, openEditGameDialog } from "./edit-game-dialog";

interface GamePresetSelectorProps {
  games: GameConfig[];
  onDeleteGameClick: (game: string) => void;
  onPickFolder: () => Promise<string | null>;
  onAddGame: (name: string, path: string, importer: string | null) => void;
  onUpdateGame: (game: string, updates: { modFolderPath: string; importer: string | null }) => void;
  onReorderGames: (games: string[]) => void;
}

export const GamePresetSelector = memo(function GamePresetSelector({
  games,
  onDeleteGameClick,
  onPickFolder,
  onAddGame,
  onUpdateGame,
  onReorderGames,
}: GamePresetSelectorProps) {
  const { t } = useTranslation();
  const location = useLocation();
  const selectedGame = useModStore((s) => s.selectedGame);
  const setSelectedGame = useModStore((s) => s.setSelectedGame);
  const selectedPreset = useModStore((s) => s.selectedPreset);
  const setSelectedPreset = useModStore((s) => s.setSelectedPreset);
  const setIsSelectedPresetDialogOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);
  const isSelectedPresetDialogOpen = useModStore((s) => s.isSelectedPresetDialogOpen);
  const setEditingGame = useModStore((s) => s.setEditingGame);
  const setIsEditGameDialogOpen = useModStore((s) => s.setIsEditGameDialogOpen);

  const { data: presets = [] } = usePresets(selectedGame);
  const { data: enabledImporters = [] } = useEnabledImporters();
  const selectedGameConfig = games.find((game) => game.game === selectedGame);
  const selectedImporter = selectedGameConfig?.importer ?? null;

  useEffect(() => {
    if (!isSelectedPresetDialogOpen) {
      setSelectedPreset(null);
    }
  }, [isSelectedPresetDialogOpen, setSelectedPreset]);

  const handleGameSelect = async (game: string) => {
    setSelectedGame(game);
    await window.api.invoke("mod:setLastGame", game);
  };

  const handleEditGameClick = (game: GameConfig) => {
    openEditGameDialog(game, {
      setEditingGame,
      setIsEditGameDialogOpen,
    });
  };

  return (
    <div className="flex flex-col items-center justify-center w-full p-2 border-t space-y-3">
      {location.pathname.startsWith("/mod") && (
        <div className="flex w-full space-x-1">
          {selectedImporter && (
            <Button
              variant="outline"
              size="icon"
              onClickPromise={() =>
                window.api.invoke("xxmi:startGame", selectedImporter).catch((err) => {
                  toast.error(err.toString());
                })
              }
            >
              <PlayIcon className="size-4" />
            </Button>
          )}
          <Select value={selectedGame || ""} onValueChange={handleGameSelect}>
            <SelectTrigger className="w-full" disabled={games.length < 1}>
              <SelectValue placeholder={games.length > 0 ? "Select a Game" : "No games"} />
            </SelectTrigger>
            <SelectContent
              position="popper"
              onCloseAutoFocus={(e) => e.preventDefault()}
              aria-describedby={undefined}
            >
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

          <AddGameDialog onPickFolder={onPickFolder} onAddGame={onAddGame} />
        </div>
      )}

      <div className="flex w-full space-x-1">
        <Select
          value={selectedPreset?.id || ""}
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
          <SelectContent
            position="popper"
            onCloseAutoFocus={(e) => e.preventDefault()}
            aria-describedby={undefined}
          >
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
        enabledImporters={enabledImporters}
        onPickFolder={onPickFolder}
        onUpdateGame={onUpdateGame}
        onDeleteGameClick={onDeleteGameClick}
        onReorderGames={onReorderGames}
      />
    </div>
  );
});
