import {
  ModelViewerDialog,
  type ModelViewerDialogSource,
} from "@renderer/components/tools/model-viewer/model-viewer-dialog";
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
import { Button } from "@renderer/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@renderer/components/ui/dialog";
import { Input } from "@renderer/components/ui/input";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import { useGames } from "@renderer/hooks/use-mod-data";
import { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { useModStore } from "@renderer/store/mod";
import type { ModInfo } from "@renderer/types/mod";
import { isNteImporter } from "@shared/mod";
import { toErrorMessage } from "@shared/utils";
import type { QueryClient } from "@tanstack/react-query";
import { useRouteContext } from "@tanstack/react-router";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

import { ModFixRunnerDialogs } from "../components/mod/mod-fix-runner-dialogs";
import { pasteModPreview } from "../components/mod/paste-preview";
import { TextureResizeDialog } from "../components/mod/texture-resize-dialog";

const DISABLED_PREFIX_REGEX = /^disabled[\s_]+/i;

function getRenameDefaultValue(name: string) {
  return name.replace(DISABLED_PREFIX_REGEX, "").trim();
}

async function cleanupModelViewerSource(source: ModelViewerDialogSource | null) {
  if (!source) {
    return;
  }

  try {
    await window.api.invoke(
      "tools:cleanupStaticGlbViewerFile",
      source.mode === "variant-set" ? source.artifactRoot : source.glbPath,
      source.memorySessionId,
    );
  } catch (error) {
    console.warn("Failed to clean up model viewer file", error);
  }
}

function scheduleModelViewerCleanup(source: ModelViewerDialogSource | null) {
  if (!source) {
    return;
  }

  window.setTimeout(() => {
    void cleanupModelViewerSource(source);
  }, 0);
}

export interface ModActionApi {
  overlays: ReactNode;
  runner: ReturnType<typeof useModFixRunner>;
  isNteGame: boolean;
  convertingModelPath: string | null;
  openDeleteMod: (mod: ModInfo) => void;
  openDeletePreview: (mod: ModInfo) => void;
  openModelViewer: (mod: ModInfo) => Promise<void>;
  openPastePreview: (mod: ModInfo) => void;
  openRenameDialog: (mod: ModInfo) => void;
  openTextureResizeDialog: (mod: ModInfo) => void;
  openWuwaFixer: (mod: ModInfo) => Promise<void>;
  markAsManualSubGroup: (mod: ModInfo) => Promise<void>;
  runPreset: (mod: ModInfo, presetId: string) => Promise<void>;
  runTool: (mod: ModInfo, toolId: string) => Promise<void>;
}

function invalidateModGroup(queryClient: QueryClient, groupPath?: string) {
  return queryClient.invalidateQueries({ queryKey: ["modGroup", groupPath] });
}

export function useModActions(selectedGroupPath?: string): ModActionApi {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const { renameModMutation } = useModMutations();
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();
  const runner = useModFixRunner();
  const selectedGame = useModStore((s) => s.selectedGame);
  const { data: games = [] } = useGames();
  const isNteGame = isNteImporter(games.find((game) => game.game === selectedGame)?.importer);

  const [textureResizeMod, setTextureResizeMod] = useState<ModInfo | null>(null);
  const [renameDialogState, setRenameDialogState] = useState<{
    groupPath?: string;
    mod: ModInfo;
  } | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [pastePreviewState, setPastePreviewState] = useState<{
    groupPath?: string;
    mod: ModInfo;
  } | null>(null);
  const [convertingModelPath, setConvertingModelPath] = useState<string | null>(null);
  const [modelViewerState, setModelViewerState] = useState<{
    groupPath?: string;
    mod: ModInfo;
    source: ModelViewerDialogSource;
  } | null>(null);
  const [showModelViewer, setShowModelViewer] = useState(false);
  const renameInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!renameDialogState) {
      return;
    }

    setRenameValue(getRenameDefaultValue(renameDialogState.mod.name));
    queueMicrotask(() => {
      renameInputRef.current?.focus();
      renameInputRef.current?.select();
    });
  }, [renameDialogState]);

  const handlePastePreview = async (mod: ModInfo, groupPath = selectedGroupPath) => {
    await pasteModPreview({
      modPath: mod.path,
      selectedGroupPath: groupPath,
      queryClient,
    });
  };

  const openDeleteMod = (mod: ModInfo) => {
    confirmTrash({
      path: mod.path,
      title: t("page.mod.dialog.delete-mod.title"),
      description: t("page.mod.dialog.delete-mod.description"),
      onSuccess: () => invalidateModGroup(queryClient, selectedGroupPath),
    });
  };

  const openDeletePreview = (mod: ModInfo) => {
    if (!mod.preview) {
      return;
    }

    confirmTrash({
      path: mod.preview,
      title: t("page.mod.dialog.delete-preview.title"),
      description: t("page.mod.dialog.delete-preview.description", { name: mod.name }),
      onSuccess: () => invalidateModGroup(queryClient, selectedGroupPath),
    });
  };

  const openPastePreview = (mod: ModInfo) => {
    if (mod.preview) {
      setPastePreviewState({ mod, groupPath: selectedGroupPath });
      return;
    }

    void handlePastePreview(mod, selectedGroupPath);
  };

  const openModelViewer = async (mod: ModInfo) => {
    if (convertingModelPath) {
      return;
    }

    setConvertingModelPath(mod.path);
    try {
      const result = await window.api.invoke("tools:convertStaticGlbForViewer", mod.path);
      setModelViewerState({
        mod,
        groupPath: selectedGroupPath,
        source:
          result.mode === "variant-set"
            ? {
                mode: "variant-set",
                artifactRoot: result.artifactRoot,
                manifestPath: result.manifestPath,
                modPath: mod.path,
                manifest: result.manifest,
                memorySessionId: result.memorySessionId,
                defaultGlbPath: result.defaultGlbPath,
                activeGlbPath: result.activeGlbPath,
                name: result.name,
              }
            : {
                mode: "single",
                glbPath: result.glbPath,
                memorySessionId: result.memorySessionId,
                modPath: mod.path,
                name: result.name,
              },
      });
      setShowModelViewer(true);
    } catch (error) {
      const rawMessage = toErrorMessage(error);
      const lastErrorIndex = rawMessage.lastIndexOf("Error");
      const message =
        lastErrorIndex !== -1
          ? rawMessage
              .slice(lastErrorIndex + "Error".length)
              .replace(/^:\s*/, "")
              .trim()
          : rawMessage;

      toast.error("Failed to open model viewer", {
        description: message,
      });
    } finally {
      setConvertingModelPath(null);
    }
  };

  const handleRenameSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();

    if (!renameDialogState) {
      return;
    }

    const nextName = renameValue.trim();
    if (!nextName) {
      toast.error(t("page.mod.hooks.use-mod-mutations.rename-mutation.2"));
      return;
    }

    try {
      await renameModMutation.mutateAsync({
        mod: renameDialogState.mod,
        newName: nextName,
        groupPath: renameDialogState.groupPath,
      });
      setRenameDialogState(null);
    } catch {
      return;
    }
  };

  const overlays = (
    <>
      <AlertDialog
        open={pastePreviewState !== null}
        onOpenChange={(open) => !open && setPastePreviewState(null)}
      >
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.overwrite-preview.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.overwrite-preview.description", {
                name: pastePreviewState?.mod.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (!pastePreviewState) {
                  return;
                }

                void handlePastePreview(pastePreviewState.mod, pastePreviewState.groupPath);
                setPastePreviewState(null);
              }}
            >
              {t("page.mod.dialog.overwrite-preview.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog
        open={renameDialogState !== null}
        onOpenChange={(open) => !open && setRenameDialogState(null)}
      >
        <DialogContent onClick={(event) => event.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>{t("page.mod.dialog.rename-mod.title")}</DialogTitle>
            <DialogDescription>{t("page.mod.dialog.rename-mod.description")}</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleRenameSubmit}>
            <Input
              ref={renameInputRef}
              value={renameValue}
              onChange={(event) => setRenameValue(event.target.value)}
              placeholder={t("page.mod.dialog.rename-mod.name-placeholder")}
              maxLength={255}
              disabled={renameModMutation.isPending}
            />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setRenameDialogState(null)}
                disabled={renameModMutation.isPending}
              >
                {t("g.cancel")}
              </Button>
              <Button type="submit" disabled={renameModMutation.isPending}>
                {t("page.mod.dialog.rename-mod.confirm")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <TextureResizeDialog
        open={textureResizeMod !== null}
        onOpenChange={(open) => !open && setTextureResizeMod(null)}
        modPath={textureResizeMod?.path ?? ""}
        modName={textureResizeMod?.name ?? ""}
      />

      <ModelViewerDialog
        open={showModelViewer}
        onOpenChange={(open) => {
          setShowModelViewer(open);
          if (!open) {
            scheduleModelViewerCleanup(modelViewerState?.source ?? null);
            setModelViewerState(null);
          }
        }}
        source={modelViewerState?.source ?? null}
        existingPreviewPath={modelViewerState?.mod.preview}
        onPreviewSaved={() => invalidateModGroup(queryClient, modelViewerState?.groupPath)}
      />

      {confirmTrashDialog}
      <ModFixRunnerDialogs runner={runner} />
    </>
  );

  return {
    overlays,
    runner,
    isNteGame,
    convertingModelPath,
    openDeleteMod,
    openDeletePreview,
    openModelViewer,
    openPastePreview,
    openRenameDialog: (mod) => setRenameDialogState({ mod, groupPath: selectedGroupPath }),
    openTextureResizeDialog: (mod) => setTextureResizeMod(mod),
    openWuwaFixer: async (mod) => {
      await runner.handleOpenWuwaFixer(mod.path);
    },
    markAsManualSubGroup: async (mod) => {
      await window.api
        .invoke("mod:setManualSubGroup", mod.path, true)
        .then(() =>
          Promise.all([
            invalidateModGroup(queryClient, selectedGroupPath),
            selectedGame
              ? queryClient.invalidateQueries({ queryKey: ["characters", selectedGame] })
              : Promise.resolve(),
            queryClient.invalidateQueries({ queryKey: ["subGroups"] }),
            queryClient.invalidateQueries({ queryKey: ["manualSubGroups"] }),
          ]),
        )
        .then(() => {
          toast.success(t("page.mod.toast.manual-subgroup-success"));
        })
        .catch((error) => {
          toast.error(toErrorMessage(error));
        });
    },
    runPreset: async (mod, presetId) => {
      await runner.handleRun("preset", presetId, mod.path);
    },
    runTool: async (mod, toolId) => {
      await runner.handleRun("tool", toolId, mod.path);
    },
  };
}
