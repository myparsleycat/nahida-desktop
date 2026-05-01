import { useTransferFilter } from "@renderer/components/transfer/hooks/use-transfer-filter";
import { TransferEmptyState } from "@renderer/components/transfer/transfer-empty-state";
import { TransferItem } from "@renderer/components/transfer/transfer-item";
import { TransferStats } from "@renderer/components/transfer/transfer-stats";
import { TransferToolbar } from "@renderer/components/transfer/transfer-toolbar";
import type { TransferItemProps } from "@renderer/components/transfer/types";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useTitlebar } from "@renderer/hooks/use-titlebar";
import { useGlobalStore } from "@renderer/store/global";
import type { Transfer } from "@shared/types";
import { formatSize, formatTime } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";

export const Route = createFileRoute("/transfer")({
  component: RouteComponent,
});

function mapStatus(
  status: Transfer["status"],
  type: Transfer["type"],
): TransferItemProps["status"] {
  switch (status) {
    case "progress":
      return type === "upload" ? "uploading" : "downloading";
    case "completed":
      return "completed";
    case "error":
      return "failed";
    case "canceled":
      return "failed";
    case "pending":
      return "queued";
    case "paused":
      return "paused";
    case "preparing":
      return "preparing";
    default:
      return "failed";
  }
}

function RouteComponent() {
  const { t } = useTranslation();
  const { Titlebar } = useTitlebar();
  const transfers = useGlobalStore((state) => state.transfers);

  const handleCancel = useCallback((id: string) => {
    window.api.invoke("transfer:cancel", id);
  }, []);

  const handlePause = useCallback((id: string) => {
    window.api.invoke("transfer:pause", id);
  }, []);

  const handleResume = useCallback((id: string) => {
    window.api.invoke("transfer:resume", id);
  }, []);

  const handleRetry = useCallback((id: string) => {
    window.api.invoke("transfer:retry", id);
  }, []);

  const handlePauseAll = useCallback(() => {
    window.api.invoke("transfer:pause-all");
  }, []);

  const handleResumeAll = useCallback(() => {
    window.api.invoke("transfer:resume-all");
  }, []);

  const handleClearCompleted = useCallback(() => {
    window.api.invoke("transfer:clear");
  }, []);

  const transferItems: TransferItemProps[] = transfers.map((t) => {
    const error = "error" in t ? t.error : undefined;

    return {
      id: t.pid,
      fileName: t.name,
      fileSize: formatSize(t.totalSize),
      fileType: "file",
      progress: t.progress,
      speed: `${formatSize(t.speed)}/s`,
      timeRemaining: formatTime(t.eta),
      status: mapStatus(t.status, t.type),
      type: t.type,
      path: t.path,
      totalFiles: t.totalFiles,
      processedFiles: t.transferedFiles,
      failedFiles: t.failedFiles,
      error: typeof error === "string" ? error : undefined,
    };
  });

  const activeUploads = transfers.filter(
    (t) => t.type === "upload" && (t.status === "progress" || t.status === "paused"),
  ).length;
  const activeDownloads = transfers.filter(
    (t) => t.type === "download" && (t.status === "progress" || t.status === "paused"),
  ).length;
  const activeTransfersCount = transfers.filter(
    (t) => t.status === "progress" || t.status === "paused",
  ).length;

  const totalUploadSpeed = transfers
    .filter((t) => t.type === "upload" && t.status === "progress")
    .reduce((acc, curr) => acc + curr.speed, 0);

  const totalDownloadSpeed = transfers
    .filter((t) => t.type === "download" && t.status === "progress")
    .reduce((acc, curr) => acc + curr.speed, 0);

  const totalTransferredBytes = transfers.reduce((acc, curr) => acc + curr.transferedSize, 0);

  const {
    searchQuery,
    setSearchQuery,
    activeTab,
    setActiveTab,
    statusFilter,
    toggleStatusFilter,
    filteredTransfers,
    counts,
  } = useTransferFilter({ transfers: transferItems });

  return (
    <div className="flex h-full flex-col overflow-hidden bg-background">
      <Titlebar title={{ text: t("page.transfer.title"), position: "center" }} />

      <div className="flex-none px-4 pt-4 pb-4 border-b">
        <div className="flex flex-col gap-6">
          <TransferStats
            totalUploads={activeUploads}
            totalDownloads={activeDownloads}
            uploadSpeed={`${formatSize(totalUploadSpeed)}/s`}
            downloadSpeed={`${formatSize(totalDownloadSpeed)}/s`}
            totalTransferred={formatSize(totalTransferredBytes)}
            activeTransfers={activeTransfersCount}
          />
          <TransferToolbar
            activeTab={activeTab}
            onTabChange={setActiveTab}
            counts={counts}
            searchQuery={searchQuery}
            onSearchChange={setSearchQuery}
            statusFilter={statusFilter}
            onToggleStatusFilter={toggleStatusFilter}
            onPauseAll={handlePauseAll}
            onResumeAll={handleResumeAll}
            onClearCompleted={handleClearCompleted}
          />
        </div>
      </div>

      <div className="flex-1 min-h-0">
        <ScrollArea className="h-full w-full">
          <main className="mx-auto px-4 py-4 w-full min-w-0 max-w-full">
            <div className="flex flex-col gap-2 w-full max-w-full min-w-0">
              {filteredTransfers.length === 0 ? (
                <TransferEmptyState activeTab={activeTab} hasSearchQuery={!!searchQuery} />
              ) : (
                filteredTransfers.map((transfer: TransferItemProps) => (
                  <TransferItem
                    key={transfer.id}
                    {...transfer}
                    onPause={handlePause}
                    onResume={handleResume}
                    onCancel={handleCancel}
                    onRetry={handleRetry}
                  />
                ))
              )}
            </div>
          </main>
        </ScrollArea>
      </div>
    </div>
  );
}
