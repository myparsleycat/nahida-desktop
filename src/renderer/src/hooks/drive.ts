import { useDragStore } from "@renderer/store/drive";
import { useRouteContext } from "@tanstack/react-router";
import { Content } from "@shared/types.gen";
import { toast } from "sonner";
import { compact } from "es-toolkit";

export function useDrag() {
    const { queryClient } = useRouteContext({ from: "__root__" });
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

        await window.api.invoke("drive:fn:startUpload", { destId: itemId, paths });
    };

    return {
        onDragEnter,
        onDragLeave,
        onDragOver,
        onDrop,
    };
}
