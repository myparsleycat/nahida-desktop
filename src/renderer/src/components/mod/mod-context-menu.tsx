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
import type { ModActionApi } from "@renderer/hooks/use-mod-actions";
import { Logger } from "@renderer/lib/logger";
import type { ModInfo } from "@renderer/types/mod";
import { useNavigate } from "@tanstack/react-router";
import {
  BoxIcon,
  ClipboardIcon,
  ExternalLinkIcon,
  FolderIcon,
  FolderTreeIcon,
  ImageIcon,
  Loader2Icon,
  PencilIcon,
  PersonStandingIcon,
  SparklesIcon,
  SwordsIcon,
  TerminalSquareIcon,
  TrashIcon,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";

import wuwaModFixerIcon from "@/renderer/assets/img/wuwa-mod-fixer-icon.png";

interface ModContextMenuProps {
  mod: ModInfo;
  actions: ModActionApi;
  children: ReactNode;
}

export function ModContextMenu({ mod, actions, children }: ModContextMenuProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [gameBananaSource, setGameBananaSource] = useState<{
    modPath: string;
    modId?: number;
  } | null>(null);
  const gameBananaModId =
    gameBananaSource?.modPath === mod.path ? gameBananaSource.modId : undefined;
  const isConvertingModel = actions.convertingModelPath === mod.path;

  const loadGameBananaModId = () => {
    if (gameBananaSource?.modPath === mod.path) return;

    const modPath = mod.path;
    setGameBananaSource({ modPath });
    void window.api
      .invoke("mod:getGameBananaModId", modPath)
      .then((modId) => setGameBananaSource({ modPath, modId }))
      .catch((error) => {
        Logger.error(error, "ModContextMenu:loadGameBananaModId");
        setGameBananaSource((source) => (source?.modPath === modPath ? null : source));
      });
  };

  return (
    <ContextMenu
      onOpenChange={(open) => {
        if (open) loadGameBananaModId();
      }}
    >
      <ContextMenuTrigger onPointerEnter={loadGameBananaModId}>{children}</ContextMenuTrigger>
      <ContextMenuContent className="w-56">
        {gameBananaModId && (
          <>
            <ContextMenuItem
              onClick={() =>
                void navigate({
                  to: "/gamebanana",
                  search: { mod: gameBananaModId },
                })
              }
            >
              <ExternalLinkIcon className="mr-2 size-4" />
              {t("page.mod.context-menu.open-gamebanana")}
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        {(mod.preview?.match(/\.(jpeg|jpg|gif|png|webp|bmp|mp4|webm|ogg)$/i) ?? false) && (
          <>
            <ContextMenuGroup>
              <ContextMenuLabel>Preview</ContextMenuLabel>
              <ContextMenuItem
                onClick={() => {
                  if (!mod.preview) {
                    return;
                  }

                  void window.api.invoke("util:openExternal", mod.preview);
                }}
              >
                <ImageIcon className="mr-2 size-4" />
                {t("page.mod.context-menu.open-preview-viewer")}
              </ContextMenuItem>
              <ContextMenuItem onClick={() => actions.openPastePreview(mod)}>
                <ClipboardIcon className="mr-2 size-4" />
                {t("page.mod.context-menu.paste-preview")}
              </ContextMenuItem>
              <ContextMenuItem variant="destructive" onClick={() => actions.openDeletePreview(mod)}>
                <TrashIcon className="mr-2 size-4" />
                {t("page.mod.context-menu.delete-preview")}
              </ContextMenuItem>
            </ContextMenuGroup>
            <ContextMenuSeparator />
          </>
        )}
        {!mod.preview && (
          <>
            <ContextMenuGroup>
              <ContextMenuLabel>Preview</ContextMenuLabel>
              <ContextMenuItem onClick={() => actions.openPastePreview(mod)}>
                <ClipboardIcon className="mr-2 size-4" />
                {t("page.mod.context-menu.paste-preview")}
              </ContextMenuItem>
            </ContextMenuGroup>
            <ContextMenuSeparator />
          </>
        )}
        {!actions.isNteGame && (
          <>
            <ContextMenuGroup>
              <ContextMenuLabel>Fix</ContextMenuLabel>
              <ContextMenuSub>
                <ContextMenuSubTrigger>Preset</ContextMenuSubTrigger>
                <ContextMenuSubContent>
                  <ContextMenuGroup>
                    {actions.runner.presets.map((preset) => (
                      <ContextMenuItem
                        key={preset.id}
                        onClick={() => void actions.runPreset(mod, preset.id)}
                      >
                        {preset.name}
                      </ContextMenuItem>
                    ))}
                    {actions.runner.presets.length === 0 && (
                      <ContextMenuItem disabled>No Presets</ContextMenuItem>
                    )}
                  </ContextMenuGroup>
                </ContextMenuSubContent>
              </ContextMenuSub>
              <ContextMenuSub>
                <ContextMenuSubTrigger>Fix Tool</ContextMenuSubTrigger>
                <ContextMenuSubContent>
                  <ContextMenuGroup>
                    {actions.runner.fixTools.map((tool) => (
                      <ContextMenuItem
                        key={tool.id}
                        onClick={() => void actions.runTool(mod, tool.id)}
                      >
                        {tool.name}
                      </ContextMenuItem>
                    ))}
                    {actions.runner.fixTools.length === 0 && (
                      <ContextMenuItem disabled>No Fix Tools</ContextMenuItem>
                    )}
                  </ContextMenuGroup>
                </ContextMenuSubContent>
              </ContextMenuSub>
              {actions.runner.showWuwaFixer && (
                <ContextMenuItem
                  disabled={actions.runner.isPreparing}
                  onClick={() => actions.openWuwaFixer(mod)}
                >
                  <img src={wuwaModFixerIcon} className="size-4" />
                  Wuwa Mod Fixer
                </ContextMenuItem>
              )}
            </ContextMenuGroup>
            <ContextMenuSeparator />
          </>
        )}
        <ContextMenuGroup>
          <ContextMenuLabel>{t("page.mod.context-menu.group-mod")}</ContextMenuLabel>
          {!actions.isNteGame && (
            <ContextMenuItem
              onClick={() => {
                void window.api.invoke("util:openCmd", mod.path);
              }}
            >
              <TerminalSquareIcon className="mr-2 size-4" />
              {t("page.mod.context-menu.open-cmd")}
            </ContextMenuItem>
          )}
          <ContextMenuItem
            onClick={() => {
              void window.api.invoke("util:openPath", mod.path);
            }}
          >
            <FolderIcon className="mr-2 size-4" />
            {t("page.mod.context-menu.open-folder")}
          </ContextMenuItem>
          {!actions.isNteGame && (
            <ContextMenuItem onClick={() => void actions.markAsManualSubGroup(mod)}>
              <FolderTreeIcon className="mr-2 size-4" />
              {t("page.mod.context-menu.mark-manual-subgroup")}
            </ContextMenuItem>
          )}
          <ContextMenuItem onClick={() => actions.openRenameDialog(mod)}>
            <PencilIcon className="mr-2 size-4" />
            {t("page.mod.context-menu.rename")}
          </ContextMenuItem>
        </ContextMenuGroup>
        {!actions.isNteGame && (
          <>
            <ContextMenuSeparator />
            <ContextMenuGroup>
              <ContextMenuLabel>{t("page.mod.context-menu.group-tools")}</ContextMenuLabel>
              <ContextMenuItem
                disabled={isConvertingModel}
                onClick={() => void actions.openModelViewer(mod)}
              >
                {isConvertingModel ? (
                  <Loader2Icon className="mr-2 size-4 animate-spin" />
                ) : (
                  <BoxIcon className="mr-2 size-4" />
                )}
                {t("page.tools.model_viewer.title")}
              </ContextMenuItem>
              <ContextMenuItem onClick={() => actions.openTextureResizeDialog(mod)}>
                <ImageIcon className="mr-2 size-4" />
                {t("page.tools.texture_resizer.title")}
              </ContextMenuItem>
              <ContextMenuItem onClick={() => actions.openBodyShapeDialog(mod)}>
                <PersonStandingIcon className="mr-2 size-4" />
                {t("page.tools.body_shape.title")} ({t("g.beta")})
              </ContextMenuItem>
              <ContextMenuItem onClick={() => actions.openTouchProfileDialog(mod)}>
                <SparklesIcon className="mr-2 size-4" />
                {t("page.tools.touch_profile.title")} ({t("g.beta")})
              </ContextMenuItem>
              <ContextMenuItem onClick={() => actions.openConflictFinder(mod)}>
                <SwordsIcon className="mr-2 size-4" />
                {t("page.mod.context-menu.find-conflict")}
              </ContextMenuItem>
            </ContextMenuGroup>
            <ContextMenuSeparator />
          </>
        )}
        {actions.isNteGame && <ContextMenuSeparator />}
        <ContextMenuItem variant="destructive" onClick={() => actions.openDeleteMod(mod)}>
          <TrashIcon className="mr-2 size-4" />
          {t("g.delete")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
