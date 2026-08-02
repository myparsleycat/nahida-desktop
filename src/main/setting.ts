import type { NahidaDesktop } from "@main/index";
import { normalizeDriveNameSortPolicy, type DriveNameSortPolicy } from "@shared/drive";
import {
    ARCHIVE_EXTRACT_PATH_MODES,
    DISABLED_PREFIX_STYLES,
    DOWNLOAD_SOURCES,
    MOD_GRID_LAYOUT_MODES,
    SIDEBAR_LAYOUT_MODES,
    type ArchiveExtractPathMode,
    type DisabledPrefixStyle,
    type DownloadSource,
    type ModGridLayoutMode,
    type SidebarLayoutMode,
} from "@shared/mod";
import {
    APP_SETTINGS,
    type AppSettings,
    type SettingDefinition,
    type SettingKey,
} from "@shared/settings";
import type { AutoUpdateMode } from "@shared/updater";
import AutoLaunch from "auto-launch";
import { app, BrowserWindow } from "electron";

import { LogLevel } from "./internal/logger";

interface Bounds {
    x: number;
    y: number;
    width: number;
    height: number;
}

const DEFAULT_TOGGLE_VIEWER_HOTKEY = "ctrl H";
const MODEL_VIEWER_TONE_MAPPINGS = ["neutral", "aces", "none"] as const;
const MODEL_VIEWER_ENVIRONMENTS = ["studio", "soft", "none"] as const;
const DEFAULT_MODEL_VIEWER_TONE_MAPPING = "neutral";
const DEFAULT_MODEL_VIEWER_ENVIRONMENT = "studio";
const DEFAULT_MODEL_VIEWER_EXPOSURE = 0.7;
const MODEL_VIEWER_EXPOSURE_MIN = 0;
const MODEL_VIEWER_EXPOSURE_MAX = 4;
const TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT = 32;
const TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX = [16, 64];
const TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_DEFAULT = 0;
const TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_MIN_MAX = [0, 1024];
const TRANSFER_UPLOAD_CONCURRENCY_DEFAULT = 8;
const TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX = [4, 16];
const MOD_GRID_WIDTH_MIN = 240;
const MOD_GRID_WIDTH_MAX = 640;
const MOD_GRID_COLUMN_MIN = 1;
const MOD_GRID_COLUMN_MAX = 8;
const MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT = 400;
const MOD_GRID_FIXED_CARD_WIDTH_DEFAULT = 360;
const MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT = 4;
const MOD_CHARACTER_SIDEBAR_WIDTH_MIN = 220;
const MOD_CHARACTER_SIDEBAR_WIDTH_MAX = 480;
const MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT = 256;

function clampTransferConcurrency(value: number, min: number, max: number, fallback: number) {
    if (!Number.isFinite(value)) {
        return fallback;
    }

    return Math.min(max, Math.max(min, Math.trunc(value)));
}

function normalizeModelViewerToneMapping(
    value: string | null | undefined,
): (typeof MODEL_VIEWER_TONE_MAPPINGS)[number] {
    return MODEL_VIEWER_TONE_MAPPINGS.includes(value as (typeof MODEL_VIEWER_TONE_MAPPINGS)[number])
        ? (value as (typeof MODEL_VIEWER_TONE_MAPPINGS)[number])
        : DEFAULT_MODEL_VIEWER_TONE_MAPPING;
}

function normalizeModelViewerEnvironment(
    value: string | null | undefined,
): (typeof MODEL_VIEWER_ENVIRONMENTS)[number] {
    return MODEL_VIEWER_ENVIRONMENTS.includes(value as (typeof MODEL_VIEWER_ENVIRONMENTS)[number])
        ? (value as (typeof MODEL_VIEWER_ENVIRONMENTS)[number])
        : DEFAULT_MODEL_VIEWER_ENVIRONMENT;
}

function clampModelViewerExposure(value: number) {
    if (!Number.isFinite(value)) {
        return DEFAULT_MODEL_VIEWER_EXPOSURE;
    }

    return Math.min(
        MODEL_VIEWER_EXPOSURE_MAX,
        Math.max(MODEL_VIEWER_EXPOSURE_MIN, Math.round(value * 100) / 100),
    );
}

function getDefaultStartPage() {
    return "/mod";
}

function sanitizeDefaultStartPage(page: string | null | undefined) {
    const fallback = getDefaultStartPage();
    if (!page) {
        return fallback;
    }

    return page;
}

function normalizeAutoUpdateMode(value: string | null | undefined): AutoUpdateMode {
    if (value === "notify") {
        return "notify";
    }

    if (value === "off" || value === "false") {
        return "off";
    }

    return "auto";
}

