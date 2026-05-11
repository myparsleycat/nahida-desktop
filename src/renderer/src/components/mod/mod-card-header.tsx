import {
  ModelViewerDialog,
  type ModelViewerDialogSource,
} from "@renderer/components/tools/model-viewer/model-viewer-dialog";
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
import { Tooltip, TooltipContent, TooltipTrigger } from "@renderer/components/ui/tooltip";
import { useConfirmTrash } from "@renderer/hooks/use-confirm-trash";
import type { useModFixRunner } from "@renderer/hooks/use-mod-fix-runner";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { useRouteContext } from "@tanstack/react-router";
import {
  BoxIcon,
  FolderIcon,
  ImageIcon,
  Loader2Icon,
  TerminalSquareIcon,
  TrashIcon,
  WrenchIcon,
} from "lucide-react";
import { memo, useEffect, useState, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

interface ModCardHeaderProps {
  mod: ModInfo;
  selectedGroupPath?: string;
  onOpenTextureResizeDialog?: (event: MouseEvent) => void;
  runner: ReturnType<typeof useModFixRunner>;
}

export const ModCardHeader = memo(function ModCardHeader({
  mod,
  selectedGroupPath,
  onOpenTextureResizeDialog,
  runner,
}: ModCardHeaderProps) {
  const { t } = useTranslation();
  const { queryClient } = useRouteContext({ from: "__root__" });
  const [isConvertingModel, setIsConvertingModel] = useState(false);
  const [modelViewerSource, setModelViewerSource] = useState<ModelViewerDialogSource | null>(null);
  const [showModelViewer, setShowModelViewer] = useState(false);
  const { confirmTrash, confirmTrashDialog } = useConfirmTrash();

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
        source.memorySessionId,
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
      scheduleModelViewerCleanup(modelViewerSource);
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
                "size-7 hover:bg-muted/20 aria-expanded:bg-muted/50",
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
                <DropdownMenuLabel>Mod Tools</DropdownMenuLabel>
                <DropdownMenuItem
                  onClick={(e) => {
                    e.stopPropagation();
                    onOpenTextureResizeDialog?.(e);
                  }}
                >
                  <ImageIcon className="mr-2 size-4" />
                  {t("page.tools.texture_resizer.title")}
                </DropdownMenuItem>
              </DropdownMenuGroup>

              <DropdownMenuSeparator />

              <DropdownMenuGroup>
                <DropdownMenuLabel>Preset ({runner.presets.length})</DropdownMenuLabel>
                {runner.presets.map((preset) => (
                  <DropdownMenuItem
                    key={preset.id}
                    onClick={(e) => {
                      e.stopPropagation();
                      runner.handleRun("preset", preset.id);
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

              <DropdownMenuGroup>
                <DropdownMenuLabel>Fix Tool ({runner.fixTools.length})</DropdownMenuLabel>
                {runner.fixTools.map((tool) => (
                  <DropdownMenuItem
                    key={tool.id}
                    onClick={(e) => {
                      e.stopPropagation();
                      runner.handleRun("tool", tool.id);
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
              {runner.showWuwaFixer && (
                <DropdownMenuGroup>
                  <DropdownMenuItem
                    disabled={runner.isPreparing}
                    onClick={(e) => {
                      e.stopPropagation();
                      void runner.handleOpenWuwaFixer();
                    }}
                  >
                    Wuwa Mod Fixer
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              )}
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

        {confirmTrashDialog}
      </div>
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
        existingPreviewPath={mod.preview}
        onPreviewSaved={async () => {
          await queryClient.invalidateQueries({ queryKey: ["modGroup", selectedGroupPath] });
        }}
      />
    </>
  );
});
