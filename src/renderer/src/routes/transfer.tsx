import { formatSize, formatTime } from "@shared/utils";
import { createFileRoute } from "@tanstack/react-router";
import { Transfer, TransferWithoutData } from "@shared/types";
import { useEffect, useState, useCallback } from "react";
import { TransferList } from "@renderer/components/transfer/transfer-list";
import { TransferStats } from "@renderer/components/transfer/transfer-stats";
import type { TransferItemProps } from "@renderer/components/transfer/types";
import { ScrollArea } from "@renderer/components/ui/scroll-area";
import { Titlebar } from "@renderer/components/titlebar";

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
  const [transfers, setTransfers] = useState<TransferWithoutData[]>([]);

  useEffect(() => {
    window.api.invoke("transfer:list").then(setTransfers);

    const removeListener = window.api.on("transfer:update", (updatedTransfers) => {
      setTransfers(updatedTransfers);
    });

    return () => {
      removeListener();
    };
  }, []);

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

  const transferItems: TransferItemProps[] = transfers.map((t) => ({
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
  }));

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

  return (
    <>
      <Titlebar title={{ text: "전송", position: "center" }} />
      <ScrollArea className="h-full w-full max-w-full">
        <div className="bg-background w-full min-w-0 max-w-[100vw] overflow-x-hidden">
          <main className="mx-auto px-4 py-6 w-full min-w-0 max-w-full">
            <div className="flex flex-col gap-6 w-full min-w-0 max-w-full">
              <TransferStats
                totalUploads={activeUploads}
                totalDownloads={activeDownloads}
                uploadSpeed={`${formatSize(totalUploadSpeed)}/s`}
                downloadSpeed={`${formatSize(totalDownloadSpeed)}/s`}
                totalTransferred={formatSize(totalTransferredBytes)}
                activeTransfers={activeTransfersCount}
              />

              <TransferList
                transfers={transferItems}
                onPause={handlePause}
                onResume={handleResume}
                onCancel={handleCancel}
                onRetry={handleRetry}
                onPauseAll={handlePauseAll}
                onResumeAll={handleResumeAll}
                onClearCompleted={handleClearCompleted}
              />
            </div>
          </main>
        </div>
      </ScrollArea>
    </>
  );
}
