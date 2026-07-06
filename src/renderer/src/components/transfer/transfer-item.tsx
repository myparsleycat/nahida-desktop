import { Button } from "@renderer/components/ui/button";
import { Dialog, DialogContent, DialogTrigger } from "@renderer/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@renderer/components/ui/dropdown-menu";
import { Progress } from "@renderer/components/ui/progress";
import { cn } from "@renderer/lib/utils";
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  FolderIcon,
  MoreHorizontal,
  Pause,
  Play,
  X,
} from "lucide-react";
import { memo } from "react";
import { useTranslation } from "react-i18next";
import type { TransferItemProps } from "./types";
import { getStatusColor } from "./utils";

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
    const { t } = useTranslation();

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
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon"
                className="h-8 w-8 text-muted-foreground hover:text-foreground shrink-0"
              />
            }
          >
            <MoreHorizontal className="h-4 w-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {!isCompleted && !isFailed && (
              <DropdownMenuItem onClick={() => onCancel?.(id)}>
                {t("page.transfer.item.dropdown_menu.cancel")}
              </DropdownMenuItem>
            )}
            {isFailed && (
              <DropdownMenuItem onClick={() => onRetry?.(id)}>
                {t("page.transfer.item.dropdown_menu.retry")}
              </DropdownMenuItem>
            )}
            {!isFailed && hasFailedFiles && (
              <DropdownMenuItem onClick={() => onRetry?.(id)}>
                {t("page.transfer.item.dropdown_menu.retry_failed_files")}
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onClick={() => onCancel?.(id)}>
              {t("page.transfer.item.dropdown_menu.remove_from_transfer")}
            </DropdownMenuItem>
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
    error,
  } = props;
  const { t } = useTranslation();

  const isActive = status === "uploading" || status === "downloading";
  const isPaused = status === "paused";
  const isCompleted = status === "completed";
  const isFailed = status === "failed";

  return (
    <div className="group grid grid-cols-[auto_minmax(0,1fr)_auto] w-full max-w-full items-center gap-4 rounded-lg border bg-card p-4 transition-all overflow-hidden hover:border-accent">
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
          {(() => {
            if (status === "preparing" || status === "downloading" || status === "uploading") {
              return (
                <span className={cn("shrink-0 text-xs font-medium", getStatusColor(status))}>
                  {totalFiles && processedFiles !== undefined
                    ? `${t(`page.transfer.item.${status}`)} (${processedFiles}/${totalFiles})`
                    : t(`page.transfer.item.${status}`)}
                </span>
              );
            } else if (status === "failed") {
              return (
                <Dialog>
                  <DialogTrigger>
                    <span className={cn("shrink-0 text-xs font-medium", getStatusColor(status))}>
                      {t(`page.transfer.item.${status}`)}
                    </span>
                  </DialogTrigger>
                  <DialogContent>{error}</DialogContent>
                </Dialog>
              );
            } else {
              return (
                <span className={cn("shrink-0 text-xs font-medium", getStatusColor(status))}>
                  {t(`page.transfer.item.${status}`)}
                </span>
              );
            }
          })()}
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
