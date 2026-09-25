// @vitest-environment jsdom

import {
    getNewlyCompletedUploadDestinations,
    useDriveUploadRefresh,
} from "@renderer/hooks/use-drive-upload-refresh";
import { globalStore } from "@renderer/store/global";
import type { TransferWithoutData } from "@shared/types";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
    cleanup();
    globalStore.getState().setTransfers([]);
});

function transfer(
    pid: string,
    status: TransferWithoutData["status"],
    currentId?: string,
    type: TransferWithoutData["type"] = "upload",
): TransferWithoutData {
    return { pid, status, currentId, type } as TransferWithoutData;
}

describe("drive upload refresh", () => {
    it("returns destinations whose uploads have just completed", () => {
        const destinations = getNewlyCompletedUploadDestinations(
            [
                transfer("drive-upload", "completed", "drive-folder"),
                transfer("share-upload", "completed", "share-folder"),
            ],
            { "drive-upload": "progress", "share-upload": "progress" },
        );

        expect(destinations).toEqual(new Set(["drive-folder", "share-folder"]));
    });

    it("does not refresh completed uploads more than once", () => {
        const destinations = getNewlyCompletedUploadDestinations(
            [transfer("upload-1", "completed", "destination")],
            { "upload-1": "completed" },
        );

        expect(destinations).toEqual(new Set());
    });

    it("ignores incomplete transfers, downloads, and uploads without a destination", () => {
        const destinations = getNewlyCompletedUploadDestinations(
            [
                transfer("pending", "progress", "drive-folder"),
                transfer("download", "completed", "drive-folder", "download"),
                transfer("missing-destination", "completed"),
            ],
            {},
        );

        expect(destinations).toEqual(new Set());
    });

    it("invalidates only the matching folder once when an upload completes", () => {
        const client = new QueryClient();
        const invalidate = vi.spyOn(client, "invalidateQueries");
        const queryKey = ["drive", "drive", "drive-folder"] as const;
        act(() =>
            globalStore.getState().setTransfers([transfer("upload-1", "progress", "drive-folder")]),
        );
        const { rerender } = renderHook(() => useDriveUploadRefresh("drive-folder", queryKey), {
            wrapper: ({ children }: { children: ReactNode }) =>
                createElement(QueryClientProvider, { client }, children),
        });
        expect(invalidate).not.toHaveBeenCalled();

        act(() =>
            globalStore
                .getState()
                .setTransfers([transfer("upload-1", "completed", "drive-folder")]),
        );
        expect(invalidate).toHaveBeenCalledExactlyOnceWith({ queryKey, exact: true });
        rerender();
        act(() =>
            globalStore
                .getState()
                .setTransfers([transfer("upload-1", "completed", "drive-folder")]),
        );
        expect(invalidate).toHaveBeenCalledTimes(1);

        act(() =>
            globalStore
                .getState()
                .setTransfers([
                    transfer("upload-1", "completed", "drive-folder"),
                    transfer("upload-2", "completed", "another-folder"),
                ]),
        );
        expect(invalidate).toHaveBeenCalledTimes(1);
    });
});
