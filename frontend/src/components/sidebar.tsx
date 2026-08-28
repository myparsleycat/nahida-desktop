import { Shell } from "@bindings/platform";
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
  const { session, isBackendOffline } = useAuth();
  const showDriveNav = !!session;

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
    <div className={`flex w-13 flex-col ${DEFAULT_BG} ${className}`}>
      <div className="flex h-full w-full flex-col select-none">
        <div
          className="dragselect-start-allowed fixed left-2 flex max-h-screen flex-col space-y-2 overflow-x-hidden overflow-y-auto py-2"
          style={{ top: "50%", transform: "translateY(-50%)" }}
        >
          <Tooltip disableHoverablePopup>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-lg"
                  className={getNavButtonClassName(isTransferPage)}
                  aria-current={isTransferPage ? "page" : undefined}
                  onPointerDown={handlePointerDown}
                  onClick={() => {
                    void navi({ to: "/transfer" });
                  }}
                />
              }
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
            </TooltipTrigger>
            <TooltipContent side="right">
              <div className="flex flex-col gap-0.5">
                <span>{t("page.transfer.title")}</span>
                {activeTransferProgress !== null && (
                  <span className="text-background/80">{transferProgressLabel}</span>
                )}
              </div>
            </TooltipContent>
          </Tooltip>

          <Tooltip disableHoverablePopup>
            <TooltipTrigger
              render={
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
                    void navi({ to: "/gamebanana" });
                  }}
                />
              }
            >
              <BananaIcon className={cn(iconSize)} />
            </TooltipTrigger>
            <TooltipContent side="right">GameBanana</TooltipContent>
          </Tooltip>

          <Separator />

          <Tooltip disableHoverablePopup>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-lg"
                  className={getNavButtonClassName(isModPage)}
                  aria-current={isModPage ? "page" : undefined}
                  onPointerDown={handlePointerDown}
                  onClick={() => {
                    void navi({ to: "/mod" });
                  }}
                />
              }
            >
              <GamepadIcon className={cn(iconSize)} />
            </TooltipTrigger>
            <TooltipContent side="right">{t("page.mod.title")}</TooltipContent>
          </Tooltip>

          <Tooltip disableHoverablePopup>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-lg"
                  className={getNavButtonClassName(isToolsPage)}
                  aria-current={isToolsPage ? "page" : undefined}
                  onPointerDown={handlePointerDown}
                  onClick={() => {
                    void navi({ to: "/tools" });
                  }}
                />
              }
            >
              <WrenchIcon className={cn(iconSize)} />
            </TooltipTrigger>
            <TooltipContent side="right">Tools</TooltipContent>
          </Tooltip>

          <Separator orientation="horizontal" />

          {showDriveNav && (
            <>
              <Tooltip disableHoverablePopup>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-lg"
                      className={cn(
                        getNavButtonClassName(isDrivePage),
                        isBackendOffline && "opacity-50",
                      )}
                      aria-current={isDrivePage ? "page" : undefined}
                      aria-disabled={isBackendOffline}
                      onPointerDown={handlePointerDown}
                      onClick={() => {
                        if (isBackendOffline) return;
                        const lastDriveId = viewStore.getState().lastDriveId;
                        void navi({
                          to: "/drive/drive/$id",
                          params: {
                            id: lastDriveId ? lastDriveId : session.drive.rootId,
                          },
                        });
                      }}
                      onDoubleClick={() => {
                        if (isBackendOffline) return;
                        void navi({
                          to: "/drive/drive/$id",
                          params: {
                            id: session.drive.rootId,
                          },
                        });
                      }}
                    />
                  }
                >
                  <HardDriveIcon className={cn(iconSize)} />
                </TooltipTrigger>
                <TooltipContent side="right">
                  {isBackendOffline ? t("page.drive.title_server_error") : t("page.drive.title")}
                </TooltipContent>
              </Tooltip>

              <Tooltip disableHoverablePopup>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-lg"
                      className={cn(
                        getNavButtonClassName(isSharePage),
                        isBackendOffline && "opacity-50",
                      )}
                      aria-current={isSharePage ? "page" : undefined}
                      aria-disabled={isBackendOffline}
                      onPointerDown={handlePointerDown}
                      onClick={() => {
                        if (isBackendOffline) return;
                        const lastShareId = viewStore.getState().lastShareId;
                        void navi({
                          to: "/drive/share/$id",
                          params: {
                            id: lastShareId,
                          },
                        });
                      }}
                      onDoubleClick={() => {
                        if (isBackendOffline) return;
                        void navi({
                          to: "/drive/share/$id",
                          params: {
                            id: "share",
                          },
                        });
                      }}
                    />
                  }
                >
                  <Share2Icon className={cn(iconSize)} />
                </TooltipTrigger>
                <TooltipContent side="right">
                  {isBackendOffline
                    ? t("page.share_drive.title_server_error")
                    : t("page.share_drive.title")}
                </TooltipContent>
              </Tooltip>

              <Separator orientation="horizontal" />
            </>
          )}

          <Tooltip disableHoverablePopup>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-lg"
                  onPointerDown={handlePointerDown}
                  onClick={() => {
                    void Shell.OpenExternal(documentationUrl);
                  }}
                />
              }
            >
              <BookOpenIcon className={cn(iconSize)} />
            </TooltipTrigger>
            <TooltipContent side="right">{t("page.docs.title")}</TooltipContent>
          </Tooltip>

          <Tooltip disableHoverablePopup>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-lg"
                  onPointerDown={handlePointerDown}
                  onClick={() => {
                    void Shell.OpenExternal(
                      "https://github.com/myparsleycat/nahida-desktop/issues",
                    );
                  }}
                />
              }
            >
              <BugIcon className={cn(iconSize)} />
            </TooltipTrigger>
            <TooltipContent side="right">Report</TooltipContent>
          </Tooltip>

          <Tooltip disableHoverablePopup>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-lg"
                  className={getNavButtonClassName(isSettingPage)}
                  aria-current={isSettingPage ? "page" : undefined}
                  onPointerDown={handlePointerDown}
                  onClick={() => {
                    void navi({ to: "/setting/gen" });
                  }}
                />
              }
            >
              <SettingsIcon className={cn(iconSize)} />
            </TooltipTrigger>
            <TooltipContent side="right">{t("page.setting.title")}</TooltipContent>
          </Tooltip>

          {appStatus?.isDev && (
            <Tooltip disableHoverablePopup>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-lg"
                    className={getNavButtonClassName(isTestPage)}
                    aria-current={isTestPage ? "page" : undefined}
                    onPointerDown={handlePointerDown}
                    onClick={() => {
                      void navi({ to: "/test" });
                    }}
                  />
                }
              >
                <BugPlayIcon className={cn(iconSize)} />
              </TooltipTrigger>
              <TooltipContent side="right">{t("page.setting.title")}</TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>
    </div>
  );
}
