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
import type { ModInfo } from "@renderer/types/mod";
import {
  BoxIcon,
  ClipboardIcon,
  FolderIcon,
  FolderTreeIcon,
  ImageIcon,
  Loader2Icon,
  PencilIcon,
  TerminalSquareIcon,
  TrashIcon,
} from "lucide-react";
import { type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import wuwaModFixerIcon from "@/renderer/assets/img/wuwa-mod-fixer-icon.png";

interface ModContextMenuProps {
  mod: ModInfo;
  actions: ModActionApi;
  children: ReactNode;
}

export function ModContextMenu({ mod, actions, children }: ModContextMenuProps) {
  const { t } = useTranslation();
  const isConvertingModel = actions.convertingModelPath === mod.path;

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ContextMenuContent className="w-56">
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
                  <ContextMenuItem key={tool.id} onClick={() => void actions.runTool(mod, tool.id)}>
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
              onClick={() => void actions.openWuwaFixer(mod)}
            >
              <img src={wuwaModFixerIcon} className="size-4" />
              Wuwa Mod Fixer
            </ContextMenuItem>
          )}
        </ContextMenuGroup>
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => {
            void window.api.invoke("util:openCmd", mod.path);
          }}
        >
          <TerminalSquareIcon className="mr-2 size-4" />
          {t("page.mod.context-menu.open-cmd")}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => {
            void window.api.invoke("util:openPath", mod.path);
          }}
        >
          <FolderIcon className="mr-2 size-4" />
          {t("page.mod.context-menu.open-folder")}
        </ContextMenuItem>
        <ContextMenuItem onClick={() => void actions.markAsManualSubGroup(mod)}>
          <FolderTreeIcon className="mr-2 size-4" />
          {t("page.mod.context-menu.mark-manual-subgroup")}
        </ContextMenuItem>
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
        <ContextMenuItem onClick={() => actions.openRenameDialog(mod)}>
          <PencilIcon className="mr-2 size-4" />
          {t("page.mod.context-menu.rename")}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem variant="destructive" onClick={() => actions.openDeleteMod(mod)}>
          <TrashIcon className="mr-2 size-4" />
          {t("g.delete")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
