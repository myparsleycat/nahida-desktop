import { Shell } from "@bindings/platform";
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
import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import { cn } from "@renderer/lib/utils";
import type { ModInfo } from "@renderer/types/mod";
import { stripDisabledPrefix } from "@shared/mod";
import {
  BoxIcon,
  FolderIcon,
  ImageIcon,
  Loader2Icon,
  PersonStandingIcon,
  SparklesIcon,
  SwordsIcon,
  TrashIcon,
  WrenchIcon,
} from "lucide-react";
import { memo } from "react";
import { useTranslation } from "react-i18next";

import wuwaModFixerIcon from "@/renderer/assets/img/wuwa-mod-fixer-icon.png";
import zzmiModFixerIcon from "@/renderer/assets/img/zzmi-mod-fixer-icon.png";

interface ModCardHeaderProps {
  mod: ModInfo;
  actions: ModActionApi;
}

export const ModCardHeader = memo(function ModCardHeader({ mod, actions }: ModCardHeaderProps) {
  const { t } = useTranslation();
  const isConvertingModel = actions.convertingModelPath === mod.path;

  return (
    <div className="relative z-10 flex items-center justify-between pb-1">
      <span className="truncate text-sm font-semibold">{stripDisabledPrefix(mod.name)}</span>
      <div className="flex items-center gap-1">
        {!actions.isNteGame && (
          <>
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
                onClick={(event) => event.stopPropagation()}
                finalFocus={false}
                className="w-full max-w-52"
              >
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Mod Tools</DropdownMenuLabel>
                  <DropdownMenuItem
                    onClick={(event) => {
                      event.stopPropagation();
                      actions.openTextureResizeDialog(mod);
                    }}
                  >
                    <ImageIcon className="size-4" />
                    {t("page.tools.texture_resizer.title")}
                  </DropdownMenuItem>

                  <DropdownMenuItem
                    onClick={(event) => {
                      event.stopPropagation();
                      actions.openBodyShapeDialog(mod);
                    }}
                  >
                    <PersonStandingIcon className="size-4" />
                    {t("page.tools.body_shape.title")} ({t("g.beta")})
                  </DropdownMenuItem>

                  <DropdownMenuItem
                    onClick={(event) => {
                      event.stopPropagation();
                      actions.openTouchProfileDialog(mod);
                    }}
                  >
                    <SparklesIcon className="size-4" />
                    {t("page.tools.touch_profile.title")} ({t("g.beta")})
                  </DropdownMenuItem>

                  <DropdownMenuItem
                    onClick={(event) => {
                      event.stopPropagation();
                      actions.openConflictFinder(mod);
                    }}
                  >
                    <SwordsIcon className="size-4" />
                    {t("page.mod.context-menu.find-conflict")}
                  </DropdownMenuItem>

                  {actions.runner.modFixer && (
                    <DropdownMenuItem
                      disabled={actions.runner.isPreparing}
                      onClick={(event) => {
                        event.stopPropagation();
                        void actions.openModFixer(mod);
                      }}
                    >
                      <img
                        src={
                          actions.runner.modFixer.id === "zzmi"
                            ? zzmiModFixerIcon
                            : wuwaModFixerIcon
                        }
                        className="size-4"
                      />
                      {actions.runner.modFixer.label}
                    </DropdownMenuItem>
                  )}
                </DropdownMenuGroup>

                <DropdownMenuSeparator />

                <DropdownMenuGroup>
                  <DropdownMenuLabel>Preset ({actions.runner.presets.length})</DropdownMenuLabel>
                  {actions.runner.presets.map((preset) => (
                    <DropdownMenuItem
                      key={preset.id}
                      onClick={(event) => {
                        event.stopPropagation();
                        void actions.runPreset(mod, preset.id);
                      }}
                      className="p-0"
                    >
                      <Tooltip disableHoverablePopup>
                        <TooltipTrigger className="h-full w-full truncate p-1 text-start">
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
                  <DropdownMenuLabel>Fix Tool ({actions.runner.fixTools.length})</DropdownMenuLabel>
                  {actions.runner.fixTools.map((tool) => (
                    <DropdownMenuItem
                      key={tool.id}
                      onClick={(event) => {
                        event.stopPropagation();
                        void actions.runTool(mod, tool.id);
                      }}
                      className="p-0"
                    >
                      <Tooltip disableHoverablePopup>
                        <TooltipTrigger className="h-full w-full truncate p-1 text-start">
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
              onClick={(event) => {
                event.stopPropagation();
                void actions.openModelViewer(mod);
              }}
              title="Model Viewer"
            >
              {isConvertingModel ? <Loader2Icon className="animate-spin" /> : <BoxIcon />}
            </Button>
          </>
        )}

        <Button
          variant="ghost"
          size="icon"
          className="size-7 hover:bg-accent/20"
          onClick={(event) => {
            event.stopPropagation();
            actions.openDeleteMod(mod);
          }}
        >
          <TrashIcon />
        </Button>

        <Button
          variant="ghost"
          size="icon"
          className="size-7 hover:bg-accent/20"
          onClick={(event) => {
            event.stopPropagation();
            void Shell.OpenPath(mod.path);
          }}
        >
          <FolderIcon />
        </Button>
      </div>
    </div>
  );
});
