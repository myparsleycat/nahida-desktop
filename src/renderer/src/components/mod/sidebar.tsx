import { CharacterSidebar } from "@renderer/components/mod/character-sidebar";
import { GamePresetSelector } from "@renderer/components/mod/game-preset-selector";
import { useCharacters, useGames } from "@renderer/hooks/use-mod-data";
import { useGameMutations } from "@renderer/hooks/use-mod-mutations";
import { useCharacterSidebarWidthSetting } from "@renderer/hooks/use-settings";
import { setSetting } from "@renderer/lib/settings";
import { useModStore } from "@renderer/store/mod";
import { useCallback, useEffect, useRef, useState } from "react";

const CHARACTER_SIDEBAR_WIDTH_DEFAULT = 256;
const CHARACTER_SIDEBAR_WIDTH_MIN = 220;
const CHARACTER_SIDEBAR_WIDTH_MAX = 480;
const CHARACTER_SIDEBAR_WIDTH_SAVE_DELAY = 300;

function clampCharacterSidebarWidth(width: number) {
  if (!Number.isFinite(width)) {
    return CHARACTER_SIDEBAR_WIDTH_DEFAULT;
  }

  return Math.min(
    CHARACTER_SIDEBAR_WIDTH_MAX,
    Math.max(CHARACTER_SIDEBAR_WIDTH_MIN, Math.round(width)),
  );
}

