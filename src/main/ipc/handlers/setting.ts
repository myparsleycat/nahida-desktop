import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import { supportsWindowsDesktopFeatures } from "@shared/platform";

export function registerSettingHandlers(d: NahidaDesktop) {
    rh("setting:get", async (key) => {
        return await d.setting.get(key);
    });

    rh("setting:getMany", async (keys) => {
        return await d.setting.getMany(keys);
    });

    rh("setting:set", async (key, value) => {
        return await d.setting.set(key, value);
    });

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

    rh("setting:general:getAutoUpdateMode", async () => {
        return await d.setting.general.getAutoUpdateMode();
    });

    rh("setting:general:setAutoUpdateMode", async (mode) => {
        return await d.setting.general.setAutoUpdateMode(mode);
    });

    rh("updater:getStatus", async () => {
        return d.updater.getStatus();
    });

    rh("updater:dismissUpdateDialog", async () => {
        d.updater.dismissUpdateDialog();
    });

    rh("updater:installUpdate", async () => {
        return await d.updater.installUpdate();
    });

    rh("updater:downloadUpdate", async () => {
        return await d.updater.downloadUpdate();
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

    rh("setting:mod:getSidebarLayout", async () => {
        return await d.setting.mod.getSidebarLayout();
    });

    rh("setting:mod:setSidebarLayout", async (mode) => {
        await d.setting.mod.setSidebarLayout(mode);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getCharacterSidebarWidth", async () => {
        return await d.setting.mod.getCharacterSidebarWidth();
    });

    rh("setting:mod:setCharacterSidebarWidth", async (width) => {
        await d.setting.mod.setCharacterSidebarWidth(width);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getArchiveExtractPathMode", async () => {
        return await d.setting.mod.getArchiveExtractPathMode();
    });

    rh("setting:mod:setArchiveExtractPathMode", async (mode) => {
        await d.setting.mod.setArchiveExtractPathMode(mode);
        d.ipc.broadcast("mod:update-settings");
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

    rh("setting:mod:getGridLayoutMode", async () => {
        return await d.setting.mod.getGridLayoutMode();
    });

    rh("setting:mod:setGridLayoutMode", async (mode) => {
        await d.setting.mod.setGridLayoutMode(mode);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getGridResponsiveBaseWidth", async () => {
        return await d.setting.mod.getGridResponsiveBaseWidth();
    });

    rh("setting:mod:setGridResponsiveBaseWidth", async (width) => {
        await d.setting.mod.setGridResponsiveBaseWidth(width);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getGridFixedCardWidth", async () => {
        return await d.setting.mod.getGridFixedCardWidth();
    });

    rh("setting:mod:setGridFixedCardWidth", async (width) => {
        await d.setting.mod.setGridFixedCardWidth(width);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getGridFixedColumnCount", async () => {
        return await d.setting.mod.getGridFixedColumnCount();
    });

    rh("setting:mod:setGridFixedColumnCount", async (count) => {
        await d.setting.mod.setGridFixedColumnCount(count);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getSearchModPreview", async () => {
        return await d.setting.mod.getSearchModPreview();
    });

    rh("setting:mod:setSearchModPreview", async (enabled) => {
        await d.setting.mod.setSearchModPreview(enabled);
        d.ipc.broadcast("mod:update-settings");
    });

    rh("setting:mod:getCopyShaderFixesOnEnable", async () => {
        return await d.setting.mod.getCopyShaderFixesOnEnable();
    });

    rh("setting:mod:setCopyShaderFixesOnEnable", async (enabled) => {
        await d.setting.mod.setCopyShaderFixesOnEnable(enabled);
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

    rh("setting:drive:getNameSortPolicy", async () => {
        return await d.setting.drive.getNameSortPolicy();
    });

    rh("setting:drive:setNameSortPolicy", async (policy) => {
        await d.setting.drive.setNameSortPolicy(policy);
        d.ipc.broadcast("drive:update-settings");
    });

    rh("setting:modelViewer:getToneMapping", async () => {
        return await d.setting.modelViewer.getToneMapping();
    });

    rh("setting:modelViewer:setToneMapping", async (toneMapping) => {
        return await d.setting.modelViewer.setToneMapping(toneMapping);
    });

    rh("setting:modelViewer:getEnvironment", async () => {
        return await d.setting.modelViewer.getEnvironment();
    });

    rh("setting:modelViewer:setEnvironment", async (environment) => {
        return await d.setting.modelViewer.setEnvironment(environment);
    });

    rh("setting:modelViewer:getExposure", async () => {
        return await d.setting.modelViewer.getExposure();
    });

    rh("setting:modelViewer:setExposure", async (exposure) => {
        return await d.setting.modelViewer.setExposure(exposure);
    });

    if (supportsWindowsDesktopFeatures(process.platform)) {
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
    }

    rh("setting:advanced:getAll", async () => {
        return await d.setting.advanced.getAll();
    });

    rh("setting:advanced:set", async (key, value) => {
        return await d.setting.advanced.set(key, value);
    });
}
