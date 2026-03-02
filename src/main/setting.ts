import type { NahidaDesktop } from "@main/index";
import { imageCache, setting } from "@main/internal/db/schema";
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
        await this.desktop.lib.db
            .update(setting)
            .set({ value: JSON.stringify(bounds) })
            .where(eq(setting.key, "bounds"));
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

        checkUpdate: async () => {
            await this.desktop.updater.checkForUpdates(true);
        },

        getCheckBackgroundUpdates: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "checkBackgroundUpdates"),
            });

            if (!qr) {
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "checkBackgroundUpdates", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setCheckBackgroundUpdates: async (enabled: boolean) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "checkBackgroundUpdates", value: String(enabled) })
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

    net = {
        getProxy: async () => {
            const qr = await this.desktop.lib.db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "net_proxy"),
            });

            if (!qr) {
                const defaultProxy = { type: "disabled" };
                await this.desktop.lib.db
                    .insert(setting)
                    .values({ key: "net_proxy", value: JSON.stringify(defaultProxy) });
                return defaultProxy;
            }

            return JSON.parse(qr.value as string);
        },

        // biome-ignore lint/suspicious/noExplicitAny: <>
        setProxy: async (settings: any) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "net_proxy", value: JSON.stringify(settings) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: JSON.stringify(settings) },
                });

            await this.desktop.updateProxy();
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

        setPersistToggles: async (enabled: boolean) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "xxmi_persist_toggles", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });

            if (this.desktop.service?.xxmi) {
                if (enabled) {
                    this.desktop.service.xxmi.startPersistWatcher();
                } else {
                    this.desktop.service.xxmi.stopPersistWatcher();
                }
            }
        },
    };

    advanced = {
        getAll: async () => {
            const rows = await this.desktop.lib.db.select().from(setting);
            const sensitiveKeys = ["proxy", "password", "token", "secret", "credentials"];

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
