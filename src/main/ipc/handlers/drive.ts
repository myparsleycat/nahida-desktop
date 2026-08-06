import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import { createDriveApiError } from "@main/services/drive-errors";

export function registerDriveHandlers(d: NahidaDesktop) {
    rh("drive:get:item", async (itemId) => {
        return await safeDriveCall(d, "get:item", `itemId=${itemId}`, () =>
            d.service.drive.get.item(itemId),
        );
    });

    rh("drive:patch:rename", async (itemId, name) => {
        return await safeDriveCall(d, "patch:rename", `itemId=${itemId}`, () =>
            d.service.drive.patch.rename(itemId, name),
        );
    });

    rh("drive:post:dir", async (parentId, name) => {
        return await safeDriveCall(d, "post:dir", `parentId=${parentId}`, () =>
            d.service.drive.post.dir(parentId, name),
        );
    });

    rh("drive:delete:items", async (ids, action) => {
        return await safeDriveCall(d, "delete:items", `action=${action},count=${ids.length}`, () =>
            d.service.drive.delete.items(ids, action),
        );
    });

    rh("drive:fn:startDownload", async ({ items, targetPath }) => {
        return await safeDriveCall(d, "fn:startDownload", `count=${items.length}`, () =>
            d.service.drive.fn.startDownload({ items, targetPath, source: "drive" }),
        );
    });

    rh("drive:fn:startUpload", async ({ destId, paths, conflictStrategy }) => {
        return await safeDriveCall(d, "fn:startUpload", `destId=${destId}`, () =>
            d.service.drive.fn.startUpload({ destId, paths, conflictStrategy }),
        );
    });

    rh("drive:fn:getUploadConflicts", async ({ destId, paths }) => {
        return await safeDriveCall(d, "fn:getUploadConflicts", `destId=${destId}`, () =>
            d.service.drive.fn.getUploadConflicts({ destId, paths }),
        );
    });

    rh("drive:fn:moveMany", async ({ ids, destId }) => {
        return await safeDriveCall(d, "fn:moveMany", `destId=${destId},count=${ids.length}`, () =>
            d.service.drive.fn.moveMany({ ids, destId }),
        );
    });

    rh("drive:fn:copyMany", async ({ ids, destId }) => {
        return await safeDriveCall(d, "fn:copyMany", `destId=${destId},count=${ids.length}`, () =>
            d.service.drive.fn.copyMany({ ids, destId }),
        );
    });

    rh("drive:fn:copyFromUrl", async (params) => {
        return await safeDriveCall(
            d,
            "fn:copyFromUrl",
            `url=${params.url},destinationId=${params.destinationId}`,
            () => d.service.drive.fn.copyFromUrl(params),
        );
    });
}

async function safeDriveCall<T>(
    d: NahidaDesktop,
    operation: string,
    context: string,
    callback: () => Promise<T>,
) {
    try {
        return await callback();
    } catch (error) {
        d.logger.error(error, `Drive:${operation}:${context}`);
        throw createDriveApiError(error, operation);
    }
}
