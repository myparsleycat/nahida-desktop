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
import { usePresets } from "@renderer/hooks/use-mod-data";
import { useModStore } from "@renderer/store/mod";
import type { GameConfig } from "@shared/types.gen";
import { Trash2 } from "lucide-react";
import { memo, useEffect } from "react";
import { AddGameDialog } from "./add-game-dialog";
import { CreatePresetDialog } from "./create-preset-dialog";

interface GamePresetSelectorProps {
  games: GameConfig[];
  onDeleteGameClick: () => void;
  onBrowseFolder: () => void;
  onAddGame: (name: string, path: string) => void;
}

export const GamePresetSelector = memo(function GamePresetSelector({
  games,
  onDeleteGameClick,
  onBrowseFolder,
  onAddGame,
}: GamePresetSelectorProps) {
  const selectedGame = useModStore((s) => s.selectedGame);
  const setSelectedGame = useModStore((s) => s.setSelectedGame);
  const selectedPreset = useModStore((s) => s.selectedPreset);
  const setSelectedPreset = useModStore((s) => s.setSelectedPreset);
  const setIsSelectedPresetDialogOpen = useModStore((s) => s.setIsSelectedPresetDialogOpen);
  const isSelectedPresetDialogOpen = useModStore((s) => s.isSelectedPresetDialogOpen);

  const { data: presets = [] } = usePresets(selectedGame);

  useEffect(() => {
    if (!isSelectedPresetDialogOpen) {
      setSelectedPreset(null);
    }
  }, [isSelectedPresetDialogOpen, setSelectedPreset]);

  const handleGameSelect = async (game: string) => {
    setSelectedGame(game);
    await window.api.invoke("mod:setLastGame", game);
  };

  return (
    <div className="flex flex-col items-center justify-center w-full p-2 border-t space-y-3">
      <div className="flex w-full space-x-1">
        <Select value={selectedGame || ""} onValueChange={handleGameSelect}>
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Select a Game" />
          </SelectTrigger>
          <SelectContent
            position="popper"
            onCloseAutoFocus={(e) => e.preventDefault()}
            aria-describedby={undefined}
          >
            <SelectGroup>
              <SelectLabel>Games</SelectLabel>
              {games.map((game) => (
                <SelectItem key={game.game} value={game.game}>
                  {game.game}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <AddGameDialog onBrowseFolder={onBrowseFolder} onAddGame={onAddGame} />

        <Button variant="outline" size="icon" disabled={!selectedGame} onClick={onDeleteGameClick}>
          <Trash2 className="size-4 text-destructive" />
        </Button>
      </div>

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
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Select a Preset" />
          </SelectTrigger>
          <SelectContent
            position="popper"
            onCloseAutoFocus={(e) => e.preventDefault()}
            aria-describedby={undefined}
          >
            <SelectGroup>
              <SelectLabel>Preset</SelectLabel>
              {presets.map((preset) => (
                <SelectItem key={preset.id} value={preset.id}>
                  {preset.name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <CreatePresetDialog disabled={!selectedGame} />
      </div>
    </div>
  );
});
