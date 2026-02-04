import React, { useState, memo } from "react";
import {
  ArrowUpFromLine,
  ArrowDownToLine,
  X,
  Pause,
  Play,
  MoreHorizontal,
  FolderIcon,
} from "lucide-react";
import { Button } from "@renderer/components/ui/button";
import { Progress } from "@renderer/components/ui/progress";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { cn } from "@renderer/lib/utils";
import { TransferItemProps } from "./types";
import { getFileIcon, getStatusColor } from "./utils";
import { useTranslation } from "react-i18next";

const TransferItemActions = memo(
  ({
    id,
    status,
    type,
    path,
    onPause,
    onResume,
    onCancel,
    onRetry,
    failedFiles,
  }: Pick<
    TransferItemProps,
    | "id"
    | "status"
    | "type"
    | "path"
    | "onPause"
    | "onResume"
    | "onCancel"
    | "onRetry"
    | "failedFiles"
  >) => {
    const isActive = status === "uploading" || status === "downloading";
    const isPaused = status === "paused";
    const isQueued = status === "queued";
    const isFailed = status === "failed";
    const isCompleted = status === "completed";
    const hasFailedFiles = (failedFiles || 0) > 0;

    return (
      <div className="flex shrink-0 items-center gap-1">
        {(isActive || isPaused || isQueued) && (
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 text-muted-foreground hover:text-foreground shrink-0"
            onClick={() => (isPaused || isQueued ? onResume?.(id) : onPause?.(id))}
          >
            {isPaused || isQueued ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
          </Button>
        )}

        {isFailed && (
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 text-muted-foreground hover:text-foreground shrink-0"
            onClick={() => onRetry?.(id)}
          >
            <Play className="h-4 w-4" />
          </Button>
        )}

        {type === "download" && isCompleted && path && (
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 text-muted-foreground hover:text-foreground shrink-0"
            onClick={() => {
              window.api.invoke("util:openPath", path);
            }}
          >
            <FolderIcon className="h-4 w-4" />
          </Button>
        )}

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 text-muted-foreground hover:text-foreground shrink-0"
            >
              <MoreHorizontal className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {!isCompleted && !isFailed && (
              <DropdownMenuItem onClick={() => onCancel?.(id)}>취소</DropdownMenuItem>
            )}
            {isFailed && <DropdownMenuItem onClick={() => onRetry?.(id)}>재시작</DropdownMenuItem>}
            {!isFailed && hasFailedFiles && (
              <DropdownMenuItem onClick={() => onRetry?.(id)}>실패한 파일 재시도</DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={() => onCancel?.(id)}>전송에서 제거</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 text-muted-foreground hover:text-destructive shrink-0"
          onClick={() => onCancel?.(id)}
        >
          <X className="h-4 w-4" />
        </Button>
      </div>
    );
  },
);

TransferItemActions.displayName = "TransferItemActions";

export const TransferItem = memo((props: TransferItemProps) => {
  const {
    id,
    fileName,
    fileSize,
    fileType,
    progress,
    speed,
    timeRemaining,
    status,
    type,
    onPause,
    onResume,
    onCancel,
    onRetry,
    totalFiles,
    processedFiles,
    failedFiles,
  } = props;
  const { t } = useTranslation();

  const [isHovered, setIsHovered] = useState(false);
  const isActive = status === "uploading" || status === "downloading";
  const isPaused = status === "paused";
  const isCompleted = status === "completed";
  const isFailed = status === "failed";

  return (
    <div
      className={cn(
        "group grid grid-cols-[auto_minmax(0,1fr)_auto] w-full max-w-full items-center gap-4 rounded-lg border border-border bg-card p-4 transition-all overflow-hidden",
        isHovered && "border-muted-foreground/30 bg-accent/30",
      )}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
    >
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-secondary">
        {type === "upload" ? (
          <ArrowUpFromLine className="size-5 shrink-0" />
        ) : (
          <ArrowDownToLine className="size-5 shrink-0" />
        )}
      </div>

      <div className="flex flex-col gap-2 overflow-hidden">
        <div className="flex items-center justify-between gap-2 w-full">
          <div className="flex-1 min-w-0">
            <span className="block truncate font-medium text-foreground w-full" title={fileName}>
              {fileName}
            </span>
          </div>
          <span className={cn("shrink-0 text-xs font-medium", getStatusColor(status))}>
            {(status === "preparing" || status === "downloading") &&
            totalFiles &&
            processedFiles !== undefined
              ? `${status.charAt(0).toUpperCase() + status.slice(1)} (${processedFiles}/${totalFiles})`
              : status.charAt(0).toUpperCase() + status.slice(1)}
          </span>
        </div>

        <Progress value={progress} className="w-full" />

        <div className="flex items-center justify-between text-xs text-muted-foreground w-full">
          <span className="shrink-0">{fileSize}</span>
          <div className="flex min-w-0 items-center gap-3 overflow-hidden ml-2">
            <div className="flex items-center gap-2 truncate min-w-0">
              {(isActive || isPaused) && speed && (
                <span className="shrink-0 whitespace-nowrap">{speed}</span>
              )}
              {(isActive || isPaused) && timeRemaining && (
                <span className="truncate whitespace-nowrap hidden sm:inline">
                  {t("page.transfer.item.time_remaining", { time: timeRemaining })}
                </span>
              )}
              {isCompleted && (
                <span className="text-success shrink-0 whitespace-nowrap">
                  {t("page.transfer.item.completed")}
                </span>
              )}
              {failedFiles && failedFiles > 0 ? (
                <span className="text-destructive shrink-0 whitespace-nowrap">
                  {t("page.transfer.item.failed_files", { count: failedFiles })}
                </span>
              ) : (
                isFailed && (
                  <span className="text-destructive shrink-0 whitespace-nowrap">
                    {t("page.transfer.item.failed")}
                  </span>
                )
              )}
            </div>
            <span className="shrink-0 whitespace-nowrap">{progress.toFixed(2)}%</span>
          </div>
        </div>
      </div>

      <TransferItemActions
        id={id}
        status={status}
        type={type}
        path={props.path}
        onPause={onPause}
        onResume={onResume}
        onCancel={onCancel}
        onRetry={onRetry}
        failedFiles={failedFiles}
      />
    </div>
  );
});

TransferItem.displayName = "TransferItem";
