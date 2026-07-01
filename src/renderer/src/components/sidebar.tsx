import { DEFAULT_BG } from "@renderer/const";
import { useAuth } from "@renderer/hooks/use-auth";
import { cn } from "@renderer/lib/utils";
import { viewStore } from "@renderer/store/drive";
import { gameBananaStore } from "@renderer/store/gamebanana";
import { useGlobalStore } from "@renderer/store/global";
import { getAggregateTransferProgress } from "@shared/transfer-progress";
import { useLocation, useNavigate } from "@tanstack/react-router";
import {
  ArrowUpDownIcon,
  BananaIcon,
  BookOpenIcon,
  BugIcon,
  BugPlayIcon,
  GamepadIcon,
  HardDriveIcon,
  SettingsIcon,
  Share2Icon,
  WrenchIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "./ui/button";
import { Separator } from "./ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";

function getDocumentationUrl(language: string) {
  switch (language) {
    case "ko":
      return "https://desktop.nahida.live/ko/";
    case "ja":
      return "https://desktop.nahida.live/ja/";
    case "zh":
      return "https://desktop.nahida.live/zh-CN/";
    default:
      return "https://desktop.nahida.live";
  }
}

export function Sidebar({ className }: { className?: string }) {
  const navi = useNavigate();
  const location = useLocation();
  const { t, i18n } = useTranslation();
  const appStatus = useGlobalStore((state) => state.appStatus);
  const transfers = useGlobalStore((state) => state.transfers);
  const { session } = useAuth();

  const handlePointerDown = (e: React.PointerEvent) => {
    e.preventDefault();
  };

  const iconSize = "size-7";
  const activeTransferProgress = getAggregateTransferProgress(transfers);
  const transferProgressLabel =
    activeTransferProgress === null ? null : `${Math.round(activeTransferProgress)}%`;
  const transferProgressRadius = 15;
  const transferProgressCircumference = 2 * Math.PI * transferProgressRadius;
  const transferProgressOffset =
    activeTransferProgress === null
      ? transferProgressCircumference
      : transferProgressCircumference * (1 - activeTransferProgress / 100);
  const pathname = location.pathname;
  const isTransferPage = pathname.startsWith("/transfer");
  const isDrivePage = pathname.startsWith("/drive/drive");
  const isSharePage = pathname.startsWith("/drive/share");
  const isModPage = pathname.startsWith("/mod");
  const isToolsPage = pathname.startsWith("/tools");
  const isGameBananaPage = pathname.startsWith("/gamebanana");
  const isSettingPage = pathname.startsWith("/setting");
  const isTestPage = pathname.startsWith("/test");
  const documentationUrl = getDocumentationUrl(i18n.language);
  const getNavButtonClassName = (isActive: boolean) =>
    cn("relative overflow-visible", isActive && "text-accent hover:text-accent");

  return (
    <div className={`w-13 flex flex-col ${DEFAULT_BG} ${className}`}>
      <div className="w-full flex flex-col select-none h-full">
        <div
          className="fixed left-2 flex flex-col overflow-y-auto overflow-x-hidden dragselect-start-allowed py-2 space-y-2 max-h-screen"
          style={{ top: "50%", transform: "translateY(-50%)" }}
        >
          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-lg"
                className={getNavButtonClassName(isTransferPage)}
                aria-current={isTransferPage ? "page" : undefined}
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({ to: "/transfer" });
                }}
              >
                {activeTransferProgress !== null && (
                  <>
                    <svg
                      className="pointer-events-none absolute inset-0 size-full -rotate-90"
                      viewBox="0 0 32 32"
                      aria-hidden="true"
                    >
                      <circle
                        cx="16"
                        cy="16"
                        r={transferProgressRadius}
                        className="fill-none stroke-border/70"
                        strokeWidth="2"
                      />
                      <circle
                        cx="16"
                        cy="16"
                        r={transferProgressRadius}
                        className="fill-none stroke-primary transition-all duration-300"
                        strokeWidth="2"
                        strokeLinecap="round"
                        strokeDasharray={transferProgressCircumference}
                        strokeDashoffset={transferProgressOffset}
                      />
                    </svg>
                    {/* <span className="pointer-events-none absolute -right-1 -bottom-1 rounded-full bg-primary px-1.5 py-0.5 text-[9px] leading-none font-semibold text-primary-foreground shadow-sm">
                      {transferProgressLabel}
                    </span> */}
                  </>
                )}
                <ArrowUpDownIcon
                  className={cn(
                    iconSize,
                    "transition-all duration-300",
                    activeTransferProgress !== null && "scale-90",
                  )}
                />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              <div className="flex flex-col gap-0.5">
                <span>{t("page.transfer.title")}</span>
                {activeTransferProgress !== null && (
                  <span className="text-background/80">{transferProgressLabel}</span>
                )}
              </div>
            </TooltipContent>
          </Tooltip>

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-lg"
                className={cn(
                  getNavButtonClassName(isGameBananaPage),
                  isGameBananaPage && "text-yellow-500 hover:text-yellow-500",
                )}
                aria-current={isGameBananaPage ? "page" : undefined}
                onPointerDown={handlePointerDown}
                onClick={() => {
                  if (isModPage) {
                    gameBananaStore.getState().requestModGameSync();
                  }
                  navi({ to: "/gamebanana" });
                }}
              >
                <BananaIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              GameBanana
            </TooltipContent>
          </Tooltip>

          <Separator />

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-lg"
                className={getNavButtonClassName(isModPage)}
                aria-current={isModPage ? "page" : undefined}
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

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-lg"
                className={getNavButtonClassName(isToolsPage)}
                aria-current={isToolsPage ? "page" : undefined}
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({ to: "/tools" });
                }}
              >
                <WrenchIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              Tools
            </TooltipContent>
          </Tooltip>

          <Separator orientation="horizontal" />

          {session && (
            <>
              <Tooltip disableHoverableContent={true}>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-lg"
                    className={getNavButtonClassName(isDrivePage)}
                    aria-current={isDrivePage ? "page" : undefined}
                    onPointerDown={handlePointerDown}
                    onClick={() => {
                      const lastDriveId = viewStore.getState().lastDriveId;
                      navi({
                        to: "/drive/drive/$id",
                        params: {
                          id: lastDriveId ? lastDriveId : session.drive.rootId,
                        },
                      });
                    }}
                    onDoubleClick={() => {
                      navi({
                        to: "/drive/drive/$id",
                        params: {
                          id: session.drive.rootId,
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
                    size="icon-lg"
                    className={getNavButtonClassName(isSharePage)}
                    aria-current={isSharePage ? "page" : undefined}
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
                size="icon-lg"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  void window.api.invoke("util:openExternal", documentationUrl);
                }}
              >
                <BookOpenIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              {t("page.docs.title")}
            </TooltipContent>
          </Tooltip>

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-lg"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  void window.api.invoke(
                    "util:openExternal",
                    "https://github.com/myparsleycat/nahida-desktop/issues",
                  );
                }}
              >
                <BugIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              Report
            </TooltipContent>
          </Tooltip>

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-lg"
                className={getNavButtonClassName(isSettingPage)}
                aria-current={isSettingPage ? "page" : undefined}
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({ to: "/setting/gen" });
                }}
              >
                <SettingsIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              {t("page.setting.title")}
            </TooltipContent>
          </Tooltip>

          {appStatus?.isDev && (
            <Tooltip disableHoverableContent={true}>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-lg"
                  className={getNavButtonClassName(isTestPage)}
                  aria-current={isTestPage ? "page" : undefined}
                  onPointerDown={handlePointerDown}
                  onClick={() => {
                    navi({ to: "/test" });
                  }}
                >
                  <BugPlayIcon className={cn(iconSize)} />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="right" hideWhenDetached={true}>
                {t("page.setting.title")}
              </TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>
    </div>
  );
}
