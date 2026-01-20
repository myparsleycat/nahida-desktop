import { rh } from "@main/ipc/helper";
import type { NahidaDesktop } from "@main/index";

export function registerSettingHandlers(d: NahidaDesktop) {
    rh("setting:general:getRunOnStartup", async () => {
        return await d.setting.general.getRunOnStartup();
    });

    rh("setting:general:setRunOnStartup", async (enabled) => {
        return await d.setting.general.setRunOnStartup(enabled);
    });

    rh("setting:general:getMoveTransferPageWhenStartTransfer", async () => {
        return await d.setting.general.getMoveTransferPageWhenStartTransfer();
    });

    rh("setting:general:setMoveTransferPageWhenStartTransfer", async (enabled) => {
        return await d.setting.general.setMoveTransferPageWhenStartTransfer(enabled);
    });

    rh("setting:general:getPowerSaveBlockInTransfer", async () => {
        return await d.setting.general.getPowerSaveBlockInTransfer();
    });

    rh("setting:general:setPowerSaveBlockInTransfer", async (enabled) => {
        return await d.setting.general.setPowerSaveBlockInTransfer(enabled);
    });

    rh("setting:general:getDefaultStartPage", async () => {
        return await d.setting.general.getDefaultStartPage();
    });

    rh("setting:general:setDefaultStartPage", async (page) => {
        return await d.setting.general.setDefaultStartPage(page);
    });

    rh("setting:general:checkUpdate", async () => {
        return await d.setting.general.checkUpdate();
    });

    rh("setting:mod:getDeleteArchiveAfterExtract", async () => {
        return await d.setting.mod.getDeleteArchiveAfterExtract();
    });

    rh("setting:mod:setDeleteArchiveAfterExtract", async (enabled) => {
        return await d.setting.mod.setDeleteArchiveAfterExtract(enabled);
    });

    rh("setting:mod:getMoveFolderInsteadOfCopy", async () => {
        return await d.setting.mod.getMoveFolderInsteadOfCopy();
    });

    rh("setting:mod:setMoveFolderInsteadOfCopy", async (enabled) => {
        return await d.setting.mod.setMoveFolderInsteadOfCopy(enabled);
    });
}
