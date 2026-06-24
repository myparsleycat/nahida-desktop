import { useSelectionStore } from "@renderer/store/drive";
import type { Content } from "@shared/types";
import { useRouteContext } from "@tanstack/react-router";
import { useCallback } from "react";
import { toast } from "sonner";

export function useDriveClipboardActions(destinationId: string) {
    const { queryClient } = useRouteContext({ from: "__root__" });
    const { selectedItems, copyOrCuts, setCopyOrCuts } = useSelectionStore();

    const handleCut = useCallback(() => {
        if (selectedItems.length === 0) return;

        const itemsToCut = [...selectedItems];
        setCopyOrCuts("cut", itemsToCut);

        if (itemsToCut.length === 1) {
            toast.info(`"${itemsToCut[0].name}"이(가) 잘라내기 상태로 설정되었습니다`);
            return;
        }

        toast.info(
            `"${itemsToCut[0].name}"외 ${itemsToCut.length - 1}개가 잘라내기 상태로 설정되었습니다.`,
        );
    }, [selectedItems, setCopyOrCuts]);

    const handleCopy = useCallback(() => {
        if (selectedItems.length === 0) return;

        const itemsToCopy = [...selectedItems];
        setCopyOrCuts("copy", itemsToCopy);

        if (itemsToCopy.length === 1) {
            toast.info(`"${itemsToCopy[0].name}"이(가) 복사 상태로 설정되었습니다`);
            return;
        }

        toast.info(
            `"${itemsToCopy[0].name}"외 ${itemsToCopy.length - 1}개가 복사 상태로 설정되었습니다.`,
        );
    }, [selectedItems, setCopyOrCuts]);

    const handlePaste = useCallback(() => {
        if (copyOrCuts.action === null || copyOrCuts.items.length === 0) return;

        if (copyOrCuts.action === "cut") {
            const itemsToMove: Content[] = [...copyOrCuts.items];

            const promise = window.api.invoke("drive:fn:moveMany", {
                ids: itemsToMove.map((item) => item.id),
                destId: destinationId,
            });

            toast.promise(promise, {
                loading: "File moving...",
                success: () => {
                    queryClient.invalidateQueries({
                        queryKey: ["drive", "drive", destinationId],
                    });
                    queryClient.invalidateQueries({
                        queryKey: ["drive", "share", destinationId],
                    });
                    setCopyOrCuts(null, []);
                    return "File moved successfully";
                },
                error: (err: unknown) =>
                    `File moving failed: ${err instanceof Error ? err.message : String(err)}`,
            });
            return;
        }

        if (copyOrCuts.action === "copy") {
            const itemsToCopy: Content[] = [...copyOrCuts.items];

            const promise = window.api
                .invoke("drive:fn:copyMany", {
                    ids: itemsToCopy.map((item) => item.id),
                    destId: destinationId,
                })
                .then(({ error }: { error: unknown }) => {
                    if (error) {
                        throw new Error(
                            typeof error === "object" && error !== null && "value" in error
                                ? String((error as { value: unknown }).value)
                                : String(error),
                        );
                    }
                });

            toast.promise(promise, {
                loading: "File copying...",
                success: () => {
                    queryClient.invalidateQueries({
                        queryKey: ["drive", "drive", destinationId],
                    });
                    queryClient.invalidateQueries({
                        queryKey: ["drive", "share", destinationId],
                    });
                    return "File copied successfully";
                },
                error: (err: unknown) =>
                    `File copying failed: ${err instanceof Error ? err.message : String(err)}`,
            });
        }
    }, [copyOrCuts, destinationId, queryClient, setCopyOrCuts]);

    return {
        copyOrCuts,
        handleCut,
        handleCopy,
        handlePaste,
    };
}
