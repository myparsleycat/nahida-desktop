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

    rh("setting:general:getTitlebarStyle", async () => {
        return await d.setting.general.getTitlebarStyle();
    });

    rh("setting:general:setTitlebarStyle", async (style) => {
        await d.setting.general.setTitlebarStyle(style);
    });

    rh("setting:general:getAutoUpdate", async () => {
        return await d.setting.general.getAutoUpdate();
    });

    rh("setting:general:setAutoUpdate", async (enabled) => {
        return await d.setting.general.setAutoUpdate(enabled);
    });

    rh("updater:getStatus", async () => {
        return d.updater.getStatus();
    });

    rh("updater:installUpdate", async () => {
        return await d.updater.installUpdate();
    });

    rh("setting:general:getRunInBackground", async () => {
        return await d.setting.general.getRunInBackground();
    });

    rh("setting:general:setRunInBackground", async (enabled) => {
        return await d.setting.general.setRunInBackground(enabled);
    });

    rh("setting:general:getImageCacheSize", async () => {
        return await d.setting.general.getImageCacheSize();
    });

    rh("setting:general:clearImageCache", async () => {
        return await d.setting.general.clearImageCache();
    });

    rh("setting:general:getLogLevel", async () => {
        return await d.setting.general.getLogLevel();
    });

    rh("setting:general:setLogLevel", async (level) => {
        return await d.setting.general.setLogLevel(level);
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

    rh("setting:transfer:getDownloadConcurrency", async () => {
        return await d.setting.transfer.getDownloadConcurrency();
    });

    rh("setting:transfer:setDownloadConcurrency", async (concurrency) => {
        return await d.setting.transfer.setDownloadConcurrency(concurrency);
    });

    rh("setting:transfer:getUploadConcurrency", async () => {
        return await d.setting.transfer.getUploadConcurrency();
    });

    rh("setting:transfer:setUploadConcurrency", async (concurrency) => {
        return await d.setting.transfer.setUploadConcurrency(concurrency);
    });

    rh("setting:transfer:getUploadCreateManyConcurrency", async () => {
        return await d.setting.transfer.getUploadCreateManyConcurrency();
    });

    rh("setting:transfer:setUploadCreateManyConcurrency", async (concurrency) => {
        return await d.setting.transfer.setUploadCreateManyConcurrency(concurrency);
    });

    rh("setting:xxmi:getPersistToggles", async () => {
        return await d.setting.xxmi.getPersistToggles();
    });

    rh("setting:xxmi:setPersistToggles", async (enabled) => {
        await d.setting.xxmi.setPersistToggles(enabled);
    });

    rh("setting:xxmi:getPersistLogs", async () => {
        return d.setting.xxmi.getPersistLogs();
    });

    rh("setting:xxmi:getToggleViewerAutoGenerate", async () => {
        return await d.setting.xxmi.getToggleViewerAutoGenerate();
    });

    rh("setting:xxmi:setToggleViewerAutoGenerate", async (enabled) => {
        await d.setting.xxmi.setToggleViewerAutoGenerate(enabled);
    });

    rh("setting:xxmi:getToggleViewerHotkey", async () => {
        return await d.setting.xxmi.getToggleViewerHotkey();
    });

    rh("setting:xxmi:setToggleViewerHotkey", async (hotkey) => {
        await d.setting.xxmi.setToggleViewerHotkey(hotkey);
    });

    rh("setting:xxmi:getToggleViewerLogs", async () => {
        return d.setting.xxmi.getToggleViewerLogs();
    });

    rh("setting:xxmi:getToggleViewerState", async () => {
        return d.setting.xxmi.getToggleViewerState();
    });

    rh("setting:xxmi:runToggleViewerBatchGenerate", async () => {
        return d.setting.xxmi.runToggleViewerBatchGenerate();
    });

    rh("setting:xxmi:runToggleViewerBatchDelete", async () => {
        return d.setting.xxmi.runToggleViewerBatchDelete();
    });

    rh("setting:xxmi:cancelToggleViewerWork", async () => {
        return d.setting.xxmi.cancelToggleViewerWork();
    });

    rh("setting:advanced:getAll", async () => {
        return await d.setting.advanced.getAll();
    });

    rh("setting:advanced:set", async (key, value) => {
        return await d.setting.advanced.set(key, value);
    });
}
