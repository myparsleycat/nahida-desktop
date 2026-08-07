import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import { createDriveApiError } from "@main/services/drive-errors";
import { toErrorMessage } from "@shared/utils";

type DriveIpcContext = {
    entity: string;
    stage: string;
    itemId?: string;
    parentId?: string;
    destinationId?: string;
    targetPath?: string;
    paths?: string[];
    ids?: string[];
    action?: string;
    url?: string;
    collectionId?: string;
    cleanupState?: string;
    createCollectionFolders?: boolean;
};

export function registerDriveHandlers(d: NahidaDesktop) {
    rh("drive:get:item", async (itemId) => {
        return await safeDriveCall(
            d,
            "get:item",
            {
                entity: "drive item",
                stage: "read-item",
                itemId,
            },
            () => d.service.drive.get.item(itemId),
        );
    });

    rh("drive:patch:rename", async (itemId, name) => {
        return await safeDriveCall(
            d,
            "patch:rename",
            {
                entity: "drive item",
                stage: "rename-item",
                itemId,
            },
            () => d.service.drive.patch.rename(itemId, name),
        );
    });

    rh("drive:post:dir", async (parentId, name) => {
        return await safeDriveCall(
            d,
            "post:dir",
            {
                entity: "drive folder",
                stage: "create-directory",
                parentId,
            },
            () => d.service.drive.post.dir(parentId, name),
        );
    });

    rh("drive:delete:items", async (ids, action) => {
        return await safeDriveCall(
            d,
            "delete:items",
            {
                entity: "drive items",
                stage: "delete-items",
                ids,
                action,
            },
            () => d.service.drive.delete.items(ids, action),
        );
    });

    rh("drive:fn:startDownload", async ({ items, targetPath }) => {
        return await safeDriveCall(
            d,
            "fn:startDownload",
            {
                entity: "drive download",
                stage: "prepare-download",
                ids: items.map((item) => item.id),
                targetPath,
            },
            () => d.service.drive.fn.startDownload({ items, targetPath, source: "drive" }),
        );
    });

    rh("drive:fn:startUpload", async ({ destId, paths, conflictStrategy }) => {
        return await safeDriveCall(
            d,
            "fn:startUpload",
            {
                entity: "drive upload",
                stage: "prepare-upload",
                destinationId: destId,
                paths,
                cleanupState: "service-managed",
            },
            () => d.service.drive.fn.startUpload({ destId, paths, conflictStrategy }),
        );
    });

    rh("drive:fn:getUploadConflicts", async ({ destId, paths }) => {
        return await safeDriveCall(
            d,
            "fn:getUploadConflicts",
            {
                entity: "drive upload conflicts",
                stage: "check-upload-conflicts",
                destinationId: destId,
                paths,
            },
            () => d.service.drive.fn.getUploadConflicts({ destId, paths }),
        );
    });

    rh("drive:fn:moveMany", async ({ ids, destId }) => {
        return await safeDriveCall(
            d,
            "fn:moveMany",
            {
                entity: "drive items",
                stage: "move-items",
                ids,
                destinationId: destId,
            },
            () => d.service.drive.fn.moveMany({ ids, destId }),
        );
    });

    rh("drive:fn:copyMany", async ({ ids, destId }) => {
        return await safeDriveCall(
            d,
            "fn:copyMany",
            {
                entity: "drive items",
                stage: "copy-items",
                ids,
                destinationId: destId,
            },
            () => d.service.drive.fn.copyMany({ ids, destId }),
        );
    });

    rh("drive:fn:copyFromUrl", async (params) => {
        return await safeDriveCall(
            d,
            "fn:copyFromUrl",
            {
                entity: "shared Drive URL",
                stage: "resolve-source-and-copy",
                url: params.url,
                destinationId: params.destinationId,
                collectionId: params.collectionId,
                itemId: params.itemId,
                createCollectionFolders: params.createCollectionFolders,
                cleanupState: "service-managed",
            },
            () => d.service.drive.fn.copyFromUrl(params),
        );
    });

    rh("drive:fn:cancelCopyFromUrl", async (operationId) => {
        return await safeDriveCall(d, "fn:cancelCopyFromUrl", `operationId=${operationId}`, () =>
            d.service.drive.fn.cancelCopyFromUrl(operationId),
        );
    });
}

async function safeDriveCall<T>(
    d: NahidaDesktop,
    operation: string,
    context: DriveIpcContext,
    callback: () => Promise<T>,
) {
    try {
        return await callback();
    } catch (error) {
        const normalizedError = createDriveApiError(error, operation);
        d.logger.error(error, `Drive:${operation}`);
        d.logger.error(
            {
                channel: `drive:${operation}`,
                operation,
                ...context,
                errorCode: normalizedError.code,
                error: toErrorMessage(error),
            },
            `Drive:${operation}:context`,
        );
        throw normalizedError;
    }
}
