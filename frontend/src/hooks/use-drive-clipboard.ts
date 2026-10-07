import { Drive } from "@bindings/drive";
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

        setCopyOrCuts("cut", [...selectedItems]);
    }, [selectedItems, setCopyOrCuts]);

    const handleCopy = useCallback(() => {
        if (selectedItems.length === 0) return;

        setCopyOrCuts("copy", [...selectedItems]);
    }, [selectedItems, setCopyOrCuts]);

    const handlePaste = useCallback(() => {
        if (pasting || copyOrCuts.action === null || copyOrCuts.items.length === 0) return;

        if (copyOrCuts.action === "cut") {
            const itemsToMove: Content[] = [...copyOrCuts.items];

            const promise = Drive.MoveMany(
                itemsToMove.map((item) => item.id),
                destinationId,
            );

            toast.promise(promise, {
                loading: t("page.drive.clipboard.move_loading"),
                success: () => {
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "drive", destinationId],
                    });
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "share", destinationId],
                    });
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "search"],
                    });
                    setCopyOrCuts(null, []);
                    return t("page.drive.clipboard.move_success");
                },
                error: (err: unknown) =>
                    t("page.drive.clipboard.move_error", {
                        message: toErrorMessage(err),
                    }),
            });
            return track(promise);
        }

        if (copyOrCuts.action === "copy") {
            const itemsToCopy: Content[] = [...copyOrCuts.items];

            const promise = Drive.CopyMany(
                itemsToCopy.map((item) => item.id),
                destinationId,
            );

            toast.promise(promise, {
                loading: t("page.drive.clipboard.copy_loading"),
                success: () => {
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "drive", destinationId],
                    });
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "share", destinationId],
                    });
                    void queryClient.invalidateQueries({
                        queryKey: ["drive", "search"],
                    });
                    return t("page.drive.clipboard.copy_success");
                },
                error: (err: unknown) =>
                    t("page.drive.clipboard.copy_error", {
                        message: toErrorMessage(err),
                    }),
            });
            return track(promise);
        }
    }, [copyOrCuts, destinationId, queryClient, setCopyOrCuts, t]);

    return {
        copyOrCuts,
        handleCut,
        handleCopy,
        handlePaste,
    };
}

// One paste at a time across every entry point: shortcut, context menu, and clipboard bar.
let pasting = false;

// toast.promise already reports the failure; callers only need to know when the paste is over.
function track(promise: Promise<unknown>) {
    pasting = true;
    const done = () => {
        pasting = false;
    };
    return promise.then(done, done);
}
