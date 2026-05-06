import {
  ModelViewerDialog,
  type ModelViewerDialogSource,
} from "@renderer/components/tools/model-viewer-dialog";
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
  ContextMenu,
  ContextMenuContent,
  ContextMenuGroup,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@renderer/components/ui/context-menu";
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
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import {
  BoxIcon,
  ClipboardIcon,
  FolderIcon,
  ImageIcon,
  Loader2Icon,
  PencilIcon,
  TerminalSquareIcon,
  TrashIcon,
} from "lucide-react";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { hasModPreviewFile } from "./paste-preview";
import type { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";

interface ModContextMenuProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  runner: ReturnType<typeof useModFixRunner>;
  onOpenTextureResizeDialog?: () => void;
  onPaste?: () => void | Promise<void>;
  children: ReactNode;
}

const DISABLED_PREFIX_REGEX = /^disabled\s+/i;

const getRenameDefaultValue = (name: string) => name.replace(DISABLED_PREFIX_REGEX, "").trim();

export function ModContextMenu({
  mod,
  selectedGroupPath,
  runner,
  onOpenTextureResizeDialog,
  onPaste,
  children,
}: ModContextMenuProps) {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const { renameModMutation } = useModMutations();
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();

  const [showPasteConfirmDialog, setShowPasteConfirmDialog] = useState(false);
  const [showRenameDialog, setShowRenameDialog] = useState(false);
  const [renameValue, setRenameValue] = useState(getRenameDefaultValue(mod.name));
  const [isConvertingModel, setIsConvertingModel] = useState(false);
  const [modelViewerSource, setModelViewerSource] = useState<ModelViewerDialogSource | null>(null);
  const [showModelViewer, setShowModelViewer] = useState(false);
  const renameInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!showRenameDialog) return;
    setRenameValue(getRenameDefaultValue(mod.name));
    queueMicrotask(() => {
      renameInputRef.current?.focus();
      renameInputRef.current?.select();
    });
  }, [mod.name, showRenameDialog]);

  const invalidateModGroup = async () => {
    await queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
  };

  useEffect(() => {
    return () => {
      scheduleModelViewerCleanup(modelViewerSource);
    };
  }, [modelViewerSource]);

  const handleDelete = () => {
    confirmTrash({
      path: mod.path,
      title: t("page.mod.dialog.delete-mod.title"),
      description: t("page.mod.dialog.delete-mod.description"),
      onSuccess: async () => {
        await invalidateModGroup();
      },
    });
  };

  const handleDeletePreview = () => {
    if (!mod.preview) return;
    confirmTrash({
      path: mod.preview,
      title: t("page.mod.dialog.delete-preview.title"),
      description: t("page.mod.dialog.delete-preview.description", { name: mod.name }),
      onSuccess: async () => {
        await invalidateModGroup();
      },
    });
  };

  const cleanupModelViewerSource = async (source: ModelViewerDialogSource | null) => {
    if (!source) {
      return;
    }

    try {
      await window.api.invoke(
        "tools:cleanupStaticGlbViewerFile",
        source.mode === "variant-set" ? source.artifactRoot : source.glbPath,
      );
    } catch (error) {
      console.warn("Failed to clean up model viewer file", error);
    }
  };

  const scheduleModelViewerCleanup = (source: ModelViewerDialogSource | null) => {
    if (!source) {
      return;
    }

    window.setTimeout(() => {
      void cleanupModelViewerSource(source);
    }, 0);
  };

  const handleOpenModelViewer = async () => {
    if (isConvertingModel) return;

    setIsConvertingModel(true);
    try {
      const result = await window.api.invoke("tools:convertStaticGlbForViewer", mod.path);
      setModelViewerSource(
        result.mode === "variant-set"
          ? {
              mode: "variant-set",
              artifactRoot: result.artifactRoot,
              manifestPath: result.manifestPath,
              manifest: result.manifest,
              defaultGlbPath: result.defaultGlbPath,
              activeGlbPath: result.activeGlbPath,
              name: result.name,
            }
          : {
              mode: "single",
              glbPath: result.glbPath,
              name: result.name,
            },
      );
      setShowModelViewer(true);
    } catch (error) {
      const rawMsg = error instanceof Error ? error.message : String(error);

      const lastErrorIndex = rawMsg.lastIndexOf("Error");

      const msg =
        lastErrorIndex !== -1
          ? rawMsg
              .slice(lastErrorIndex + "Error".length)
              .replace(/^:\s*/, "")
              .trim()
          : rawMsg;

      toast.error("Failed to open model viewer", {
        description: msg,
      });
    } finally {
      setIsConvertingModel(false);
    }
  };

  const handlePaste = () => {
    void onPaste?.();
  };

  const handlePasteClick = () => {
    if (!onPaste) return;

    if (hasModPreviewFile(mod.path, mod.preview)) {
      setShowPasteConfirmDialog(true);
      return;
    }

    handlePaste();
  };

  const handleRenameSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const nextName = renameValue.trim();

    if (!nextName) {
      toast.error(t("page.mod.hooks.use-mod-mutations.rename-mutation.2"));
      return;
    }

    try {
      await renameModMutation.mutateAsync({ mod, newName: nextName });
      setShowRenameDialog(false);
    } catch {
      return;
    }
  };

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
        <ContextMenuContent className="w-48">
          {(mod.preview?.match(/\.(jpeg|jpg|gif|png|webp|bmp|mp4|webm|ogg)$/i) || onPaste) && (
            <>
              <ContextMenuGroup>
                <ContextMenuLabel>Preview</ContextMenuLabel>
                {mod.preview?.match(/\.(jpeg|jpg|gif|png|webp|bmp|mp4|webm|ogg)$/i) && (
                  <ContextMenuItem
                    onClick={() => {
                      if (!mod.preview) return;

                      window.api.invoke("util:openExternal", mod.preview).catch((error) => {
                        toast.error("Failed to open external", {
                          description: error.message,
                        });
                      });
                    }}
                  >
                    <ImageIcon className="mr-2 size-4" />
                    {t("page.mod.context-menu.open-preview-viewer")}
                  </ContextMenuItem>
                )}
                {onPaste && (
                  <ContextMenuItem onClick={handlePasteClick}>
                    <ClipboardIcon className="mr-2 size-4" />
                    {t("page.mod.context-menu.paste-preview")}
                  </ContextMenuItem>
                )}
                {mod.preview && (
                  <ContextMenuItem variant="destructive" onClick={handleDeletePreview}>
                    <TrashIcon className="mr-2 size-4" />
                    {t("page.mod.context-menu.delete-preview")}
                  </ContextMenuItem>
                )}
              </ContextMenuGroup>
              <ContextMenuSeparator />
            </>
          )}
          <ContextMenuGroup>
            <ContextMenuLabel>Fix</ContextMenuLabel>
            <ContextMenuSub>
                <ContextMenuSubTrigger>Preset</ContextMenuSubTrigger>
              <ContextMenuSubContent>
                <ContextMenuGroup>
                  {runner.presets.map((preset) => (
                    <ContextMenuItem
                      key={preset.id}
                      onClick={() => runner.handleRun("preset", preset.id)}
                    >
                      {preset.name}
                    </ContextMenuItem>
                  ))}
                  {runner.presets.length === 0 && (
                    <ContextMenuItem disabled>No Presets</ContextMenuItem>
                  )}
                </ContextMenuGroup>
              </ContextMenuSubContent>
            </ContextMenuSub>
            <ContextMenuSub>
              <ContextMenuSubTrigger>Fix Tool</ContextMenuSubTrigger>
              <ContextMenuSubContent>
                <ContextMenuGroup>
                  {runner.fixTools.map((tool) => (
                    <ContextMenuItem
                      key={tool.id}
                      onClick={() => runner.handleRun("tool", tool.id)}
                    >
                      {tool.name}
                    </ContextMenuItem>
                  ))}
                  {runner.fixTools.length === 0 && (
                    <ContextMenuItem disabled>No Fix Tools</ContextMenuItem>
                  )}
                </ContextMenuGroup>
              </ContextMenuSubContent>
            </ContextMenuSub>
            {runner.showWuwaFixer && (
              <ContextMenuItem disabled={runner.isPreparing} onClick={() => void runner.handleOpenWuwaFixer()}>
                Wuwa Mod Fixer
              </ContextMenuItem>
            )}
          </ContextMenuGroup>
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={() => {
              window.api.invoke("util:openCmd", mod.path);
            }}
          >
            <TerminalSquareIcon className="mr-2 size-4" />
            {t("page.mod.context-menu.open-cmd")}
          </ContextMenuItem>
          <ContextMenuItem
            onClick={() => {
              window.api.invoke("util:openPath", mod.path);
            }}
          >
            <FolderIcon className="mr-2 size-4" />
            {t("page.mod.context-menu.open-folder")}
          </ContextMenuItem>
          <ContextMenuItem disabled={isConvertingModel} onClick={handleOpenModelViewer}>
            {isConvertingModel ? (
              <Loader2Icon className="mr-2 size-4 animate-spin" />
            ) : (
              <BoxIcon className="mr-2 size-4" />
            )}
            {t("page.tools.model_viewer.title")}
          </ContextMenuItem>
          {onOpenTextureResizeDialog && (
            <ContextMenuItem onClick={onOpenTextureResizeDialog}>
              <ImageIcon className="mr-2 size-4" />
              {t("page.tools.texture_resizer.title")}
            </ContextMenuItem>
          )}
          <ContextMenuItem onClick={() => setShowRenameDialog(true)}>
            <PencilIcon className="mr-2 size-4" />
            {t("page.mod.context-menu.rename")}
          </ContextMenuItem>
          <ContextMenuSeparator />
          <ContextMenuItem variant="destructive" onClick={handleDelete}>
            <TrashIcon className="mr-2 size-4" />
            {t("g.delete")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>

      <AlertDialog open={showPasteConfirmDialog} onOpenChange={setShowPasteConfirmDialog}>
        <AlertDialogContent onClick={(e) => e.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.overwrite-preview.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.overwrite-preview.description", { name: mod.name })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={handlePaste}>
              {t("page.mod.dialog.overwrite-preview.confirm")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={showRenameDialog} onOpenChange={setShowRenameDialog}>
        <DialogContent aria-describedby={undefined} onClick={(e) => e.stopPropagation()}>
          <DialogHeader>
            <DialogTitle>{t("page.mod.dialog.rename-mod.title")}</DialogTitle>
            <DialogDescription>{t("page.mod.dialog.rename-mod.description")}</DialogDescription>
          </DialogHeader>
          <form className="space-y-4" onSubmit={handleRenameSubmit}>
            <Input
              ref={renameInputRef}
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              placeholder={t("page.mod.dialog.rename-mod.name-placeholder")}
              maxLength={255}
              disabled={renameModMutation.isPending}
            />
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setShowRenameDialog(false)}
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

      {confirmTrashDialog}
      <ModelViewerDialog
        open={showModelViewer}
        onOpenChange={(open) => {
          setShowModelViewer(open);
          if (!open) {
            scheduleModelViewerCleanup(modelViewerSource);
            setModelViewerSource(null);
          }
        }}
        source={modelViewerSource}
      />
    </>
  );
}