function clampIntegerSetting(value: number, min: number, max: number, fallback: number) {
    if (!Number.isFinite(value)) {
        return fallback;
    }

    return Math.min(max, Math.max(min, Math.trunc(value)));
}

function normalizeModGridLayoutMode(value: string | null | undefined): ModGridLayoutMode {
    return MOD_GRID_LAYOUT_MODES.includes(value as ModGridLayoutMode)
        ? (value as ModGridLayoutMode)
        : "responsive";
}

function normalizeSidebarLayoutMode(value: string | null | undefined): SidebarLayoutMode {
    return SIDEBAR_LAYOUT_MODES.includes(value as SidebarLayoutMode)
        ? (value as SidebarLayoutMode)
        : "row";
}

type MainSettingSpec<K extends SettingKey> = {
    definition: SettingDefinition<K>;
    getDefault: () => AppSettings[K] | Promise<AppSettings[K]>;
    fromStored: (value: string | null | undefined) => AppSettings[K];
    toStored?: (value: AppSettings[K]) => string;
    normalize?: (value: AppSettings[K]) => AppSettings[K];
    afterSet?: (value: AppSettings[K]) => Promise<void> | void;
};

type MainSettingSpecMap = {
    [K in SettingKey]: MainSettingSpec<K>;
};

function parseBooleanSetting(value: string | null | undefined, fallback: boolean) {
    if (value == null) {
        return fallback;
    }

    return value === "true";
}

function normalizeDownloadSources(value: unknown): DownloadSource[] {
    if (!Array.isArray(value)) return ["gamebanana"];
    return value.filter((source): source is DownloadSource =>
        DOWNLOAD_SOURCES.includes(source as DownloadSource),
    );
}

function parseDownloadSources(value: string | null | undefined): DownloadSource[] {
    if (!value) return ["gamebanana"] satisfies DownloadSource[];
    try {
        return normalizeDownloadSources(JSON.parse(value));
    } catch {
        return ["gamebanana"];
    }
}

