import type { NahidaDesktop } from "@main/index";
import { imageCache, setting } from "@main/internal/db/schema";
import { normalizeDriveNameSortPolicy, type DriveNameSortPolicy } from "@shared/drive";
import {
    ARCHIVE_EXTRACT_PATH_MODES,
    MOD_GRID_LAYOUT_MODES,
    SIDEBAR_LAYOUT_MODES,
    type ArchiveExtractPathMode,
    type ModGridLayoutMode,
    type SidebarLayoutMode,
} from "@shared/mod";
import { supportsWindowsDesktopFeatures } from "@shared/platform";
import {
    APP_SETTINGS,
    type AppSettings,
    type SettingDefinition,
    type SettingKey,
} from "@shared/settings";
import type { AutoUpdateMode } from "@shared/updater";
import AutoLaunch from "auto-launch";
import { eq, sum } from "drizzle-orm";
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
const TRANSFER_UPLOAD_CONCURRENCY_DEFAULT = 8;
const TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX = [4, 16];
const TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT = 2;
const TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX = [1, 4];
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

function getDefaultStartPageForPlatform(platform: NodeJS.Platform) {
    return supportsWindowsDesktopFeatures(platform) ? "/mod" : "/transfer";
}

function sanitizeDefaultStartPage(page: string | null | undefined, platform: NodeJS.Platform) {
    const fallback = getDefaultStartPageForPlatform(platform);
    if (!page) {
        return fallback;
    }

    if (!supportsWindowsDesktopFeatures(platform) && page === "/mod") {
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
                getDefault: () => getDefaultStartPageForPlatform(process.platform),
                fromStored: (value) => sanitizeDefaultStartPage(value, process.platform),
                normalize: (value) => sanitizeDefaultStartPage(value, process.platform),
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
            "transfer.uploadCreateManyConcurrency": {
                definition: APP_SETTINGS["transfer.uploadCreateManyConcurrency"],
                getDefault: () => TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT,
                fromStored: (value) =>
                    clampTransferConcurrency(
                        Number.parseInt(value ?? "", 10),
                        TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[0],
                        TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[1],
                        TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT,
                    ),
                normalize: (value) =>
                    clampTransferConcurrency(
                        value,
                        TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[0],
                        TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[1],
                        TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT,
                    ),
                toStored: (value) =>
                    String(
                        clampTransferConcurrency(
                            value,
                            TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[0],
                            TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[1],
                            TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT,
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
                        await this.desktop.setting.general.setRunInBackground(true);
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
                        await this.desktop.setting.general.setRunInBackground(true);
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
        return await this.desktop.lib.db.query.setting.findFirst({
            where: (t, { eq }) => eq(t.key, storageKey),
        });
    }

    private async upsertStoredSetting(storageKey: string, value: string) {
        await this.desktop.lib.db
            .insert(setting)
            .values({ key: storageKey, value })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value },
            });
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
        const qr = await this.desktop.lib.db.query.setting.findFirst({
            where: (t, { eq }) => eq(t.key, key),
        });

        if (!qr) return null;

        const bounds = JSON.parse(qr.value as string) as Bounds;

        return bounds;
    }

    private async setStoredBounds(key: string, bounds: Bounds) {
        const value = JSON.stringify(bounds);
        await this.desktop.lib.db.insert(setting).values({ key, value }).onConflictDoUpdate({
            target: setting.key,
            set: { value },
        });
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
        getRunOnStartup: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "runOnStartup"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "runOnStartup", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setRunOnStartup: async (enabled: boolean) => {
            const current = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "runOnStartup"),
            });
            if (current) {
                await this.desktop.lib.db
                    .update(setting)
                    .set({ value: String(enabled) })
                    .where(eq(setting.key, "runOnStartup"));
            } else {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "runOnStartup", value: String(enabled) });
            }

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

        getLanguage: async (): Promise<string> => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "language"),
            });

            const systemLocale = app.getSystemLocale();
            const fallbackLanguage = ["ko", "en", "ja", "zh"].includes(systemLocale.split("-")[0])
                ? systemLocale.split("-")[0]
                : "en";

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "language", value: fallbackLanguage });
                return fallbackLanguage;
            }

            if (!qr.value) {
                await this.desktop.lib.db
                    .update(setting)
                    .set({ value: fallbackLanguage })
                    .where(eq(setting.key, "language"));
                return fallbackLanguage;
            }

            return qr.value;
        },

        setLanguage: async (language: string) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "language", value: language })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: language },
                });

            this.desktop.ipc.broadcast("language:update", language);
            await this.desktop.updater.handleLanguageChanged(language);
        },

        getMoveTransferPageWhenStartTransfer: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "moveTransferPageWhenStartTransfer"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "moveTransferPageWhenStartTransfer", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setMoveTransferPageWhenStartTransfer: async (enabled: boolean) => {
            const current = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "moveTransferPageWhenStartTransfer"),
            });
            if (current) {
                await this.desktop.lib.db
                    .update(setting)
                    .set({ value: String(enabled) })
                    .where(eq(setting.key, "moveTransferPageWhenStartTransfer"));
            } else {
                await this.desktop.lib.db.insert(setting).values({
                    key: "moveTransferPageWhenStartTransfer",
                    value: String(enabled),
                });
            }
        },

        getPowerSaveBlockInTransfer: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "powerSaveBlockInTransfer"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "powerSaveBlockInTransfer", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setPowerSaveBlockInTransfer: async (enabled: boolean) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "powerSaveBlockInTransfer", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
        },

        getDefaultStartPage: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "defaultStartPage"),
            });
            const defaultPage = getDefaultStartPageForPlatform(process.platform);

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "defaultStartPage", value: defaultPage });
                return defaultPage;
            }

            return sanitizeDefaultStartPage(qr.value, process.platform);
        },

        setDefaultStartPage: async (page: string | null) => {
            const nextPage = sanitizeDefaultStartPage(page, process.platform);
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "defaultStartPage", value: nextPage })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: nextPage },
                });
        },

        getTitlebarStyle: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "titlebarStyle"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "titlebarStyle", value: "modern" });
                return "modern";
            }

            return qr.value;
        },

        setTitlebarStyle: async (style: string) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "titlebarStyle", value: style })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: style },
                });

            const windows = BrowserWindow.getAllWindows();
            for (const window of windows) {
                window.close();
            }
            await this.desktop.window.main.focusAndNavigate("/setting/gen");
        },

        getAutoUpdateMode: async (): Promise<AutoUpdateMode> => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "autoUpdate"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "autoUpdate", value: "auto" });
                return "auto";
            }

            const mode = normalizeAutoUpdateMode(qr.value);

            if (qr.value !== mode) {
                await this.desktop.lib.db
                    .update(setting)
                    .set({ value: mode })
                    .where(eq(setting.key, "autoUpdate"));
            }

            return mode;
        },

        setAutoUpdateMode: async (mode: AutoUpdateMode) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "autoUpdate", value: mode })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: mode },
                });

            await this.desktop.updater.handleAutoUpdateModeChanged(mode);
        },

        getRunInBackground: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "runInBackground"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "runInBackground", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setRunInBackground: async (enabled: boolean) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "runInBackground", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
        },

        getImageCacheSize: async () => {
            const [result] = await this.desktop.lib.db
                .select({ totalSize: sum(imageCache.size) })
                .from(imageCache);
            return Number(result?.totalSize || 0);
        },

        clearImageCache: async () => {
            await this.desktop.lib.db.delete(imageCache);
        },

        getLogLevel: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "logLevel"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "logLevel", value: "error" });
                return "error";
            } else if (!qr.value) {
                return "error";
            }

            return qr.value as LogLevel;
        },

        setLogLevel: async (level: LogLevel) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "logLevel", value: level })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: level },
                });

            this.desktop.logger.setLevel(level);
        },
    };

    mod = {
        getSidebarLayout: async (): Promise<SidebarLayoutMode> => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_sidebar_layout"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "mod_sidebar_layout",
                    value: "row",
                });
                return "row";
            }

            const normalizedLayout = normalizeSidebarLayoutMode(qr.value);

            if (qr.value !== normalizedLayout) {
                await this.desktop.lib.db
                    .update(setting)
                    .set({ value: normalizedLayout })
                    .where(eq(setting.key, "mod_sidebar_layout"));
            }

            return normalizedLayout;
        },

        setSidebarLayout: async (mode: SidebarLayoutMode) => {
            await this.set("mod.sidebarLayout", mode);
        },

        getCharacterSidebarWidth: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_character_sidebar_width"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "mod_character_sidebar_width",
                    value: String(MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT),
                });
                return MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT;
            }

            const normalizedWidth = clampIntegerSetting(
                parseInt(qr.value as string, 10),
                MOD_CHARACTER_SIDEBAR_WIDTH_MIN,
                MOD_CHARACTER_SIDEBAR_WIDTH_MAX,
                MOD_CHARACTER_SIDEBAR_WIDTH_DEFAULT,
            );

            if (String(normalizedWidth) !== qr.value) {
                await this.desktop.lib.db
                    .update(setting)
                    .set({ value: String(normalizedWidth) })
                    .where(eq(setting.key, "mod_character_sidebar_width"));
            }

            return normalizedWidth;
        },

        setCharacterSidebarWidth: async (width: number) => {
            await this.set("mod.characterSidebarWidth", width);
        },

        getArchiveExtractPathMode: async (): Promise<ArchiveExtractPathMode> => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_archive_extract_path_mode"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "mod_archive_extract_path_mode",
                    value: "flatten_single_root",
                });
                return "flatten_single_root";
            }

            if (ARCHIVE_EXTRACT_PATH_MODES.includes(qr.value as ArchiveExtractPathMode)) {
                return qr.value as ArchiveExtractPathMode;
            }

            return "flatten_single_root";
        },

        setArchiveExtractPathMode: async (mode: ArchiveExtractPathMode) => {
            await this.set("mod.archiveExtractPathMode", mode);
        },

        getDeleteArchiveAfterExtract: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_delete_archive_after_extract"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "mod_delete_archive_after_extract", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setDeleteArchiveAfterExtract: async (enabled: boolean) => {
            await this.set("mod.deleteArchiveAfterExtract", enabled);
        },

        getMoveFolderInsteadOfCopy: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_move_folder_instead_of_copy"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "mod_move_folder_instead_of_copy", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setMoveFolderInsteadOfCopy: async (enabled: boolean) => {
            await this.set("mod.moveFolderInsteadOfCopy", enabled);
        },

        getVirtualizationEnabled: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_virtualization_enabled"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "mod_virtualization_enabled", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setVirtualizationEnabled: async (enabled: boolean) => {
            await this.set("mod.virtualizationEnabled", enabled);
        },

        getVirtualizationThreshold: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_virtualization_threshold"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "mod_virtualization_threshold", value: "30" });
                return 30;
            }

            return parseInt(qr.value as string) || 30;
        },

        setVirtualizationThreshold: async (threshold: number) => {
            await this.set("mod.virtualizationThreshold", threshold);
        },

        getGridLayoutMode: async (): Promise<ModGridLayoutMode> => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_grid_layout_mode"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "mod_grid_layout_mode", value: "responsive" });
                return "responsive";
            }

            return normalizeModGridLayoutMode(qr.value as string | null | undefined);
        },

        setGridLayoutMode: async (mode: ModGridLayoutMode) => {
            await this.set("mod.gridLayoutMode", mode);
        },

        getGridResponsiveBaseWidth: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_grid_responsive_base_width"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "mod_grid_responsive_base_width",
                    value: String(MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT),
                });
                return MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT;
            }

            return clampIntegerSetting(
                parseInt(qr.value as string, 10),
                MOD_GRID_WIDTH_MIN,
                MOD_GRID_WIDTH_MAX,
                MOD_GRID_RESPONSIVE_BASE_WIDTH_DEFAULT,
            );
        },

        setGridResponsiveBaseWidth: async (width: number) => {
            await this.set("mod.gridResponsiveBaseWidth", width);
        },

        getGridFixedCardWidth: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_grid_fixed_card_width"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "mod_grid_fixed_card_width",
                    value: String(MOD_GRID_FIXED_CARD_WIDTH_DEFAULT),
                });
                return MOD_GRID_FIXED_CARD_WIDTH_DEFAULT;
            }

            return clampIntegerSetting(
                parseInt(qr.value as string, 10),
                MOD_GRID_WIDTH_MIN,
                MOD_GRID_WIDTH_MAX,
                MOD_GRID_FIXED_CARD_WIDTH_DEFAULT,
            );
        },

        setGridFixedCardWidth: async (width: number) => {
            await this.set("mod.gridFixedCardWidth", width);
        },

        getGridFixedColumnCount: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_grid_fixed_column_count"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "mod_grid_fixed_column_count",
                    value: String(MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT),
                });
                return MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT;
            }

            return clampIntegerSetting(
                parseInt(qr.value as string, 10),
                MOD_GRID_COLUMN_MIN,
                MOD_GRID_COLUMN_MAX,
                MOD_GRID_FIXED_COLUMN_COUNT_DEFAULT,
            );
        },

        setGridFixedColumnCount: async (count: number) => {
            await this.set("mod.gridFixedColumnCount", count);
        },

        getSearchModPreview: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_search_mod_preview"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "mod_search_mod_preview", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setSearchModPreview: async (enabled: boolean) => {
            await this.set("mod.searchModPreview", enabled);
        },

        getCopyShaderFixesOnEnable: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_copy_shader_fixes_on_enable"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "mod_copy_shader_fixes_on_enable", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setCopyShaderFixesOnEnable: async (enabled: boolean) => {
            await this.set("mod.copyShaderFixesOnEnable", enabled);
        },
    };

    transfer = {
        getDownloadConcurrency: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "transfer_download_concurrency"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "transfer_download_concurrency",
                    value: String(TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT),
                });
                return TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT;
            }

            return clampTransferConcurrency(
                parseInt(qr.value as string, 10),
                TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[0],
                TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[1],
                TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT,
            );
        },

        setDownloadConcurrency: async (concurrency: number) => {
            const value = String(
                clampTransferConcurrency(
                    concurrency,
                    TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[0],
                    TRANSFER_DOWNLOAD_CONCURRENCY_MIN_MAX[1],
                    TRANSFER_DOWNLOAD_CONCURRENCY_DEFAULT,
                ),
            );

            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "transfer_download_concurrency", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
        },

        getUploadConcurrency: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "transfer_upload_concurrency"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "transfer_upload_concurrency",
                    value: String(TRANSFER_UPLOAD_CONCURRENCY_DEFAULT),
                });
                return TRANSFER_UPLOAD_CONCURRENCY_DEFAULT;
            }

            return clampTransferConcurrency(
                parseInt(qr.value as string, 10),
                TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[0],
                TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[1],
                TRANSFER_UPLOAD_CONCURRENCY_DEFAULT,
            );
        },

        setUploadConcurrency: async (concurrency: number) => {
            const value = String(
                clampTransferConcurrency(
                    concurrency,
                    TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[0],
                    TRANSFER_UPLOAD_CONCURRENCY_MIN_MAX[1],
                    TRANSFER_UPLOAD_CONCURRENCY_DEFAULT,
                ),
            );

            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "transfer_upload_concurrency", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
        },

        getUploadCreateManyConcurrency: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "transfer_upload_create_many_concurrency"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "transfer_upload_create_many_concurrency",
                    value: String(TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT),
                });
                return TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT;
            }

            return clampTransferConcurrency(
                parseInt(qr.value as string, 10),
                TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[0],
                TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[1],
                TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT,
            );
        },

        setUploadCreateManyConcurrency: async (concurrency: number) => {
            const value = String(
                clampTransferConcurrency(
                    concurrency,
                    TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[0],
                    TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_MIN_MAX[1],
                    TRANSFER_UPLOAD_CREATE_MANY_CONCURRENCY_DEFAULT,
                ),
            );

            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "transfer_upload_create_many_concurrency", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
        },
    };

    drive = {
        getNameSortPolicy: async (): Promise<DriveNameSortPolicy> => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "drive_name_sort_policy"),
            });

            if (!qr) {
                const value = normalizeDriveNameSortPolicy(null);
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "drive_name_sort_policy", value })
                    .onConflictDoNothing();
                return value;
            }

            return normalizeDriveNameSortPolicy(qr.value);
        },

        setNameSortPolicy: async (policy: DriveNameSortPolicy) => {
            await this.set("drive.nameSortPolicy", policy);
        },
    };

    modelViewer = {
        getToneMapping: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "model_viewer_tone_mapping"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "model_viewer_tone_mapping",
                    value: DEFAULT_MODEL_VIEWER_TONE_MAPPING,
                });
                return DEFAULT_MODEL_VIEWER_TONE_MAPPING;
            }

            return normalizeModelViewerToneMapping(qr.value);
        },

        setToneMapping: async (toneMapping: string) => {
            const value = normalizeModelViewerToneMapping(toneMapping);

            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "model_viewer_tone_mapping", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
        },

        getEnvironment: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "model_viewer_environment"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "model_viewer_environment",
                    value: DEFAULT_MODEL_VIEWER_ENVIRONMENT,
                });
                return DEFAULT_MODEL_VIEWER_ENVIRONMENT;
            }

            return normalizeModelViewerEnvironment(qr.value);
        },

        setEnvironment: async (environment: string) => {
            const value = normalizeModelViewerEnvironment(environment);

            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "model_viewer_environment", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
        },

        getExposure: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "model_viewer_exposure"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "model_viewer_exposure",
                    value: String(DEFAULT_MODEL_VIEWER_EXPOSURE),
                });
                return DEFAULT_MODEL_VIEWER_EXPOSURE;
            }

            return clampModelViewerExposure(Number.parseFloat(qr.value as string));
        },

        setExposure: async (exposure: number) => {
            const value = String(clampModelViewerExposure(exposure));

            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "model_viewer_exposure", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
        },
    };

    xxmi = {
        getPersistToggles: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "xxmi_persist_toggles"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "xxmi_persist_toggles", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        getPersistLogs: async () => {
            return this.desktop.service.modTools.togglePersist.getPersistLogs();
        },

        setPersistToggles: async (enabled: boolean) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "xxmi_persist_toggles", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });

            if (enabled) {
                await this.desktop.setting.general.setRunInBackground(true);
            }

            if (this.desktop.service?.modTools) {
                if (enabled) {
                    this.desktop.service.modTools.startPersistWatcher();
                } else {
                    this.desktop.service.modTools.stopPersistWatcher();
                }
            }
        },

        getToggleViewerAutoGenerate: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "xxmi_toggle_viewer_auto_generate"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "xxmi_toggle_viewer_auto_generate",
                    value: "false",
                });
                return false;
            }

            return qr.value === "true";
        },

        getToggleViewerHotkey: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "xxmi_toggle_viewer_hotkey"),
            });

            if (!qr) {
                await this.desktop.lib.db.insert(setting).values({
                    key: "xxmi_toggle_viewer_hotkey",
                    value: DEFAULT_TOGGLE_VIEWER_HOTKEY,
                });
                return DEFAULT_TOGGLE_VIEWER_HOTKEY;
            }

            const value = (qr.value || "").trim();
            return value || DEFAULT_TOGGLE_VIEWER_HOTKEY;
        },

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

        setToggleViewerHotkey: async (hotkey: string) => {
            const normalizedHotkey = hotkey.trim() || DEFAULT_TOGGLE_VIEWER_HOTKEY;

            await this.desktop.lib.db
                .insert(setting)
                .values({
                    key: "xxmi_toggle_viewer_hotkey",
                    value: normalizedHotkey,
                })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: normalizedHotkey },
                });

            if (this.desktop.service?.modTools) {
                await this.desktop.service.modTools.toggleViewer.applyHotkeyToArtifacts(
                    normalizedHotkey,
                );
            }
        },

        setToggleViewerAutoGenerate: async (enabled: boolean) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({
                    key: "xxmi_toggle_viewer_auto_generate",
                    value: String(enabled),
                })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });

            if (enabled) {
                await this.desktop.setting.general.setRunInBackground(true);
            }

            if (this.desktop.service?.modTools) {
                if (enabled) {
                    const toggleViewerState = this.desktop.service.modTools.toggleViewer.getState();
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
    };

    advanced = {
        getAll: async () => {
            const rows = await this.desktop.lib.db.select().from(setting);
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
            const existing = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, key),
            });

            if (!existing) {
                throw new Error(`Setting key "${key}" not found.`);
            }

            await this.desktop.lib.db.update(setting).set({ value }).where(eq(setting.key, key));
            this.desktop.ipc.broadcast("setting:update", { key, value });
            this.desktop.ipc.broadcast("renderer:reload");
        },
    };
}

export default Setting;
