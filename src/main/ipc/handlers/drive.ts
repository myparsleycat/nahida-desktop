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

    rh("drive:delete:items", async (ids) => {
        return await d.service.drive.delete.items(ids);
    });

    rh("drive:fn:startDownload", async ({ id, suggestedName }) => {
        return await d.service.drive.fn.startDownload({ id, suggestedName });
    });

    rh("drive:fn:startUpload", async ({ destId, paths }) => {
        return d.service.drive.fn.startUpload({ destId, paths });
    });

    rh("drive:fn:moveMany", async ({ ids, destId }) => {
        return d.service.drive.fn.moveMany({ ids, destId });
    });
}