export class Setting {
    private desktop: NahidaDesktop;
    private settingSpecs: MainSettingSpecMap | null = null;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    private getSettingSpecMap(): MainSettingSpecMap {
        if (this.settingSpecs) {
            return this.settingSpecs;
        }

        this.settingSpecs = {
            "general.runOnStartup": {
                definition: APP_SETTINGS["general.runOnStartup"],
                getDefault: () => false,
                fromStored: (value) => parseBooleanSetting(value, false),
                toStored: (value) => String(value),
                afterSet: async (enabled) => {
                    if (app.isPackaged) {
                        const autoLaunch = new AutoLaunch({
                            name: "Nahida Desktop",
                            path: app.getPath("exe"),
                            isHidden: true,
                        });

                        if (enabled) {
                            await autoLaunch.enable();
                        } else {
                            await autoLaunch.disable();
                        }
                    }
                },
            },
            "general.language": {
                definition: APP_SETTINGS["general.language"],
                getDefault: () => {
                    const systemLocale = app.getSystemLocale();
                    const language = systemLocale.split("-")[0];
                    return ["ko", "en", "ja", "zh"].includes(language) ? language : "en";
                },
                fromStored: (value) => {
                    const systemLocale = app.getSystemLocale();
                    const language = systemLocale.split("-")[0];
                    return value || (["ko", "en", "ja", "zh"].includes(language) ? language : "en");
                },
                afterSet: async (language) => {
                    this.desktop.ipc.broadcast("language:update", language);
                    await this.desktop.updater.handleLanguageChanged(language);
                },
            },
            "general.autoUpdateMode": {
                definition: APP_SETTINGS["general.autoUpdateMode"],
                getDefault: () => "auto",
                fromStored: (value) => normalizeAutoUpdateMode(value),
                normalize: (value) => normalizeAutoUpdateMode(value),
                afterSet: async (mode) => {
                    await this.desktop.updater.handleAutoUpdateModeChanged(mode);
                },
            },
            "general.runInBackground": {
                definition: APP_SETTINGS["general.runInBackground"],
                getDefault: () => true,
                fromStored: (value) => parseBooleanSetting(value, true),
                toStored: (value) => String(value),
            },
            "general.defaultStartPage": {
                definition: APP_SETTINGS["general.defaultStartPage"],
                getDefault: () => getDefaultStartPage(),
                fromStored: (value) => sanitizeDefaultStartPage(value),
                normalize: (value) => sanitizeDefaultStartPage(value),
            },
            "general.titlebarStyle": {
                definition: APP_SETTINGS["general.titlebarStyle"],
                getDefault: () => "modern",
                fromStored: (value) => value || "modern",
                afterSet: async () => {
                    for (const window of BrowserWindow.getAllWindows()) {
                        window.close();
                    }
                    await this.desktop.window.main.focusAndNavigate("/setting/gen");
                },
            },
            "general.logLevel": {
                definition: APP_SETTINGS["general.logLevel"],
                getDefault: () => "error",
                fromStored: (value) => value || "error",
                afterSet: (level) => {
                    this.desktop.logger.setLevel(level as LogLevel);
                },
            },
            "general.moveTransferPageWhenStartTransfer": {
                definition: APP_SETTINGS["general.moveTransferPageWhenStartTransfer"],
                getDefault: () => false,
                fromStored: (value) => parseBooleanSetting(value, false),
                toStored: (value) => String(value),
            },
            "general.powerSaveBlockInTransfer": {
                definition: APP_SETTINGS["general.powerSaveBlockInTransfer"],
                getDefault: () => false,
                fromStored: (value) => parseBooleanSetting(value, false),
                toStored: (value) => String(value),
                afterSet: async () => {
                    await this.desktop.service.transfer.refreshPowerSaveBlock();
                },
            },
            "general.bisectPreserveD3dx": {
                definition: APP_SETTINGS["general.bisectPreserveD3dx"],
                getDefault: () => true,
                fromStored: (value) => parseBooleanSetting(value, true),
                toStored: (value) => String(value),
            },
            "mod.archiveExtractPathMode": {
                definition: APP_SETTINGS["mod.archiveExtractPathMode"],
                getDefault: () => "flatten_single_root",
                fromStored: (value) =>
                    ARCHIVE_EXTRACT_PATH_MODES.includes(value as ArchiveExtractPathMode)
                        ? (value as ArchiveExtractPathMode)
                        : "flatten_single_root",
                normalize: (value) =>
                    ARCHIVE_EXTRACT_PATH_MODES.includes(value) ? value : "flatten_single_root",
            },
            "mod.deleteArchiveAfterExtract": {
                definition: APP_SETTINGS["mod.deleteArchiveAfterExtract"],
                getDefault: () => true,
                fromStored: (value) => parseBooleanSetting(value, true),
                toStored: (value) => String(value),
            },
            "mod.moveFolderInsteadOfCopy": {
                definition: APP_SETTINGS["mod.moveFolderInsteadOfCopy"],
                getDefault: () => true,
                fromStored: (value) => parseBooleanSetting(value, true),
                toStored: (value) => String(value),
            },
            "mod.virtualizationEnabled": {
                definition: APP_SETTINGS["mod.virtualizationEnabled"],
                getDefault: () => true,
                fromStored: (value) => parseBooleanSetting(value, true),
                toStored: (value) => String(value),
            },
            "mod.virtualizationThreshold": {
                definition: APP_SETTINGS["mod.virtualizationThreshold"],
                getDefault: () => 30,
                fromStored: (value) => {
                    const parsed = Number.parseInt(value ?? "", 10);
                    return parsed || 30;
                },
                normalize: (value) =>
                    Number.isFinite(value) && value > 0 ? Math.trunc(value) : 30,
                toStored: (value) => String(value),
            },
            "mod.searchModPreview": {
                definition: APP_SETTINGS["mod.searchModPreview"],
                getDefault: () => false,
                fromStored: (value) => parseBooleanSetting(value, false),
                toStored: (value) => String(value),
            },
            "mod.autoResolveDownloadTarget": {
                definition: APP_SETTINGS["mod.autoResolveDownloadTarget"],
                getDefault: () => false,
                fromStored: (value) => parseBooleanSetting(value, false),
                toStored: (value) => String(value),
            },
            "mod.autoResolveDownloadTargetSources": {
                definition: APP_SETTINGS["mod.autoResolveDownloadTargetSources"],
                getDefault: () => ["gamebanana"],
                fromStored: (value) => parseDownloadSources(value),
                normalize: (value) => normalizeDownloadSources(value),
                toStored: (value) => JSON.stringify(value),
            },
            "mod.copyShaderFixesOnEnable": {
                definition: APP_SETTINGS["mod.copyShaderFixesOnEnable"],
                getDefault: () => true,
                fromStored: (value) => parseBooleanSetting(value, true),
                toStored: (value) => String(value),
            },
            "mod.sidebarLayout": {
                definition: APP_SETTINGS["mod.sidebarLayout"],
                getDefault: () => "row",
                fromStored: (value) => normalizeSidebarLayoutMode(value),
                normalize: (value) => normalizeSidebarLayoutMode(value),
            },
            "mod.characterSidebarWidth": {
                definition: APP_SETTINGS["mod.characterSidebarWidth"],
                getDefault: () => MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT,
                fromStored: (value) =>
                    clampIntegerSetting(
                        Number.parseInt(value ?? "", 10),
                        MOD_CHARACTER_SIDEBAR_WIDTH_MIN,
                        MOD_CHARACTER_SIDEBAR_WIDTH_MAX,
                        MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT,
                    ),
                normalize: (value) =>
                    clampIntegerSetting(
                        value,
                        MOD_CHARACTER_SIDEBAR_WIDTH_MIN,
                        MOD_CHARACTER_SIDEBAR_WIDTH_MAX,
                        MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampIntegerSetting(
                            value,
                            MOD_CHARACTER_SIDEBAR_WIDTH_MIN,
                            MOD_CHARACTER_SIDEBAR_WIDTH_MAX,
                            MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT,
                        ),
                    ),
            },
            "mod.gridLayoutMode": {
                definition: APP_SETTINGS["mod.gridLayoutMode"],
                getDefault: () => "responsive",
                fromStored: (value) => normalizeModGridLayoutMode(value),
                normalize: (value) => normalizeModGridLayoutMode(value),
            },
            "mod.gridResponsiveBaseWidth": {
                definition: APP_SETTINGS["mod.gridResponsiveBaseWidth"],
                getDefault: () => MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT,
                fromStored: (value) =>
                    clampIntegerSetting(
                        Number.parseInt(value ?? "", 10),
                        MOD_GRID_WIDTH_MIN,
                        MOD_GRID_WIDTH_MAX,
                        MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT,
                    ),
                normalize: (value) =>
                    clampIntegerSetting(
                        value,
                        MOD_GRID_WIDTH_MIN,
                        MOD_GRID_WIDTH_MAX,
                        MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampIntegerSetting(
                            value,
                            MOD_GRID_WIDTH_MIN,
                            MOD_GRID_WIDTH_MAX,
                            MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT,
                        ),
                    ),
            },
            "mod.gridFixedCardWidth": {
                definition: APP_SETTINGS["mod.gridFixedCardWidth"],
                getDefault: () => MOD_GRID_FIXED_CARD_WIDTH_DEFAULT,
                fromStored: (value) =>
                    clampIntegerSetting(
                        Number.parseInt(value ?? "", 10),
                        MOD_GRID_WIDTH_MIN,
                        MOD_GRID_WIDTH_MAX,
                        MOD_GRID_FIXED_CARD_WIDTH_DEFAULT,
                    ),
                normalize: (value) =>
                    clampIntegerSetting(
                        value,
                        MOD_GRID_WIDTH_MIN,
                        MOD_GRID_WIDTH_MAX,
                        MOD_GRID_FIXED_CARD_WIDTH_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampIntegerSetting(
                            value,
                            MOD_GRID_WIDTH_MIN,
                            MOD_GRID_WIDTH_MAX,
                            MOD_GRID_FIXED_CARD_WIDTH_DEFAULT,
                        ),
                    ),
            },
            "mod.gridFixedColumnCount": {
                definition: APP_SETTINGS["mod.gridFixedColumnCount"],
                getDefault: () => MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT,
                fromStored: (value) =>
                    clampIntegerSetting(
                        Number.parseInt(value ?? "", 10),
                        MOD_GRID_COLUMN_MIN,
                        MOD_GRID_COLUMN_MAX,
                        MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT,
                    ),
                normalize: (value) =>
                    clampIntegerSetting(
                        value,
                        MOD_GRID_COLUMN_MIN,
                        MOD_GRID_COLUMN_MAX,
                        MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampIntegerSetting(
                            value,
                            MOD_GRID_COLUMN_MIN,
                            MOD_GRID_COLUMN_MAX,
                            MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT,
                        ),
                    ),
            },
            "mod.disabledPrefixStyle": {
                definition: APP_SETTINGS["mod.disabledPrefixStyle"],
                getDefault: () => "space",
                fromStored: (value) =>
                    DISABLED_PREFIX_STYLES.includes(value as DisabledPrefixStyle)
                        ? (value as DisabledPrefixStyle)
                        : "space",
                normalize: (value) => (DISABLED_PREFIX_STYLES.includes(value) ? value : "space"),
            },
            "transfer.downloadConcurrency": {
                definition: APP_SETTINGS["transfer.downloadConcurrency"],
                getDefault: () => TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT,
                fromStored: (value) =>
                    clampTransferConcurrency(
                        Number.parseInt(value ?? "", 10),
                        TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[0],
                        TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[1],
                        TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT,
                    ),
                normalize: (value) =>
                    clampTransferConcurrency(
                        value,
                        TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[0],
                        TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[1],
                        TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampTransferConcurrency(
                            value,
                            TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[0],
                            TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[1],
                            TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT,
                        ),
                    ),
            },
            "transfer.downloadBandwidthLimitMibps": {
                definition: APP_SETTINGS["transfer.downloadBandwidthLimitMibps"],
                getDefault: () => TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_DEFAULT,
                fromStored: (value) =>
                    clampTransferConcurrency(
                        Number.parseInt(value ?? "", 10),
                        TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_MIN_MAX[0],
                        TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_MIN_MAX[1],
                        TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_DEFAULT,
                    ),
                normalize: (value) =>
                    clampTransferConcurrency(
                        value,
                        TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_MIN_MAX[0],
                        TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_MIN_MAX[1],
                        TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampTransferConcurrency(
                            value,
                            TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_MIN_MAX[0],
                            TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_MIN_MAX[1],
                            TRANSFER_DOWNLOAD_BANDWIDTH_LIMIT_MIBPS_DEFAULT,
                        ),
                    ),
                afterSet: (value) => {
                    this.desktop.service.transfer.setDownloadBandwidthLimitMibps(value);
                },
            },
            "transfer.uploadConcurrency": {
                definition: APP_SETTINGS["transfer.uploadConcurrency"],
                getDefault: () => TRANSFER_UPLOAD_CONCURRENCY_DEFAULT,
                fromStored: (value) =>
                    clampTransferConcurrency(
                        Number.parseInt(value ?? "", 10),
                        TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[0],
                        TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[1],
                        TRANSFER_UPLOAD_CONCURRENCY_DEFAULT,
                    ),
                normalize: (value) =>
                    clampTransferConcurrency(
                        value,
                        TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[0],
                        TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[1],
                        TRANSFER_UPLOAD_CONCURRENCY_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampTransferConcurrency(
                            value,
                            TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[0],
                            TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[1],
                            TRANSFER_UPLOAD_CONCURRENCY_DEFAULT,
                        ),
                    ),
            },
            "drive.nameSortPolicy": {
                definition: APP_SETTINGS["drive.nameSortPolicy"],
                getDefault: () => normalizeDriveNameSortPolicy(null),
                fromStored: (value) => normalizeDriveNameSortPolicy(value),
                normalize: (value) => normalizeDriveNameSortPolicy(value),
            },
            "modelViewer.toneMapping": {
                definition: APP_SETTINGS["modelViewer.toneMapping"],
                getDefault: () => DEFAULT_MODEL_VIEWER_TONE_MAPPING,
                fromStored: (value) => normalizeModelViewerToneMapping(value),
                normalize: (value) => normalizeModelViewerToneMapping(value),
            },
            "modelViewer.environment": {
                definition: APP_SETTINGS["modelViewer.environment"],
                getDefault: () => DEFAULT_MODEL_VIEWER_ENVIRONMENT,
                fromStored: (value) => normalizeModelViewerEnvironment(value),
                normalize: (value) => normalizeModelViewerEnvironment(value),
            },
            "modelViewer.exposure": {
                definition: APP_SETTINGS["modelViewer.exposure"],
                getDefault: () => DEFAULT_MODEL_VIEWER_EXPOSURE,
                fromStored: (value) => clampModelViewerExposure(Number.parseFloat(value ?? "")),
                normalize: (value) => clampModelViewerExposure(value),
                toStored: (value) => String(clampModelViewerExposure(value)),
            },
            "xxmi.persistToggles": {
                definition: APP_SETTINGS["xxmi.persistToggles"],
                getDefault: () => false,
                fromStored: (value) => parseBooleanSetting(value, false),
                toStored: (value) => String(value),
                afterSet: async (enabled) => {
                    if (enabled) {
                        await this.set("general.runInBackground", true);
                    }

                    if (this.desktop.service?.modTools) {
                        if (enabled) {
                            await this.desktop.service.modTools.startPersistWatcher();
                        } else {
                            await this.desktop.service.modTools.stopPersistWatcher();
                        }
                    }
                },
            },
            "xxmi.toggleViewerAutoGenerate": {
                definition: APP_SETTINGS["xxmi.toggleViewerAutoGenerate"],
                getDefault: () => false,
                fromStored: (value) => parseBooleanSetting(value, false),
                toStored: (value) => String(value),
                afterSet: async (enabled) => {
                    if (enabled) {
                        await this.set("general.runInBackground", true);
                    }

                    if (this.desktop.service?.modTools) {
                        if (enabled) {
                            const toggleViewerState =
                                this.desktop.service.modTools.toggleViewer.getState();
                            if (toggleViewerState.mode === "generate") {
                                this.desktop.logger.info(
                                    "Deferred toggle viewer watcher start until manual generate completes",
                                    "Setting.xxmi.setToggleViewerAutoGenerate",
                                );
                            } else {
                                await this.desktop.service.modTools.startToggleViewerWatcher();
                            }
                        } else {
                            this.desktop.service.modTools.toggleViewer.cancelCurrentWork();
                            await this.desktop.service.modTools.stopToggleViewerWatcher();
                        }
                    }
                },
            },
            "xxmi.toggleViewerHotkey": {
                definition: APP_SETTINGS["xxmi.toggleViewerHotkey"],
                getDefault: () => DEFAULT_TOGGLE_VIEWER_HOTKEY,
                fromStored: (value) => value?.trim() || DEFAULT_TOGGLE_VIEWER_HOTKEY,
                normalize: (value) => value.trim() || DEFAULT_TOGGLE_VIEWER_HOTKEY,
                afterSet: async (hotkey) => {
                    if (this.desktop.service?.modTools) {
                        await this.desktop.service.modTools.toggleViewer.applyHotkeyToArtifacts(
                            hotkey,
                        );
                    }
                },
            },
        };

        return this.settingSpecs;
    }

