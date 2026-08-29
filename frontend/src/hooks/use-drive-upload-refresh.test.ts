import { getNewlyCompletedUploadDestinations } from "@renderer/hooks/use-drive-upload-refresh";
import type { TransferWithoutData } from "@shared/types";
import { describe, expect, it } from "vitest";

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
});
