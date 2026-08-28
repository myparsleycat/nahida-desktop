import type { QueryKey } from "@tanstack/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Events } from "@wailsio/runtime";
import { useEffect } from "react";

type DriveUploadCompleted = {
    pid: string;
    currentId: string;
};

function isDriveUploadCompleted(value: unknown): value is DriveUploadCompleted {
    if (typeof value !== "object" || value === null) return false;

    const event = value as Partial<DriveUploadCompleted>;
    return typeof event.pid === "string" && typeof event.currentId === "string";
}

export function subscribeDriveUploadCompleted(listener: (event: DriveUploadCompleted) => void) {
    return Events.On("drive:upload-completed", (event) => {
        if (isDriveUploadCompleted(event.data)) {
            listener(event.data);
        }
    });
}

export function useDriveUploadRefresh(currentId: string, queryKey: QueryKey) {
    const queryClient = useQueryClient();

    useEffect(() => {
        return subscribeDriveUploadCompleted((event) => {
            if (event.currentId === currentId) {
                void queryClient.invalidateQueries({ queryKey, exact: true });
            }
        });
    }, [currentId, queryClient, queryKey]);
}