    private getSettingSpec<K extends SettingKey>(key: K): MainSettingSpec<K> {
        return this.getSettingSpecMap()[key] as MainSettingSpec<K>;
    }

    private async findStoredSetting(storageKey: string) {
        return await this.desktop.lib.db.settings.get(storageKey);
    }

    private async upsertStoredSetting(storageKey: string, value: string | null) {
        await this.desktop.lib.db.settings.upsert(storageKey, value);
    }

    public async get<K extends SettingKey>(key: K): Promise<AppSettings[K]> {
        const spec = this.getSettingSpec(key);
        const current = await this.findStoredSetting(spec.definition.storageKey);

        if (!current || current.value == null) {
            const fallback = spec.normalize
                ? spec.normalize(await spec.getDefault())
                : await spec.getDefault();
            const storedValue = spec.toStored ? spec.toStored(fallback) : String(fallback);
            await this.upsertStoredSetting(spec.definition.storageKey, storedValue);
            return fallback;
        }

        const resolved = spec.normalize
            ? spec.normalize(spec.fromStored(current.value))
            : spec.fromStored(current.value);
        const storedValue = spec.toStored ? spec.toStored(resolved) : String(resolved);

        if (storedValue !== current.value) {
            await this.upsertStoredSetting(spec.definition.storageKey, storedValue);
        }

        return resolved;
    }

