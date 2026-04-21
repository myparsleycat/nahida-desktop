import {
  ModelViewerDialog,
  type ModelViewerDialogSource,
} from "@renderer/components/tools/model-viewer-dialog";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@renderer/components/ui/alert-dialog";
import { Button, buttonVariants } from "@renderer/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Input } from "@renderer/components/ui/input";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import {
  BoxIcon,
  ChevronRightIcon,
  FolderIcon,
  Loader2Icon,
  TerminalSquareIcon,
  TrashIcon,
  WrenchIcon,
} from "lucide-react";
import { memo, useEffect, useRef, useState, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface ModCardHeaderProps {
  mod: ModInfo;
  selectedGroupPath?: string;
}

export const ModCardHeader = memo(function ModCardHeader({
  mod,
  selectedGroupPath,
}: ModCardHeaderProps) {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });

  const [fixTools, setFixTools] = useState<
    { id: string; name: string; type: string; size: number }[]
  >([]);
  const [presets, setPresets] = useState<{ id: string; name: string }[]>([]);
  const [showLogModal, setShowLogModal] = useState(false);
  const [logs, setLogs] = useState<string[]>([]);
  const [isRunning, setIsRunning] = useState(false);
  const [isConvertingModel, setIsConvertingModel] = useState(false);
  const [modelViewerSource, setModelViewerSource] = useState<ModelViewerDialogSource | null>(null);
  const [showModelViewer, setShowModelViewer] = useState(false);
  const [inputCmd, setInputCmd] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();

  useEffect(() => {
    window.api.invoke("ftm:getScripts").then(setFixTools);
    window.api.invoke("ftm:getPresets").then(setPresets);
  }, []);

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

  const handleDelete = (e: MouseEvent) => {
    e.stopPropagation();
    confirmTrash({
      path: mod.path,
      title: t("page.mod.dialog.delete-mod.title"),
      description: t("page.mod.dialog.delete-mod.description"),
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
      },
      contentProps: {
        onClick: (event) => event.stopPropagation(),
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

  const handleOpenModelViewer = async (e: MouseEvent) => {
    e.stopPropagation();
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

  useEffect(() => {
    return () => {
      void cleanupModelViewerSource(modelViewerSource);
    };
  }, [modelViewerSource]);

  return (
    <>
      <div className="flex items-center justify-between pb-1 relative z-10">
        <span className="text-sm truncate font-semibold">
          {mod.name.replace(/disabled/gi, "").trim()}
        </span>
        <div className="flex items-center gap-1">
          <DropdownMenu>
            <DropdownMenuTrigger
              className={cn(
                buttonVariants({ variant: "ghost", size: "icon" }),
                "size-7 hover:bg-accent/20",
              )}
            >
              <WrenchIcon />
            </DropdownMenuTrigger>
            <DropdownMenuContent
              onClick={(e) => e.stopPropagation()}
              onCloseAutoFocus={(e) => e.preventDefault()}
              className="max-w-52"
            >
              <DropdownMenuGroup>
                <DropdownMenuLabel>Preset</DropdownMenuLabel>
                {presets.map((preset) => (
                  <DropdownMenuItem
                    key={preset.id}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleRun("preset", preset.id);
                    }}
                    className="p-0"
                  >
                    <Tooltip disableHoverableContent={true}>
                      <TooltipTrigger className="w-full h-full text-start truncate p-1">
                        {preset.name}
                      </TooltipTrigger>
                      <TooltipContent>
                        <p className="text-wrap">{preset.name}</p>
                      </TooltipContent>
                    </Tooltip>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Fix Tool</DropdownMenuLabel>
                {fixTools.map((tool) => (
                  <DropdownMenuItem
                    key={tool.id}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleRun("tool", tool.id);
                    }}
                    className="p-0"
                  >
                    <Tooltip disableHoverableContent={true}>
                      <TooltipTrigger className="w-full h-full text-start truncate p-1">
                        {tool.name}
                      </TooltipTrigger>
                      <TooltipContent>
                        <p className="text-wrap break-all">{tool.name}</p>
                      </TooltipContent>
                    </Tooltip>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>

          <Button
            variant="ghost"
            size="icon"
            className="size-7 hover:bg-accent/20"
            disabled={isConvertingModel}
            onClick={handleOpenModelViewer}
            title="Model Viewer"
          >
            {isConvertingModel ? <Loader2Icon className="animate-spin" /> : <BoxIcon />}
          </Button>

          <Button
            variant="ghost"
            size="icon"
            className="size-7 hover:bg-accent/20"
            onClick={(e) => {
              e.stopPropagation();
              window.api.invoke("util:openCmd", mod.path);
            }}
          >
            <TerminalSquareIcon />
          </Button>

          <Button
            variant="ghost"
            size="icon"
            className="size-7 hover:bg-accent/20"
            onClick={handleDelete}
          >
            <TrashIcon />
          </Button>

          <Button
            variant="ghost"
            size="icon"
            className="size-7 hover:bg-accent/20"
            onClick={(e) => {
              e.stopPropagation();
              window.api.invoke("util:openPath", mod.path);
            }}
          >
            <FolderIcon />
          </Button>
        </div>

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
                  <div className="animate-pulse text-primary">
                    {t("page.mod.log-dialog.running")}
                  </div>
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
        {confirmTrashDialog}
      </div>
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
});
