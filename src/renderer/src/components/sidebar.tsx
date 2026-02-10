import { cn } from "@renderer/lib/utils";
import { viewStore } from "@renderer/store/drive";
import { useGlobalStore } from "@renderer/store/global";
import { useNavigate } from "@tanstack/react-router";
import {
  ArrowUpDownIcon,
  DatabaseBackupIcon,
  GamepadIcon,
  HardDriveIcon,
  PackageIcon,
  SettingsIcon,
  Share2Icon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "./ui/button";
import { Separator } from "./ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";

export function Sidebar({ className }: { className?: string }) {
  const navi = useNavigate();
  const { t } = useTranslation();
  const appStatus = useGlobalStore((state) => state.appStatus);
  const session = useGlobalStore((state) => state.session);

  const handlePointerDown = (e: React.PointerEvent) => {
    e.preventDefault();
  };

  const iconSize = "size-6";

  return (
    <div className={`flex flex-col border-r ${className}`}>
      <div className="w-full flex flex-col h-full select-none">
        <div className="flex flex-col overflow-y-auto overflow-x-hidden dragselect-start-allowed p-2 space-y-2">
          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({ to: "/transfer" });
                }}
              >
                <ArrowUpDownIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              {t("page.transfer.title")}
            </TooltipContent>
          </Tooltip>

          {session && (
            <>
              <Tooltip disableHoverableContent={true}>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    onPointerDown={handlePointerDown}
                    onClick={() => {
                      const lastDriveId = viewStore.getState().lastDriveId;
                      navi({
                        to: "/drive/drive/$id",
                        params: {
                          id: lastDriveId,
                        },
                      });
                    }}
                    onDoubleClick={() => {
                      navi({
                        to: "/drive/drive/$id",
                        params: {
                          id: "root",
                        },
                      });
                    }}
                  >
                    <HardDriveIcon className={cn(iconSize)} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="right" hideWhenDetached={true}>
                  {t("page.drive.title")}
                </TooltipContent>
              </Tooltip>

              <Tooltip disableHoverableContent={true}>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    onPointerDown={handlePointerDown}
                    onClick={() => {
                      const lastShareId = viewStore.getState().lastShareId;
                      navi({
                        to: "/drive/share/$id",
                        params: {
                          id: lastShareId,
                        },
                      });
                    }}
                    onDoubleClick={() => {
                      navi({
                        to: "/drive/share/$id",
                        params: {
                          id: "share",
                        },
                      });
                    }}
                  >
                    <Share2Icon className={cn(iconSize)} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="right" hideWhenDetached={true}>
                  {t("page.share_drive.title")}
                </TooltipContent>
              </Tooltip>

              <Separator orientation="horizontal" />
            </>
          )}

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({ to: "/mod" });
                }}
              >
                <GamepadIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              {t("page.mod.title")}
            </TooltipContent>
          </Tooltip>

          {appStatus?.isDev && (
            <>
              <Tooltip disableHoverableContent={true}>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    onPointerDown={handlePointerDown}
                    onClick={() => {
                      navi({ to: "/xxmi" });
                    }}
                  >
                    <PackageIcon className={cn(iconSize)} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="right" hideWhenDetached={true}>
                  XXMI
                </TooltipContent>
              </Tooltip>

              <Tooltip disableHoverableContent={true}>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    onPointerDown={handlePointerDown}
                    onClick={() => {
                      navi({ to: "/backup" });
                    }}
                  >
                    <DatabaseBackupIcon className={cn(iconSize)} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="right" hideWhenDetached={true}>
                  {t("page.backup.title")}
                </TooltipContent>
              </Tooltip>
            </>
          )}

          <Separator orientation="horizontal" />

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  window.api.invoke("window:openSetting");
                }}
              >
                <SettingsIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              {t("page.setting.title")}
            </TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  );
}