    public async getMany<K extends readonly SettingKey[]>(
        keys: K,
    ): Promise<{ [P in K[number]]: AppSettings[P] }> {
        const entries = await Promise.all(
            keys.map(async (key) => [key, await this.get(key)] as const),
        );

        return Object.fromEntries(entries) as { [P in K[number]]: AppSettings[P] };
    }

    public async set<K extends SettingKey>(key: K, value: AppSettings[K]) {
        const spec = this.getSettingSpec(key);
        const normalized = spec.normalize ? spec.normalize(value) : value;
        const storedValue = spec.toStored ? spec.toStored(normalized) : String(normalized);

        await this.upsertStoredSetting(spec.definition.storageKey, storedValue);
        await spec.afterSet?.(normalized);

        this.desktop.ipc.broadcast("setting:update", { key, value: normalized });
    }

    private async getStoredBounds(key: string) {
        const qr = await this.desktop.lib.db.settings.get(key);

        if (!qr) return null;

        const bounds = JSON.parse(qr.value as string) as Bounds;

        return bounds;
    }

    private async setStoredBounds(key: string, bounds: Bounds) {
        const value = JSON.stringify(bounds);
        await this.desktop.lib.db.settings.upsert(key, value);
    }

    public async getBounds() {
        return this.getStoredBounds("bounds");
    }

