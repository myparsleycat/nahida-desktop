import { ReactNode } from "react";

export type TransferStatus =
    | "uploading"
    | "downloading"
    | "paused"
    | "completed"
    | "failed"
    | "queued"
    | "preparing";

export type TransferType = "upload" | "download";

export interface TransferItemProps {
    id: string;
    fileName: string;
    fileSize: string;
    fileType: string;
    progress: number;
    speed?: string;
    timeRemaining?: string;
    status: TransferStatus;
    type: TransferType;
    path?: string;
    onPause?: (id: string) => void;
    onResume?: (id: string) => void;
    onCancel?: (id: string) => void;
    onRetry?: (id: string) => void;
}

export interface TransferStatsProps {
    totalUploads: number;
    totalDownloads: number;
    uploadSpeed: string;
    downloadSpeed: string;
    totalTransferred: string;
    activeTransfers: number;
}

export type TransferTabType = "all" | "uploads" | "downloads";
