import type { NahidaDesktop } from "@main/index";
import { imageCache, setting } from "@main/internal/db/schema";
import {
    AUTO_MOD_ACTIONS_SETTING_KEY,
    type AutoModActionsConfig,
    normalizeAutoModActionsConfig,
} from "@shared/auto-mod-actions";
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

export class Setting {
    private desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async getBounds() {
        const qr = await this.desktop.lib.db.query.setting.findFirst({
            where: (t, { eq }) => eq(t.key, "bounds"),
        });

        if (!qr) return null;

        const bounds = JSON.parse(qr.value as string) as Bounds;

        return bounds;
    }

    public async setBounds(bounds: Bounds) {
        const value = JSON.stringify(bounds);
        await this.desktop.lib.db
            .insert(setting)
            .values({ key: "bounds", value })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value },
            });
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

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "defaultStartPage", value: "/mod" });
                return "/mod";
            }

            return qr.value;
        },

        setDefaultStartPage: async (page: string | null) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "defaultStartPage", value: page || "/mod" })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: page || "/mod" },
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
            await this.desktop.window.main.createMainWindow();
            this.desktop.window.setting.focus();
        },

        getAutoUpdate: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "autoUpdate"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "autoUpdate", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setAutoUpdate: async (enabled: boolean) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "autoUpdate", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
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

        getAutoModActionsConfig: async (): Promise<AutoModActionsConfig> => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, AUTO_MOD_ACTIONS_SETTING_KEY),
            });

            const importerKeys = this.desktop.service.xxmi
                .getEnabledImporters()
                .map((importer) => importer.key);

            if (!qr?.value) {
                const defaultConfig = normalizeAutoModActionsConfig({}, importerKeys);
                await this.desktop.lib.db
                    .insert(setting)
                    .values({
                        key: AUTO_MOD_ACTIONS_SETTING_KEY,
                        value: JSON.stringify(defaultConfig),
                    })
                    .onConflictDoUpdate({
                        target: setting.key,
                        set: { value: JSON.stringify(defaultConfig) },
                    });
                return defaultConfig;
            }

            try {
                return normalizeAutoModActionsConfig(JSON.parse(qr.value), importerKeys);
            } catch {
                return normalizeAutoModActionsConfig({}, importerKeys);
            }
        },

        setAutoModActionsConfig: async (config: AutoModActionsConfig) => {
            const importerKeys = this.desktop.service.xxmi
                .getEnabledImporters()
                .map((importer) => importer.key);
            const normalizedConfig = normalizeAutoModActionsConfig(config, importerKeys);

            await this.desktop.lib.db
                .insert(setting)
                .values({
                    key: AUTO_MOD_ACTIONS_SETTING_KEY,
                    value: JSON.stringify(normalizedConfig),
                })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: JSON.stringify(normalizedConfig) },
                });

            if (this.desktop.service?.modTools) {
                await this.desktop.service.modTools.refreshAutoModActionsWatcher();
            }
        },

        restoreAutoModActionsBackups: async (importerKey: string) => {
            return await this.desktop.service.modTools.restoreAutoModActionsBackups(importerKey);
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
