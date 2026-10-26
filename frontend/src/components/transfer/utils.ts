import { TransferStatus } from "./types";

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
    }
};
