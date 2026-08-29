import { Shell } from "@bindings/platform";
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
    errorCode,
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
    | "errorCode"
  >) => {
    const { t } = useTranslation();

    const isActive = status === "uploading" || status === "downloading";
    const isPaused = status === "paused";
    const isQueued = status === "queued";
    const isFailed = status === "failed";
    const isCompleted = status === "completed";
    const hasFailedFiles = (failedFiles || 0) > 0;
    const canRetry = ![
      "invalid_nte_mod_file",
      "nte_client_upgrade_required",
      "upload_file_too_large",
      "nte_bundle_too_large",
    ].includes(errorCode ?? "");

    return (
      <div className="flex shrink-0 items-center gap-1">
        {(isActive || isPaused || isQueued) && (
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => (isPaused || isQueued ? onResume?.(id) : onPause?.(id))}
          >
            {isPaused || isQueued ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
          </Button>
        )}

        {isFailed && canRetry && (
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => onRetry?.(id)}
          >
            <Play className="h-4 w-4" />
          </Button>
        )}

        {type === "download" && isCompleted && path && (
          <Button
            variant="ghost"
            size="icon"
            className="h-8 w-8 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => {
              void Shell.OpenPath(path);
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
                className="h-8 w-8 shrink-0 text-muted-foreground hover:text-foreground"
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
            {isFailed && canRetry && (
              <DropdownMenuItem onClick={() => onRetry?.(id)}>
                {t("page.transfer.item.dropdown_menu.retry")}
              </DropdownMenuItem>
            )}
            {!isFailed && hasFailedFiles && canRetry && (
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
          className="h-8 w-8 shrink-0 text-muted-foreground hover:text-destructive"
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
    errorCode,
    planPhase,
    planProgress,
  } = props;
  const { t } = useTranslation();

  const isActive = status === "uploading" || status === "downloading";
  const isPaused = status === "paused";
  const isCompleted = status === "completed";
  const isFailed = status === "failed";
  const isPlanning = status === "uploading" && planPhase != null;
  const isFinalizing = isActive && progress >= 100;
  const translatedError = errorCode
    ? t(`page.transfer.item.error.${errorCode}`, { defaultValue: error ?? errorCode })
    : error;

  return (
    <div className="group grid w-full max-w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-4 overflow-hidden rounded-lg border bg-card p-4 transition-all hover:border-accent">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-secondary">
        {type === "upload" ? (
          <ArrowUpFromLine className="size-5 shrink-0" />
        ) : (
          <ArrowDownToLine className="size-5 shrink-0" />
        )}
      </div>

      <div className="flex flex-col gap-2 overflow-hidden">
        <div className="flex w-full items-center justify-between gap-2">
          <div className="min-w-0 flex-1">
            <span className="block w-full truncate font-medium text-foreground" title={fileName}>
              {fileName}
            </span>
          </div>
          {(() => {
            if (isPlanning) {
              return (
                <span className={cn("shrink-0 text-xs font-medium", getStatusColor(status))}>
                  {t(`page.transfer.item.planning.${planPhase}`)}
                </span>
              );
            } else if (isFinalizing) {
              return (
                <span className={cn("shrink-0 text-xs font-medium", getStatusColor(status))}>
                  {t("page.transfer.item.finalizing")}
                </span>
              );
            } else if (
              status === "preparing" ||
              status === "downloading" ||
              status === "uploading"
            ) {
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
                  <DialogContent>{translatedError}</DialogContent>
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

        <Progress value={isPlanning ? (planProgress ?? 0) : progress} className="w-full" />

        <div className="flex w-full items-center justify-between text-xs text-muted-foreground">
          <span className="shrink-0">{fileSize}</span>
          <div className="ml-2 flex min-w-0 items-center gap-3 overflow-hidden">
            <div className="flex min-w-0 items-center gap-2 truncate">
              {isPlanning && (
                <span className="shrink-0 whitespace-nowrap text-blue-400">
                  {t(`page.transfer.item.planning.${planPhase}`)}
                </span>
              )}
              {!isPlanning && !isFinalizing && (isActive || isPaused) && speed && (
                <span className="shrink-0 whitespace-nowrap">{speed}</span>
              )}
              {!isPlanning && !isFinalizing && (isActive || isPaused) && timeRemaining && (
                <span className="hidden truncate whitespace-nowrap sm:inline">
                  {t("page.transfer.item.time_remaining", { time: timeRemaining })}
                </span>
              )}
              {isCompleted && (
                <span className="text-success shrink-0 whitespace-nowrap">
                  {t("page.transfer.item.completed")}
                </span>
              )}
              {failedFiles && failedFiles > 0 ? (
                <span className="shrink-0 whitespace-nowrap text-destructive">
                  {t("page.transfer.item.failed_files", { count: failedFiles })}
                </span>
              ) : (
                isFailed && (
                  <span className="shrink-0 whitespace-nowrap text-destructive">
                    {t("page.transfer.item.failed")}
                  </span>
                )
              )}
            </div>
            <span className="shrink-0 whitespace-nowrap">
              {(isPlanning ? (planProgress ?? 0) : progress).toFixed(2)}%
            </span>
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
        errorCode={errorCode}
      />
    </div>
  );
});

TransferItem.displayName = "TransferItem";
