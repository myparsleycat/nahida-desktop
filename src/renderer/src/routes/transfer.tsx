import { useTransferFilter } from "@renderer/components/transfer/hooks/use-transfer-filter";
import { TransferEmptyState } from "@renderer/components/transfer/transfer-empty-state";
import { TransferItem } from "@renderer/components/transfer/transfer-item";
import { TransferStats } from "@renderer/components/transfer/transfer-stats";
import { TransferToolbar } from "@renderer/components/transfer/transfer-toolbar";
import type { TransferItemProps } from "@renderer/components/transfer/types";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { useGlobalStore } from "@renderer/store/global";
import type { Transfer } from "@shared/types";
import { formatSize, formatTime } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import { useCallback } from "react";

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
  const transfers = useGlobalStore((state) => state.transfers);

  const handleCancel = useCallback((id: string) => {
    void window.api.invoke("transfer:cancel", id);
  }, []);

  const handlePause = useCallback((id: string) => {
    void window.api.invoke("transfer:pause", id);
  }, []);

  const handleResume = useCallback((id: string) => {
    void window.api.invoke("transfer:resume", id);
  }, []);

  const handleRetry = useCallback((id: string) => {
    void window.api.invoke("transfer:retry", id);
  }, []);

  const handlePauseAll = useCallback(() => {
    void window.api.invoke("transfer:pause-all");
  }, []);

  const handleResumeAll = useCallback(() => {
    void window.api.invoke("transfer:resume-all");
  }, []);

  const handleClearCompleted = useCallback(() => {
    void window.api.invoke("transfer:clear");
  }, []);

  const transferItems: TransferItemProps[] = transfers.map((t) => {
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
      error: t.error,
      errorCode: t.errorCode,
      planPhase: t.planPhase,
      planProgress: t.planProgress,
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
      <div className="flex-none border-b px-4 pt-4 pb-4">
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

      <div className="min-h-0 flex-1">
        <ScrollArea className="h-full w-full">
          <main className="mx-auto w-full max-w-full min-w-0 px-4 py-4">
            <div className="flex w-full max-w-full min-w-0 flex-col gap-2">
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
