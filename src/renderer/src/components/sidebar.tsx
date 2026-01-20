import {
  ArrowUpDownIcon,
  GamepadIcon,
  HardDriveIcon,
  HomeIcon,
  SettingsIcon,
  Share2Icon,
} from "lucide-react";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { cn } from "@renderer/lib/utils";
import { Button } from "./ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/tooltip";
import { Separator } from "./ui/separator";

export function Sidebar({ className }: { className?: string }) {
  const navi = useNavigate();
  const location = useLocation();

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
                isActive={location.pathname.startsWith("/transfer")}
              >
                <ArrowUpDownIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              전송
            </TooltipContent>
          </Tooltip>

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({
                    to: "/drive/drive/$id",
                    params: {
                      id: "root",
                    },
                  });
                }}
                isActive={location.pathname.startsWith("/drive/drive/")}
              >
                <HardDriveIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              드라이브
            </TooltipContent>
          </Tooltip>

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({
                    to: "/drive/share/$id",
                    params: {
                      id: "share",
                    },
                  });
                }}
                isActive={location.pathname.startsWith("/drive/share/")}
              >
                <Share2Icon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              공유 드라이브
            </TooltipContent>
          </Tooltip>

          <Separator orientation="horizontal" />

          <Tooltip disableHoverableContent={true}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onPointerDown={handlePointerDown}
                onClick={() => {
                  navi({ to: "/mod" });
                }}
                isActive={location.pathname.startsWith("/mod")}
              >
                <GamepadIcon className={cn(iconSize)} />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="right" hideWhenDetached={true}>
              모드
            </TooltipContent>
          </Tooltip>

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
              설정
            </TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  );
}
