import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerDriveHandlers(d: NahidaDesktop) {
    rh("drive:get:item", async (itemId) => {
        return await d.service.drive.get.item(itemId);
    });

    rh("drive:patch:rename", async (itemId, name) => {
        return await d.service.drive.patch.rename(itemId, name);
    });

    rh("drive:post:dir", async (parentId, name) => {
        return await d.service.drive.post.dir(parentId, name);
    });

    rh("drive:delete:items", async (ids, action) => {
        return await d.service.drive.delete.items(ids, action);
    });

    rh("drive:fn:startDownload", async ({ items, targetPath }) => {
        return await d.service.drive.fn.startDownload({ items, targetPath, source: "drive" });
    });

    rh("drive:fn:startUpload", async ({ destId, paths, conflictStrategy }) => {
        return d.service.drive.fn.startUpload({ destId, paths, conflictStrategy });
    });

    rh("drive:fn:getUploadConflicts", async ({ destId, paths }) => {
        return d.service.drive.fn.getUploadConflicts({ destId, paths });
    });

    rh("drive:fn:moveMany", async ({ ids, destId }) => {
        return d.service.drive.fn.moveMany({ ids, destId });
    });

    rh("drive:fn:copyMany", async ({ ids, destId }) => {
        return d.service.drive.fn.copyMany({ ids, destId });
    });
}
