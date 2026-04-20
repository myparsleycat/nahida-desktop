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
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import { useModMutations } from "@renderer/hooks/use-mod-mutations";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import {
  BoxIcon,
  ClipboardIcon,
  ChevronRightIcon,
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

interface ModContextMenuProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  fixTools: {
    id: string;
    name: string;
    type: string;
    size: number;
  }[];
  presets: {
    id: string;
    name: string;
  }[];
  onPaste?: () => void | Promise<void>;
  children: ReactNode;
}

const DISABLED_PREFIX_REGEX = /^disabled\s+/i;

const getRenameDefaultValue = (name: string) => name.replace(DISABLED_PREFIX_REGEX, "").trim();

export function ModContextMenu({
  mod,
  selectedGroupPath,
  fixTools,
  presets,
  onPaste,
  children,
}: ModContextMenuProps) {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const { renameModMutation } = useModMutations();
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();

  const [showLogModal, setShowLogModal] = useState(false);
  const [showPasteConfirmDialog, setShowPasteConfirmDialog] = useState(false);
  const [showRenameDialog, setShowRenameDialog] = useState(false);
  const [renameValue, setRenameValue] = useState(getRenameDefaultValue(mod.name));
  const [logs, setLogs] = useState<string[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [isConvertingModel, setIsConvertingModel] = useState(false);
  const [modelViewerSource, setModelViewerSource] = useState<ModelViewerDialogSource | null>(null);
  const [showModelViewer, setShowModelViewer] = useState(false);
  const [inputCmd, setInputCmd] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const renameInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!showLogModal) return;
    const removeListener = window.api.on("ftm:log", (msg: string) => {
      setLogs((prev) => [...prev, msg]);
    });
    return () => removeListener();
  }, [showLogModal]);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs]);

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
      void cleanupModelViewerSource(modelViewerSource);
    };
  }, [modelViewerSource]);

  const handleRun = async (type: "tool" | "preset", id: string) => {
    setShowLogModal(true);
    setLogs([]);
    setIsRunning(true);
    try {
      if (type === "tool") {
        await window.api.invoke("ftm:runScript", id, mod.path);
      } else {
        await window.api.invoke("ftm:runPreset", id, mod.path);
      }
    } catch (e) {
      console.error(e);
    } finally {
      setIsRunning(false);
    }
  };

  const handleCancel = () => {
    window.api.invoke("ftm:cancelRun");
  };

  const handleSendInput = () => {
    if (!isRunning) {
      return;
    }

    window.api.invoke("ftm:sendInput", `${inputCmd}\r\n`);
    setInputCmd("");
  };

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
    if (!source?.glbPath) {
      return;
    }

    try {
      await window.api.invoke("tools:cleanupStaticGlbViewerFile", source.glbPath);
    } catch (error) {
      console.warn("Failed to clean up model viewer file", error);
    }
  };

  const handleOpenModelViewer = async () => {
    if (isConvertingModel) return;

    setIsConvertingModel(true);
    try {
      const result = await window.api.invoke("tools:convertStaticGlbForViewer", mod.path);
      setModelViewerSource({
        glbPath: result.glbPath,
        name: result.name,
      });
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
                  {presets.map((preset) => (
                    <ContextMenuItem key={preset.id} onClick={() => handleRun("preset", preset.id)}>
                      {preset.name}
                    </ContextMenuItem>
                  ))}
                  {presets.length === 0 && <ContextMenuItem disabled>No Presets</ContextMenuItem>}
                </ContextMenuGroup>
              </ContextMenuSubContent>
            </ContextMenuSub>
            <ContextMenuSub>
              <ContextMenuSubTrigger>Fix Tool</ContextMenuSubTrigger>
              <ContextMenuSubContent>
                <ContextMenuGroup>
                  {fixTools.map((tool) => (
                    <ContextMenuItem key={tool.id} onClick={() => handleRun("tool", tool.id)}>
                      {tool.name}
                    </ContextMenuItem>
                  ))}
                  {fixTools.length === 0 && (
                    <ContextMenuItem disabled>No Fix Tools</ContextMenuItem>
                  )}
                </ContextMenuGroup>
              </ContextMenuSubContent>
            </ContextMenuSub>
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
            Model Viewer
          </ContextMenuItem>
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

      <AlertDialog open={showLogModal} onOpenChange={setShowLogModal}>
        <AlertDialogContent
          onEscapeKeyDown={(e) => {
            if (isRunning) {
              e.preventDefault();
              handleCancel();
            }
          }}
          onClick={(e) => e.stopPropagation()}
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            inputRef.current?.focus();
          }}
          className="min-w-xl"
        >
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.log-dialog.title")}</AlertDialogTitle>
          </AlertDialogHeader>
          <ScrollArea
            viewportRef={scrollRef}
            className="h-[calc(100vh-430px)] w-full rounded-md border bg-muted font-mono text-xs whitespace-pre-wrap break-all"
          >
            <div className="p-3 space-y-2">
              {logs.map((log, i) => (
                <div key={`log-${i.toString()}`} className="flex flex-row space-x-1 w-full">
                  <ChevronRightIcon className="size-4 shrink-0" />
                  <div
                    className={cn(
                      log.toLowerCase().includes("complete") && "text-green-500",
                      log.toLowerCase().includes("error") && "text-red-500",
                      log.toLowerCase().includes("warning") && "text-yellow-500",
                    )}
                  >
                    {log}
                  </div>
                </div>
              ))}
              {isRunning && (
                <div className="animate-pulse text-primary">{t("page.mod.log-dialog.running")}</div>
              )}
            </div>
          </ScrollArea>
          <div className="flex gap-2">
            <Input
              ref={inputRef}
              placeholder="Input..."
              value={inputCmd}
              disabled={!isRunning}
              onChange={(e) => setInputCmd(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  if (!isRunning) {
                    return;
                  }

                  handleSendInput();
                }
              }}
            />
            <Button
              variant="outline"
              size="icon"
              className="shrink-0"
              onClick={handleSendInput}
              disabled={!isRunning}
            >
              <TerminalSquareIcon className="size-4" />
            </Button>
          </div>
          <AlertDialogFooter>
            {isRunning ? (
              <Button variant="destructive" onClick={handleCancel}>
                {t("g.cancel")}
              </Button>
            ) : (
              <Button onClick={() => setShowLogModal(false)}>{t("g.close")}</Button>
            )}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {confirmTrashDialog}
      <ModelViewerDialog
        open={showModelViewer}
        onOpenChange={(open) => {
          setShowModelViewer(open);
          if (!open) {
            void cleanupModelViewerSource(modelViewerSource);
            setModelViewerSource(null);
          }
        }}
        source={modelViewerSource}
      />
    </>
  );
}
