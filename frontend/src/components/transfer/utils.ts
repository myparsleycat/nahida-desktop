import { FileImage, FileVideo, FileAudio, FileArchive, FileText, File } from "lucide-react";

import { TransferStatus } from "./types";

export const getFileIcon = (fileType: string) => {
    if (fileType.includes("image")) return FileImage;
    if (fileType.includes("video")) return FileVideo;
    if (fileType.includes("audio")) return FileAudio;
    if (fileType.includes("zip") || fileType.includes("rar") || fileType.includes("archive"))
        return FileArchive;
    if (fileType.includes("pdf") || fileType.includes("doc") || fileType.includes("text"))
        return FileText;
    return File;
};

export const getStatusColor = (status: TransferStatus) => {
    switch (status) {
        case "uploading":
        case "downloading":
            return "text-info";
        case "paused":
            return "text-warning";
        case "completed":
            return "text-green-400";
        case "failed":
            return "text-destructive";
        case "queued":
            return "text-muted-foreground";
        case "preparing":
            return "text-blue-400";
        default:
            return "text-muted-foreground";
    }
};

export const getProgressColor = (status: TransferStatus) => {
    switch (status) {
        case "uploading":
        case "downloading":
            return "bg-info";
        case "paused":
            return "bg-warning";
        case "completed":
            return "bg-success";
        case "failed":
            return "bg-destructive";
        case "queued":
            return "bg-muted-foreground";
        case "preparing":
            return "bg-blue-400";
        default:
            return "bg-muted-foreground";
    }
};
