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
import { Trash2 } from "lucide-react";
import type { GameConfig, Preset } from "@shared/types";
import { AddGameDialog } from "./add-game-dialog";
import { CreatePresetDialog } from "./create-preset-dialog";

interface GamePresetSelectorProps {
  games: GameConfig[];
  selectedGame: string | null;
  onGameSelect: (game: string) => void;
  onDeleteGameClick: () => void;

  isAddGameDialogOpen: boolean;
  onAddGameDialogOpenChange: (open: boolean) => void;
  newGameName: string;
  newGamePath: string;
  onNewGameNameChange: (name: string) => void;
  onNewGamePathChange: (path: string) => void;
  onBrowseFolder: () => void;
  onAddGame: (name: string, path: string) => void;

  presets: Preset[];
  selectedPreset: Preset | null;
  onPresetSelect: (preset: Preset) => void;

  isPresetDialogOpen: boolean;
  onPresetDialogOpenChange: (open: boolean) => void;
  newPresetName: string;
  onNewPresetNameChange: (name: string) => void;
  onCreatePreset: () => void;
}

export function GamePresetSelector({
  games,
  selectedGame,
  onGameSelect,
  onDeleteGameClick,
  isAddGameDialogOpen,
  onAddGameDialogOpenChange,
  newGameName,
  newGamePath,
  onNewGameNameChange,
  onNewGamePathChange,
  onBrowseFolder,
  onAddGame,
  presets,
  selectedPreset,
  onPresetSelect,
  isPresetDialogOpen,
  onPresetDialogOpenChange,
  newPresetName,
  onNewPresetNameChange,
  onCreatePreset,
}: GamePresetSelectorProps) {
  return (
    <div className="flex flex-col items-center justify-center w-full p-2 border-t space-y-3">
      {/* Game Selection */}
      <div className="flex w-full space-x-1">
        <Select value={selectedGame || undefined} onValueChange={onGameSelect}>
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Select a Game" />
          </SelectTrigger>
          <SelectContent position="popper">
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

        <AddGameDialog
          isOpen={isAddGameDialogOpen}
          onOpenChange={onAddGameDialogOpenChange}
          newGameName={newGameName}
          newGamePath={newGamePath}
          onGameNameChange={onNewGameNameChange}
          onGamePathChange={onNewGamePathChange}
          onBrowseFolder={onBrowseFolder}
          onAddGame={onAddGame}
        />

        <Button variant="outline" size="icon" disabled={!selectedGame} onClick={onDeleteGameClick}>
          <Trash2 className="size-4 text-destructive" />
        </Button>
      </div>

      {/* Preset Selection */}
      <div className="flex w-full space-x-1">
        <Select
          value={selectedPreset?.id}
          onValueChange={(id) => {
            const preset = presets.find((p) => p.id === id);
            if (preset) {
              onPresetSelect(preset);
            }
          }}
        >
          <SelectTrigger className="w-full">
            <SelectValue placeholder="Select a Preset" />
          </SelectTrigger>
          <SelectContent position="popper">
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

        <CreatePresetDialog
          isOpen={isPresetDialogOpen}
          onOpenChange={onPresetDialogOpenChange}
          newPresetName={newPresetName}
          onPresetNameChange={onNewPresetNameChange}
          onCreatePreset={onCreatePreset}
          disabled={!selectedGame}
        />
      </div>
    </div>
  );
}