export default function ModSidebar({
  showWuwaFixer,
  onOpenWuwaFixer,
}: {
  showWuwaFixer: boolean;
  onOpenWuwaFixer: (path: string) => Promise<void>;
}) {
  const selectedGame = useModStore((s) => s.selectedGame);
  const setDeletingGame = useModStore((s) => s.setDeletingGame);
  const setIsDeleteGameDialogOpen = useModStore((s) => s.setIsDeleteGameDialogOpen);

  const { data: games = [] } = useGames();
  const { data: characters = [], isPlaceholderData, isPending } = useCharacters(selectedGame);
  const { data: storedWidth = CHARACTER_SIDEBAR_WIDTH_DEFAULT } = useCharacterSidebarWidthSetting();

  const [sidebarWidth, setSidebarWidth] = useState(CHARACTER_SIDEBAR_WIDTH_DEFAULT);
  const isDraggingRef = useRef(false);
  const dragStateRef = useRef<{ pointerId: number; startX: number; startWidth: number } | null>(
    null,
  );
  const saveTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingWidthRef = useRef<number | null>(null);

  const persistSidebarWidth = useCallback(async (width: number) => {
    try {
      await setSetting("mod.characterSidebarWidth", width);
    } catch (error) {
      console.error("Failed to persist mod sidebar width", error);
    }
  }, []);

  const flushPendingSidebarWidth = useCallback(() => {
    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current);
      saveTimeoutRef.current = null;
    }

    const pendingWidth = pendingWidthRef.current;
    pendingWidthRef.current = null;

    if (pendingWidth !== null) {
      void persistSidebarWidth(pendingWidth);
    }
  }, [persistSidebarWidth]);

  const scheduleSidebarWidthSave = useCallback(
    (width: number) => {
      pendingWidthRef.current = width;

      if (saveTimeoutRef.current) {
        clearTimeout(saveTimeoutRef.current);
      }

      saveTimeoutRef.current = setTimeout(() => {
        const pendingWidthValue = pendingWidthRef.current;
        pendingWidthRef.current = null;
        saveTimeoutRef.current = null;

        if (pendingWidthValue !== null) {
          void persistSidebarWidth(pendingWidthValue);
        }
      }, CHARACTER_SIDEBAR_WIDTH_SAVE_DELAY);
    },
    [persistSidebarWidth],
  );

  const handlePickFolder = useCallback(async () => {
    return await window.api.invoke("mod:pickFolder");
  }, []);

  const { addGameMutation, reorderGamesMutation, updateGameMutation } = useGameMutations();
  const { mutate: addGame } = addGameMutation;
  const { mutate: reorderGames } = reorderGamesMutation;
  const { mutate: updateGame } = updateGameMutation;

  const handleDeleteGameClick = useCallback(
    (game: string) => {
      setDeletingGame(game);
      setIsDeleteGameDialogOpen(true);
    },
    [setDeletingGame, setIsDeleteGameDialogOpen],
  );

  const handleAddGame = useCallback(
    (name: string, path: string, importer: string | null) => {
      addGame({ name, path, importer });
    },
    [addGame],
  );

  const handleUpdateGame = useCallback(
    (game: string, updates: { modFolderPath: string; importer: string | null }) => {
      updateGame({ game, updates });
    },
    [updateGame],
  );

  const handleReorderGames = useCallback(
    (games: string[]) => {
      reorderGames(games);
    },
    [reorderGames],
  );

  useEffect(() => {
    if (!isDraggingRef.current) {
      setSidebarWidth(clampCharacterSidebarWidth(storedWidth));
    }
  }, [storedWidth]);

  useEffect(() => {
    return () => {
      flushPendingSidebarWidth();
      document.body.style.userSelect = "";
      document.body.style.cursor = "";
    };
  }, [flushPendingSidebarWidth]);

  const stopDragging = useCallback(() => {
    if (!isDraggingRef.current) {
      return;
    }

    isDraggingRef.current = false;
    dragStateRef.current = null;
    document.body.style.userSelect = "";
    document.body.style.cursor = "";
    flushPendingSidebarWidth();
  }, [flushPendingSidebarWidth]);

  useEffect(() => {
    const handlePointerMove = (event: PointerEvent) => {
      const dragState = dragStateRef.current;
      if (!dragState || event.pointerId !== dragState.pointerId) {
        return;
      }

      const nextWidth = clampCharacterSidebarWidth(
        dragState.startWidth + (event.clientX - dragState.startX),
      );

      setSidebarWidth(nextWidth);
      scheduleSidebarWidthSave(nextWidth);
    };

    const handlePointerUp = (event: PointerEvent) => {
      const dragState = dragStateRef.current;
      if (!dragState || event.pointerId !== dragState.pointerId) {
        return;
      }

      stopDragging();
    };

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);
    window.addEventListener("pointercancel", handlePointerUp);
    window.addEventListener("blur", stopDragging);

    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
      window.removeEventListener("blur", stopDragging);
    };
  }, [scheduleSidebarWidthSave, stopDragging]);

  const handleResizePointerDown = useCallback(
    (event: React.PointerEvent<HTMLDivElement>) => {
      if (event.button !== 0) {
        return;
      }

      const nextWidth = clampCharacterSidebarWidth(sidebarWidth);
      isDraggingRef.current = true;
      dragStateRef.current = {
        pointerId: event.pointerId,
        startX: event.clientX,
        startWidth: nextWidth,
      };
      setSidebarWidth(nextWidth);
      document.body.style.userSelect = "none";
      document.body.style.cursor = "col-resize";
      event.preventDefault();
    },
    [sidebarWidth],
  );

  return (
    <div
      className="relative z-20 flex h-full shrink-0 flex-col border-r"
      style={{
        width: `${sidebarWidth}px`,
        minWidth: `${sidebarWidth}px`,
        maxWidth: `${sidebarWidth}px`,
      }}
    >
      <div className="flex-1 overflow-y-auto h-full">
        <CharacterSidebar
          groups={characters}
          isLoading={isPending || isPlaceholderData}
          showWuwaFixer={showWuwaFixer}
          onOpenWuwaFixer={onOpenWuwaFixer}
        />
      </div>

      <GamePresetSelector
        games={games}
        onDeleteGameClick={handleDeleteGameClick}
        onPickFolder={handlePickFolder}
        onAddGame={handleAddGame}
        onUpdateGame={handleUpdateGame}
        onReorderGames={handleReorderGames}
      />

      <div
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize mod sidebar"
        className="group absolute inset-y-0 -right-1 z-30 flex w-2 cursor-col-resize touch-none items-center justify-center"
        onPointerDown={handleResizePointerDown}
      >
        <div className="h-full w-px bg-border/60 transition-colors group-hover:bg-primary/70" />
      </div>
    </div>
  );
}
