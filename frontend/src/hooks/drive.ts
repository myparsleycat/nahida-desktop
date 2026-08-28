import { Drive } from "@bindings/drive";
import { dialogStore, useDragStore } from "@renderer/store/drive";

export function useDrag() {
    const setUploadDragging = useDragStore((s) => s.setUploadDragging);
    const setCurrentDragOver = useDragStore((s) => s.setCurrentDragOver);

    const onDragEnter = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            setUploadDragging(true);
        }
    };

    const onDragLeave = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            setUploadDragging(false);
        }

        setCurrentDragOver(null);
    };

    const onDragOver = (e: React.DragEvent) => {
        if (e.dataTransfer?.types.includes("Files")) {
            setUploadDragging(true);
        }
    };

    const onDrop = (e: React.DragEvent) => {
        if (!e.dataTransfer?.types.includes("Files")) return;
        setUploadDragging(false);
        setCurrentDragOver(null);
    };

    const uploadPaths = async (paths: string[], itemId: string) => {
        if (paths.length < 1) return;

        const { selectedPaths, conflicts } = await Drive.GetUploadConflicts({
            destId: itemId,
            paths,
        });

        if (!selectedPaths || selectedPaths.length < 1) return;

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
        uploadPaths,
    };
}
