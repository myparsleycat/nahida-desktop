type TransferProgressStatus =
    | "pending"
    | "preparing"
    | "progress"
    | "completed"
    | "paused"
    | "canceled"
    | "error";

export type TransferProgressInput = {
    status: TransferProgressStatus;
    totalSize: number;
    transferedSize: number;
    progress: number;
    queueGroupId?: number;
};

export function isOpenTransferQueueStatus(status: TransferProgressStatus) {
    return status === "preparing" || status === "pending" || status === "progress";
}

export function isAggregateTransferQueueStatus(status: TransferProgressStatus) {
    return isOpenTransferQueueStatus(status) || status === "completed";
}

export function getAggregateTransferProgress(transfers: TransferProgressInput[]): number | null {
    const openQueueGroupIds = new Set(
        transfers
            .filter((transfer) => isOpenTransferQueueStatus(transfer.status))
            .map((transfer) => transfer.queueGroupId)
            .filter((queueGroupId): queueGroupId is number => queueGroupId !== undefined),
    );

    const scopedTransfers = transfers.filter((transfer) => {
        if (!isAggregateTransferQueueStatus(transfer.status)) return false;
        if (openQueueGroupIds.size === 0) return isOpenTransferQueueStatus(transfer.status);
        return transfer.queueGroupId !== undefined && openQueueGroupIds.has(transfer.queueGroupId);
    });

    if (scopedTransfers.length === 0) return null;

    const totalSize = scopedTransfers.reduce((sum, transfer) => sum + transfer.totalSize, 0);
    if (totalSize > 0) {
        const transferredSize = scopedTransfers.reduce((sum, transfer) => {
            if (transfer.status === "completed") return sum + transfer.totalSize;
            return sum + Math.max(0, Math.min(transfer.transferedSize, transfer.totalSize));
        }, 0);

        return Math.max(0, Math.min(100, (transferredSize / totalSize) * 100));
    }

    const totalProgress = scopedTransfers.reduce((sum, transfer) => {
        if (transfer.status === "completed") return sum + 100;
        return sum + Math.max(0, Math.min(100, transfer.progress));
    }, 0);

    return Math.max(0, Math.min(100, totalProgress / scopedTransfers.length));
}
