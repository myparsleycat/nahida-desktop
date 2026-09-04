import { Drive } from "@bindings/drive";
import { dialogStore, useDragStore } from "@renderer/store/drive";
import { useState } from "react";

export function useDrag() {
    const setCurrentDragOver = useDragStore((s) => s.setCurrentDragOver);
    const [uploadDragging, setUploadDragging] = useState(false);

    const clearDragging = () => {
        setUploadDragging(false);
        setCurrentDragOver(null);
    };

    const onDragEnter = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            setUploadDragging(true);
        }
    };

    const onDragLeave = (e: React.DragEvent) => {
        if (!e.dataTransfer?.types.includes("Files")) {
            return;
        }

        const rect = e.currentTarget.getBoundingClientRect();
        const x = e.clientX;
        const y = e.clientY;

        if (x <= rect.left || x >= rect.right || y <= rect.top || y >= rect.bottom) {
            clearDragging();
        }
    };

    const onDragOver = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            setUploadDragging(true);
        }
    };

    const onDrop = (e: React.DragEvent) => {
        if (!e.dataTransfer?.types.includes("Files")) return;
        clearDragging();
    };

    const uploadPaths = async (paths: string[], itemId: string) => {
        if (paths.length < 1) return;

        const { selectedPaths, conflicts, skippedExtensions } = await Drive.GetUploadConflicts({
            destId: itemId,
            paths,
        });

        if (!selectedPaths || selectedPaths.length < 1) return;

        if ((skippedExtensions ?? []).length > 0) {
            const result = await dialogStore
                .getState()
                .showDialog<"proceed" | "cancel">("unsupportedExtensionsDialog", {
                    extensions: skippedExtensions ?? [],
                });
            if (result !== "proceed") return;
        }

        let conflictStrategy: "suffix" | "skip" = "suffix";
        if ((conflicts ?? []).length > 0) {
            const result = await dialogStore
                .getState()
                .showDialog<"suffix" | "skip" | "cancel">("conflictNameDialog", { conflicts });
            if (result === "cancel") return;
            conflictStrategy = result;
        }

        await Drive.StartUpload({
            destId: itemId,
            paths: selectedPaths,
            conflictStrategy: conflictStrategy as never,
        });
    };

    return {
        onDragEnter,
        onDragLeave,
        onDragOver,
        onDrop,
        uploadDragging,
        clearDragging,
        uploadPaths,
    };
}