    public async setBounds(bounds: Bounds) {
        await this.setStoredBounds("bounds", bounds);
    }

    public async getSettingBounds() {
        return this.getStoredBounds("settingBounds");
    }

    public async setSettingBounds(bounds: Bounds) {
        await this.setStoredBounds("settingBounds", bounds);
    }

    general = {
        getRunOnStartup: async () => await this.get("general.runOnStartup"),
        setRunOnStartup: async (enabled: boolean) =>
            await this.set("general.runOnStartup", enabled),
        getLanguage: async (): Promise<string> => await this.get("general.language"),
        setLanguage: async (language: string) => await this.set("general.language", language),
        getMoveTransferPageWhenStartTransfer: async () =>
            await this.get("general.moveTransferPageWhenStartTransfer"),
        setMoveTransferPageWhenStartTransfer: async (enabled: boolean) =>
            await this.set("general.moveTransferPageWhenStartTransfer", enabled),
        getPowerSaveBlockInTransfer: async () => await this.get("general.powerSaveBlockInTransfer"),
        setPowerSaveBlockInTransfer: async (enabled: boolean) =>
            await this.set("general.powerSaveBlockInTransfer", enabled),
        getDefaultStartPage: async () => await this.get("general.defaultStartPage"),
        setDefaultStartPage: async (page: string | null) =>
            await this.set("general.defaultStartPage", page ?? ""),
        getTitlebarStyle: async () => await this.get("general.titlebarStyle"),
        setTitlebarStyle: async (style: string) => await this.set("general.titlebarStyle", style),
        getAutoUpdateMode: async (): Promise<AutoUpdateMode> =>
            await this.get("general.autoUpdateMode"),
        setAutoUpdateMode: async (mode: AutoUpdateMode) =>
            await this.set("general.autoUpdateMode", mode),
        getRunInBackground: async () => await this.get("general.runInBackground"),
        setRunInBackground: async (enabled: boolean) =>
            await this.set("general.runInBackground", enabled),
        getImageCacheSize: async () => await this.desktop.lib.db.imageCache.sumSize(),
        clearImageCache: async () => {
            await this.desktop.lib.db.imageCache.deleteAll();
        },
        getLogLevel: async () => (await this.get("general.logLevel")) as LogLevel,
        setLogLevel: async (level: LogLevel) => await this.set("general.logLevel", level),
    };

