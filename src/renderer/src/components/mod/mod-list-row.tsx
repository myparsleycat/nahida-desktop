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
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import i18n from "@renderer/lib/i18n";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { formatDate, formatSize } from "@shared/utils";
import { useRouteContext } from "@tanstack/react-router";
import {
  ChevronRightIcon,
  FolderIcon,
  ImageIcon,
  TerminalSquareIcon,
  TrashIcon,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { ModPreviewLightbox } from "./mod-preview-lightbox";
import { getModColorClass } from "./utils";

export function ModListRow({
  mod,
  selectedGroupPath,
  handleToggle,
  fixTools,
  presets,
}: {
  mod: ModInfo;
  selectedGroupPath?: string;
  handleToggle: (mod: ModInfo, e?: React.MouseEvent) => void;
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
}) {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });

  const [showDeleteModal, setShowDeleteModal] = useState(false);
  const [showLogModal, setShowLogModal] = useState(false);
  const [logs, setLogs] = useState<string[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [inputCmd, setInputCmd] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

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
    window.api.invoke("ftm:sendInput", `${inputCmd}\r\n`);
    setInputCmd("");
  };

  const handleDelete = () => {
    const promise = window.api.invoke("util:fs:trash", mod.path);
    toast.promise(promise, {
      loading: t("page.mod.toast.trash-loading"),
      success: t("page.mod.toast.trash-success"),
      error: t("page.mod.toast.trash-error"),
    });
    promise.then(() => {
      queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
    });
    setShowDeleteModal(false);
  };

  const handleDeletePreview = () => {
    if (!mod.preview) return;
    const promise = window.api.invoke("util:fs:trash", mod.preview);
    toast.promise(promise, {
      loading: t("page.mod.toast.trash-loading"),
      success: t("page.mod.toast.trash-success"),
      error: t("page.mod.toast.trash-error"),
    });
    promise.then(() => {
      queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
    });
  };

  return (
    <>
      <ContextMenu>
        <ContextMenuTrigger asChild>
          <tr
            className={cn(
              "relative group cursor-pointer border-b border-transparent transition-colors",
              getModColorClass(mod.isEnabled),
              "after:absolute after:inset-0 after:pointer-events-none hover:after:bg-black/10 dark:hover:after:bg-white/10",
            )}
            onClick={(e) => {
              const target = e.target as HTMLElement;
              if (target.closest("button") || target.closest(".preview-trigger")) {
                return;
              }
              handleToggle(mod, e);
            }}
          >
            <td className="py-2 pl-2 align-middle text-center w-[40px]">
              {mod.preview ? (
                <div className="preview-trigger" onClick={(e) => e.stopPropagation()}>
                  <ModPreviewLightbox preview={mod.preview} />
                </div>
              ) : (
                <div className="size-10 rounded-sm bg-secondary/20 flex items-center justify-center overflow-hidden shrink-0">
                  <FolderIcon className="size-5 text-muted-foreground" />
                </div>
              )}
            </td>
            <td className="p-2 align-middle text-left w-full max-w-0">
              <span className="truncate block w-full text-left font-medium">
                {mod.name.replace(/disabled/gi, "").trim()}
              </span>
            </td>
            <td className="p-2 align-middle text-muted-foreground whitespace-nowrap text-right w-[1%]">
              {formatSize(mod.size || 0)}
            </td>
            <td className="p-2 pr-3 align-middle text-muted-foreground whitespace-nowrap text-right w-[1%]">
              {formatDate(new Date(mod.mtime), i18n.language)}
            </td>
          </tr>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-48">
          {mod.preview?.match(/\.(jpeg|jpg|gif|png|webp|bmp|mp4|webm|ogg)$/i) && (
            <>
              <ContextMenuGroup>
                <ContextMenuLabel>Preview</ContextMenuLabel>
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
                <ContextMenuItem onClick={handleDeletePreview}>
                  <TrashIcon className="mr-2 size-4" />
                  {t("page.mod.context-menu.delete-preview")}
                </ContextMenuItem>
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
          <ContextMenuSeparator />
          <ContextMenuItem
            onClick={() => setShowDeleteModal(true)}
            className="text-destructive focus:bg-destructive/10 focus:text-destructive"
          >
            <TrashIcon className="mr-2 size-4" />
            {t("g.delete")}
          </ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>

      <AlertDialog open={showDeleteModal} onOpenChange={setShowDeleteModal}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("page.mod.dialog.delete-mod.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("page.mod.dialog.delete-mod.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("g.cancel")}</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>{t("g.delete")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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
              onChange={(e) => setInputCmd(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
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
    </>
  );
}
