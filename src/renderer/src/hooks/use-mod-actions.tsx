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
import { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ModFixRunnerDialogs } from "../components/mod/mod-fix-runner-dialogs";
import { pasteModPreview } from "../components/mod/paste-preview";
import { TextureResizeDialog } from "../components/mod/texture-resize-dialog";

const DISABLED_PREFIX_REGEX = /^disabled\s+/i;

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
  convertingModelPath: string | null;
  openDeleteMod: (mod: ModInfo) => void;
  openDeletePreview: (mod: ModInfo) => void;
  openModelViewer: (mod: ModInfo) => Promise<void>;
  openPastePreview: (mod: ModInfo) => void;
  openRenameDialog: (mod: ModInfo) => void;
  openTextureResizeDialog: (mod: ModInfo) => void;
  openWuwaFixer: (mod: ModInfo) => Promise<void>;
  runPreset: (mod: ModInfo, presetId: string) => Promise<void>;
  runTool: (mod: ModInfo, toolId: string) => Promise<void>;
}

export function useModActions(selectedGroupPath?: string): ModActionApi {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const { renameModMutation } = useModMutations();
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();
  const runner = useModFixRunner();

  const [textureResizeMod, setTextureResizeMod] = useState<ModInfo | null>(null);
  const [renameMod, setRenameMod] = useState<ModInfo | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [pastePreviewMod, setPastePreviewMod] = useState<ModInfo | null>(null);
  const [convertingModelPath, setConvertingModelPath] = useState<string | null>(null);
  const [modelViewerMod, setModelViewerMod] = useState<ModInfo | null>(null);
  const [modelViewerSource, setModelViewerSource] = useState<ModelViewerDialogSource | null>(null);
  const [showModelViewer, setShowModelViewer] = useState(false);
  const renameInputRef = useRef<HTMLInputElement>(null);

  const invalidateModGroup = async () => {
    await queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
  };

  useEffect(() => {
    if (!renameMod) {
      return;
    }

    setRenameValue(getRenameDefaultValue(renameMod.name));
    queueMicrotask(() => {
      renameInputRef.current?.focus();
      renameInputRef.current?.select();
    });
  }, [renameMod]);

  useEffect(() => {
    return () => {
      scheduleModelViewerCleanup(modelViewerSource);
    };
  }, [modelViewerSource]);

  const handlePastePreview = async (mod: ModInfo) => {
    await pasteModPreview({
      modPath: mod.path,
      selectedGroupPath,
      queryClient,
    });
  };

  const openDeleteMod = (mod: ModInfo) => {
    confirmTrash({
      path: mod.path,
      title: t("page.mod.dialog.delete-mod.title"),
      description: t("page.mod.dialog.delete-mod.description"),
      onSuccess: invalidateModGroup,
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
      onSuccess: invalidateModGroup,
    });
  };

  const openPastePreview = (mod: ModInfo) => {
    if (mod.preview) {
      setPastePreviewMod(mod);
      return;
    }

    void handlePastePreview(mod);
  };

  const openModelViewer = async (mod: ModInfo) => {
    if (convertingModelPath) {
      return;
    }

    setConvertingModelPath(mod.path);
    try {
      const result = await window.api.invoke("tools:convertStaticGlbForViewer", mod.path);
      setModelViewerMod(mod);
      setModelViewerSource(
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
      );
      setShowModelViewer(true);
    } catch (error) {
      const rawMessage = error instanceof Error ? error.message : String(error);
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

    if (!renameMod) {
      return;
    }

    const nextName = renameValue.trim();
    if (!nextName) {
      toast.error(t("page.mod.hooks.use-mod-mutations.rename-mutation.2"));
      return;
    }

    try {
      await renameModMutation.mutateAsync({ mod: renameMod, newName: nextName });
      setRenameMod(null);
    } catch {
      return;
    }
  };

  const overlays = (
    <>
      <AlertDialog
        open={pastePreviewMod !== null}
        onOpenChange={(open) => !open && setPastePreviewMod(null)}
      >
        <AlertDialogContent onClick={(event) => event.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.overwrite-preview.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.overwrite-preview.description", {
                name: pastePreviewMod?.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (!pastePreviewMod) {
                  return;
                }

                void handlePastePreview(pastePreviewMod);
                setPastePreviewMod(null);
              }}
            >
              {t("page.mod.dialog.overwrite-preview.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={renameMod !== null} onOpenChange={(open) => !open && setRenameMod(null)}>
        <DialogContent aria-describedby={undefined} onClick={(event) => event.stopPropagation()}>
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
                onClick={() => setRenameMod(null)}
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
            scheduleModelViewerCleanup(modelViewerSource);
            setModelViewerSource(null);
            setModelViewerMod(null);
          }
        }}
        source={modelViewerSource}
        existingPreviewPath={modelViewerMod?.preview}
        onPreviewSaved={invalidateModGroup}
      />

      {confirmTrashDialog}
      <ModFixRunnerDialogs runner={runner} />
    </>
  );

  return {
    overlays,
    runner,
    convertingModelPath,
    openDeleteMod,
    openDeletePreview,
    openModelViewer,
    openPastePreview,
    openRenameDialog: (mod) => setRenameMod(mod),
    openTextureResizeDialog: (mod) => setTextureResizeMod(mod),
    openWuwaFixer: async (mod) => {
      await runner.handleOpenWuwaFixer(mod.path);
    },
    runPreset: async (mod, presetId) => {
      await runner.handleRun("preset", presetId, mod.path);
    },
    runTool: async (mod, toolId) => {
      await runner.handleRun("tool", toolId, mod.path);
    },
  };
}
