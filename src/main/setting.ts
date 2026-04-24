import type { NahidaDesktop } from "@main/index";
import { imageCache, setting } from "@main/internal/db/schema";
import { normalizeDriveNameSortPolicy, type DriveNameSortPolicy } from "@shared/drive";
import { ARCHIVE_EXTRACT_PATH_MODES, type ArchiveExtractPathMode } from "@shared/mod";
import { supportsWindowsDesktopFeatures } from "@shared/platform";
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

function clampTransferConcurrency(value: number, min: number, max: number, fallback: number) {
    if (!Number.isFinite(value)) {
        return fallback;
    }

    return Math.min(max, Math.max(min, Math.trunc(value)));
}

function normalizeModelViewerToneMapping(value: string | null | undefined) {
    return MODEL_VIEWER_TONE_MAPPINGS.includes(value as (typeof MODEL_VIEWER_TONE_MAPPINGS)[number])
        ? value
        : DEFAULT_MODEL_VIEWER_TONE_MAPPING;
}

function normalizeModelViewerEnvironment(value: string | null | undefined) {
    return MODEL_VIEWER_ENVIRONMENTS.includes(value as (typeof MODEL_VIEWER_ENVIRONMENTS)[number])
        ? value
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

export class Setting {
    private desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
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

        getLanguage: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "language"),
            });

            if (!qr) {
                const systemLocale = app.getSystemLocale();
                const language = ["ko", "en", "ja", "zh"].includes(systemLocale.split("-")[0])
                    ? systemLocale.split("-")[0]
                    : "en";
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "language", value: language });
                return language;
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
            await this.desktop.lib.db
                .insert(setting)
                .values({
                    key: "mod_archive_extract_path_mode",
                    value: mode,
                })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: mode },
                });
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
            await this.desktop.lib.db
                .insert(setting)
                .values({
                    key: "mod_delete_archive_after_extract",
                    value: String(enabled),
                })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
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
            await this.desktop.lib.db
                .insert(setting)
                .values({
                    key: "mod_move_folder_instead_of_copy",
                    value: String(enabled),
                })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
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
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "mod_virtualization_enabled", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
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
            await this.desktop.lib.db
                .insert(setting)
                .values({
                    key: "mod_virtualization_threshold",
                    value: String(threshold),
                })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(threshold) },
                });
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
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "mod_search_mod_preview", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
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
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "mod_copy_shader_fixes_on_enable", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
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
            const value = normalizeDriveNameSortPolicy(policy);

            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "drive_name_sort_policy", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
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