    mod = {
        getSidebarLayout: async (): Promise<SidebarLayoutMode> =>
            await this.get("mod.sidebarLayout"),
        setSidebarLayout: async (mode: SidebarLayoutMode) => {
            await this.set("mod.sidebarLayout", mode);
        },
        getCharacterSidebarWidth: async () => await this.get("mod.characterSidebarWidth"),
        setCharacterSidebarWidth: async (width: number) => {
            await this.set("mod.characterSidebarWidth", width);
        },
        getArchiveExtractPathMode: async (): Promise<ArchiveExtractPathMode> =>
            await this.get("mod.archiveExtractPathMode"),
        setArchiveExtractPathMode: async (mode: ArchiveExtractPathMode) => {
            await this.set("mod.archiveExtractPathMode", mode);
        },
        getDeleteArchiveAfterExtract: async () => await this.get("mod.deleteArchiveAfterExtract"),
        setDeleteArchiveAfterExtract: async (enabled: boolean) => {
            await this.set("mod.deleteArchiveAfterExtract", enabled);
        },
        getMoveFolderInsteadOfCopy: async () => await this.get("mod.moveFolderInsteadOfCopy"),
        setMoveFolderInsteadOfCopy: async (enabled: boolean) => {
            await this.set("mod.moveFolderInsteadOfCopy", enabled);
        },
        getVirtualizationEnabled: async () => await this.get("mod.virtualizationEnabled"),
        setVirtualizationEnabled: async (enabled: boolean) => {
            await this.set("mod.virtualizationEnabled", enabled);
        },
        getVirtualizationThreshold: async () => await this.get("mod.virtualizationThreshold"),
        setVirtualizationThreshold: async (threshold: number) => {
            await this.set("mod.virtualizationThreshold", threshold);
        },
        getGridLayoutMode: async (): Promise<ModGridLayoutMode> =>
            await this.get("mod.gridLayoutMode"),
        setGridLayoutMode: async (mode: ModGridLayoutMode) => {
            await this.set("mod.gridLayoutMode", mode);
        },
        getGridResponsiveBaseWidth: async () => await this.get("mod.gridResponsiveBaseWidth"),
        setGridResponsiveBaseWidth: async (width: number) => {
            await this.set("mod.gridResponsiveBaseWidth", width);
        },
        getGridFixedCardWidth: async () => await this.get("mod.gridFixedCardWidth"),
        setGridFixedCardWidth: async (width: number) => {
            await this.set("mod.gridFixedCardWidth", width);
        },
        getGridFixedColumnCount: async () => await this.get("mod.gridFixedColumnCount"),
        setGridFixedColumnCount: async (count: number) => {
            await this.set("mod.gridFixedColumnCount", count);
        },
        getSearchModPreview: async () => await this.get("mod.searchModPreview"),
        setSearchModPreview: async (enabled: boolean) => {
            await this.set("mod.searchModPreview", enabled);
        },
        getCopyShaderFixesOnEnable: async () => await this.get("mod.copyShaderFixesOnEnable"),
        setCopyShaderFixesOnEnable: async (enabled: boolean) => {
            await this.set("mod.copyShaderFixesOnEnable", enabled);
        },
        getDisabledPrefixStyle: async (): Promise<DisabledPrefixStyle> =>
            await this.get("mod.disabledPrefixStyle"),
        setDisabledPrefixStyle: async (style: DisabledPrefixStyle) => {
            await this.set("mod.disabledPrefixStyle", style);
        },
    };

