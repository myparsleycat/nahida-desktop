import { dialogStore, useDragStore } from "@renderer/store/drive";
import { compact } from "es-toolkit";

export function useDrag() {
    const setUploadDragging = useDragStore((s) => s.setUploadDragging);
    const setCurrentDragOver = useDragStore((s) => s.setCurrentDragOver);

    const onDragEnter = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            e.preventDefault();
            e.stopPropagation();
            setUploadDragging(true);
        }
    };

    const onDragLeave = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            e.preventDefault();
            e.stopPropagation();
            setUploadDragging(false);
        }

        setCurrentDragOver(null);
    };

    const onDragOver = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            e.preventDefault();
            e.stopPropagation();
            setUploadDragging(true);
        }
    };

    const onDrop = async (e: React.DragEvent, itemId: string) => {
        if (!e.dataTransfer?.types.includes("Files")) return;

        e.preventDefault();
        e.stopPropagation();

        const files = Array.from(e.dataTransfer.files);
        const paths = compact(files.map((file) => window.webUtils.getPathForFile(file)));

        if (paths.length < 1) return;

        const { selectedPaths, conflicts } = await window.api.invoke(
            "drive:fn:getUploadConflicts",
            {
                destId: itemId,
                paths,
            },
        );

        if (!selectedPaths || selectedPaths.length < 1) return;

        let conflictStrategy: "suffix" | "skip" = "suffix";
        if (conflicts.length > 0) {
            const result = await dialogStore
                .getState()
                .showDialog<"suffix" | "skip" | "cancel">("conflictNameDialog", { conflicts });
            if (result === "cancel") return;
            conflictStrategy = result;
        }

        await window.api.invoke("drive:fn:startUpload", {
            destId: itemId,
            paths: selectedPaths,
            conflictStrategy,
        });
    };

    return {
        onDragEnter,
        onDragLeave,
        onDragOver,
        onDrop,
    };
}
