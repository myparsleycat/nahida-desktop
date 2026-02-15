import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";

export function registerSettingHandlers(d: NahidaDesktop) {
    rh("setting:general:getRunOnStartup", async () => {
        return await d.setting.general.getRunOnStartup();
    });

    rh("setting:general:setRunOnStartup", async (enabled) => {
        return await d.setting.general.setRunOnStartup(enabled);
    });

    rh("setting:general:getLanguage", async () => {
        return await d.setting.general.getLanguage();
    });

    rh("setting:general:setLanguage", async (language) => {
        return await d.setting.general.setLanguage(language);
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

    rh("setting:general:getCheckBackgroundUpdates", async () => {
        return await d.setting.general.getCheckBackgroundUpdates();
    });

    rh("setting:general:setCheckBackgroundUpdates", async (enabled) => {
        return await d.setting.general.setCheckBackgroundUpdates(enabled);
    });

    rh("setting:general:getImageCacheSize", async () => {
        return await d.setting.general.getImageCacheSize();
    });

    rh("setting:general:clearImageCache", async () => {
        return await d.setting.general.clearImageCache();
    });

    rh("setting:mod:getDeleteArchiveAfterExtract", async () => {
        return await d.setting.mod.getDeleteArchiveAfterExtract();
    });

    rh("setting:mod:setDeleteArchiveAfterExtract", async (enabled) => {
        await d.setting.mod.setDeleteArchiveAfterExtract(enabled);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getMoveFolderInsteadOfCopy", async () => {
        return await d.setting.mod.getMoveFolderInsteadOfCopy();
    });

    rh("setting:mod:setMoveFolderInsteadOfCopy", async (enabled) => {
        await d.setting.mod.setMoveFolderInsteadOfCopy(enabled);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getVirtualizationEnabled", async () => {
        return await d.setting.mod.getVirtualizationEnabled();
    });

    rh("setting:mod:setVirtualizationEnabled", async (enabled) => {
        await d.setting.mod.setVirtualizationEnabled(enabled);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getVirtualizationThreshold", async () => {
        return await d.setting.mod.getVirtualizationThreshold();
    });

    rh("setting:mod:setVirtualizationThreshold", async (threshold) => {
        await d.setting.mod.setVirtualizationThreshold(threshold);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getSearchModPreview", async () => {
        return await d.setting.mod.getSearchModPreview();
    });

    rh("setting:mod:setSearchModPreview", async (enabled) => {
        await d.setting.mod.setSearchModPreview(enabled);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:overlay:getEnabled", async () => {
        return await d.setting.overlay.getEnabled();
    });

    rh("setting:overlay:setEnabled", async (enabled) => {
        await d.setting.overlay.setEnabled(enabled);
    });

    rh("setting:overlay:getToggleKey", async () => {
        return await d.setting.overlay.getToggleKey();
    });

    rh("setting:overlay:setToggleKey", async (key) => {
        await d.setting.overlay.setToggleKey(key);
    });

    rh("setting:net:getProxy", async () => {
        return await d.setting.net.getProxy();
    });

    rh("setting:net:setProxy", async (settings) => {
        return await d.setting.net.setProxy(settings);
    });

    rh("setting:advanced:getAll", async () => {
        return await d.setting.advanced.getAll();
    });

    rh("setting:advanced:set", async (key, value) => {
        return await d.setting.advanced.set(key, value);
    });
}