    transfer = {
        getDownloadConcurrency: async () => await this.get("transfer.downloadConcurrency"),
        setDownloadConcurrency: async (concurrency: number) =>
            await this.set("transfer.downloadConcurrency", concurrency),
        getDownloadBandwidthLimitMibps: async () =>
            await this.get("transfer.downloadBandwidthLimitMibps"),
        setDownloadBandwidthLimitMibps: async (mibps: number) =>
            await this.set("transfer.downloadBandwidthLimitMibps", mibps),
        getUploadConcurrency: async () => await this.get("transfer.uploadConcurrency"),
        setUploadConcurrency: async (concurrency: number) =>
            await this.set("transfer.uploadConcurrency", concurrency),
    };

    drive = {
        getNameSortPolicy: async (): Promise<DriveNameSortPolicy> =>
            await this.get("drive.nameSortPolicy"),
        setNameSortPolicy: async (policy: DriveNameSortPolicy) => {
            await this.set("drive.nameSortPolicy", policy);
        },
    };

    modelViewer = {
        getToneMapping: async () => await this.get("modelViewer.toneMapping"),
        setToneMapping: async (toneMapping: string) =>
            await this.set("modelViewer.toneMapping", normalizeModelViewerToneMapping(toneMapping)),
        getEnvironment: async () => await this.get("modelViewer.environment"),
        setEnvironment: async (environment: string) =>
            await this.set("modelViewer.environment", normalizeModelViewerEnvironment(environment)),
        getExposure: async () => await this.get("modelViewer.exposure"),
        setExposure: async (exposure: number) => await this.set("modelViewer.exposure", exposure),
    };

    xxmi = {
        getPersistToggles: async () => await this.get("xxmi.persistToggles"),
        getPersistLogs: async () => {
            return this.desktop.service.modTools.togglePersist.getPersistLogs();
        },
        setPersistToggles: async (enabled: boolean) =>
            await this.set("xxmi.persistToggles", enabled),
        getToggleViewerAutoGenerate: async () => await this.get("xxmi.toggleViewerAutoGenerate"),
        getToggleViewerHotkey: async () => await this.get("xxmi.toggleViewerHotkey"),
        getToggleViewerLogs: async () => {
            return this.desktop.service.modTools.toggleViewer.getLogs();
        },

        getToggleViewerState: async () => {
            return this.desktop.service.modTools.toggleViewer.getState();
        },

        runToggleViewerBatchGenerate: async () => {
            await this.desktop.service.modTools.toggleViewer.runBatchGenerate();
        },

        runToggleViewerBatchDelete: async () => {
            await this.desktop.service.modTools.toggleViewer.runBatchDelete();
        },

        cancelToggleViewerWork: async () => {
            this.desktop.service.modTools.toggleViewer.cancelCurrentWork();
        },
        setToggleViewerHotkey: async (hotkey: string) =>
            await this.set("xxmi.toggleViewerHotkey", hotkey),
        setToggleViewerAutoGenerate: async (enabled: boolean) =>
            await this.set("xxmi.toggleViewerAutoGenerate", enabled),
    };

    advanced = {
        getAll: async () => {
            const rows = await this.desktop.lib.db.settings.list();
            const sensitiveKeys = ["password", "token", "secret", "credentials"];

            return rows.map((row) => {
                const isSensitive = sensitiveKeys.some((k) => row.key.toLowerCase().includes(k));
                if (isSensitive) {
                    return { ...row, value: "********" };
                }
                return row;
            });
        },

        set: async (key: string, value: string) => {
            const existing = await this.desktop.lib.db.settings.get(key);

            if (!existing) {
                throw new Error(`Setting key "${key}" not found.`);
            }

            await this.desktop.lib.db.settings.updateValue(key, value);
            this.desktop.ipc.broadcast("setting:update", { key, value });
            this.desktop.ipc.broadcast("renderer:reload");
        },
    };
}

export default Setting;
