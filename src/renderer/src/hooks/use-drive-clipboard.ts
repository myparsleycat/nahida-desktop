import { useSelectionStore } from "@renderer/store/drive";
import type { Content } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import { useRouteContext } from "@tanstack/react-router";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";

export function useDriveClipboardActions(destinationId: string) {
    const { t } = useTranslation();
    const { queryClient } = useRouteContext({ from: "__root__" });
    const { selectedItems, copyOrCuts, setCopyOrCuts } = useSelectionStore();

    const handleCut = useCallback(() => {
        if (selectedItems.length === 0) return;

        const itemsToCut = [...selectedItems];
        setCopyOrCuts("cut", itemsToCut);

        if (itemsToCut.length === 1) {
            toast.info(t("page.drive.clipboard.cut_single", { name: itemsToCut[0].name }));
            return;
        }

        toast.info(
            t("page.drive.clipboard.cut_multiple", {
                name: itemsToCut[0].name,
                count: itemsToCut.length - 1,
            }),
        );
    }, [selectedItems, setCopyOrCuts, t]);

    const handleCopy = useCallback(() => {
        if (selectedItems.length === 0) return;

        const itemsToCopy = [...selectedItems];
        setCopyOrCuts("copy", itemsToCopy);

        if (itemsToCopy.length === 1) {
            toast.info(t("page.drive.clipboard.copy_single", { name: itemsToCopy[0].name }));
            return;
        }

        toast.info(
            t("page.drive.clipboard.copy_multiple", {
                name: itemsToCopy[0].name,
                count: itemsToCopy.length - 1,
            }),
        );
    }, [selectedItems, setCopyOrCuts, t]);

    const handlePaste = useCallback(() => {
        if (copyOrCuts.action === null || copyOrCuts.items.length === 0) return;

        if (copyOrCuts.action === "cut") {
            const itemsToMove: Content[] = [...copyOrCuts.items];

            const promise = window.api.invoke("drive:fn:moveMany", {
                ids: itemsToMove.map((item) => item.id),
                destId: destinationId,
            });

            toast.promise(promise, {
                loading: t("page.drive.clipboard.move_loading"),
                success: () => {
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "drive", destinationId],
                    });
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "share", destinationId],
                    });
                    setCopyOrCuts(null, []);
                    return t("page.drive.clipboard.move_success");
                },
                error: (err: unknown) =>
                    t("page.drive.clipboard.move_error", {
                        message: toErrorMessage(err),
                    }),
            });
            return;
        }

        if (copyOrCuts.action === "copy") {
            const itemsToCopy: Content[] = [...copyOrCuts.items];

            const promise = window.api.invoke("drive:fn:copyMany", {
                ids: itemsToCopy.map((item) => item.id),
                destId: destinationId,
            });

            toast.promise(promise, {
                loading: t("page.drive.clipboard.copy_loading"),
                success: () => {
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "drive", destinationId],
                    });
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "share", destinationId],
                    });
                    return t("page.drive.clipboard.copy_success");
                },
                error: (err: unknown) =>
                    t("page.drive.clipboard.copy_error", {
                        message: toErrorMessage(err),
                    }),
            });
        }
    }, [copyOrCuts, destinationId, queryClient, setCopyOrCuts, t]);

    return {
        copyOrCuts,
        handleCut,
        handleCopy,
        handlePaste,
    };
}
