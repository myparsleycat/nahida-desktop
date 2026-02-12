import type { NahidaDesktop } from "@main/index";
import { db } from "@main/internal/db";
import { imageCache, setting } from "@main/internal/db/schema";
import AutoLaunch from "auto-launch";
import { eq, sum } from "drizzle-orm";
import { app } from "electron";

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
        const qr = await db.query.setting.findFirst({
            where: (t, { eq }) => eq(t.key, "bounds"),
        });

        if (!qr) return null;

        const bounds = JSON.parse(qr.value as string) as Bounds;

        return bounds;
    }

    public async setBounds(bounds: Bounds) {
        await db
            .update(setting)
            .set({ value: JSON.stringify(bounds) })
            .where(eq(setting.key, "bounds"));
    }

    general = {
        getRunOnStartup: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "runOnStartup"),
            });

            if (!qr) {
                await db.insert(setting).values({ key: "runOnStartup", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setRunOnStartup: async (enabled: boolean) => {
            const current = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "runOnStartup"),
            });
            if (current) {
                await db
                    .update(setting)
                    .set({ value: String(enabled) })
                    .where(eq(setting.key, "runOnStartup"));
            } else {
                await db.insert(setting).values({ key: "runOnStartup", value: String(enabled) });
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
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "language"),
            });

            if (!qr) {
                const systemLocale = app.getSystemLocale();
                const language = ["ko", "en", "ja", "zh"].includes(systemLocale.split("-")[0])
                    ? systemLocale.split("-")[0]
                    : "en";
                await db.insert(setting).values({ key: "language", value: language });
                return language;
            }

            return qr.value;
        },

        setLanguage: async (language: string) => {
            await db
                .insert(setting)
                .values({ key: "language", value: language })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: language },
                });

            this.desktop.ipc.broadcast("language:update", language);
        },

        getMoveTransferPageWhenStartTransfer: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "moveTransferPageWhenStartTransfer"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "moveTransferPageWhenStartTransfer", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setMoveTransferPageWhenStartTransfer: async (enabled: boolean) => {
            const current = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "moveTransferPageWhenStartTransfer"),
            });
            if (current) {
                await db
                    .update(setting)
                    .set({ value: String(enabled) })
                    .where(eq(setting.key, "moveTransferPageWhenStartTransfer"));
            } else {
                await db
                    .insert(setting)
                    .values({ key: "moveTransferPageWhenStartTransfer", value: String(enabled) });
            }
        },

        getPowerSaveBlockInTransfer: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "powerSaveBlockInTransfer"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "powerSaveBlockInTransfer", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setPowerSaveBlockInTransfer: async (enabled: boolean) => {
            await db
                .insert(setting)
                .values({ key: "powerSaveBlockInTransfer", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
        },

        getDefaultStartPage: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "defaultStartPage"),
            });

            if (!qr) {
                await db.insert(setting).values({ key: "defaultStartPage", value: "/mod" });
                return "/mod";
            }

            return qr.value;
        },

        setDefaultStartPage: async (page: string | null) => {
            await db
                .insert(setting)
                .values({ key: "defaultStartPage", value: page || "/mod" })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: page || "/mod" },
                });
        },

        checkUpdate: async () => {
            await this.desktop.updater.checkForUpdates(true);
        },

        getCheckBackgroundUpdates: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "checkBackgroundUpdates"),
            });

            if (!qr) {
                await db.insert(setting).values({ key: "checkBackgroundUpdates", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setCheckBackgroundUpdates: async (enabled: boolean) => {
            await db
                .insert(setting)
                .values({ key: "checkBackgroundUpdates", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
        },

        getGameFolderCompressionEnabled: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "gameFolderCompressionEnabled"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "gameFolderCompressionEnabled", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setGameFolderCompressionEnabled: async (enabled: boolean) => {
            await db
                .insert(setting)
                .values({ key: "gameFolderCompressionEnabled", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });

            this.desktop.ipc.broadcast("setting:update", {
                key: "gameFolderCompressionEnabled",
                value: enabled,
            });

            if (this.desktop.lib?.compact) {
                this.desktop.lib.compact.updateCompression();
            }
        },

        getGameFolderCompressionFeatureEnabled: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "gameFolderCompressionFeatureEnabled"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "gameFolderCompressionFeatureEnabled", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setGameFolderCompressionFeatureEnabled: async (enabled: boolean) => {
            await db
                .insert(setting)
                .values({ key: "gameFolderCompressionFeatureEnabled", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });

            this.desktop.ipc.broadcast("setting:update", {
                key: "gameFolderCompressionFeatureEnabled",
                value: enabled,
            });

            if (this.desktop.lib?.compact) {
                this.desktop.lib.compact.updateFeature();
            }
        },

        getImageCacheSize: async () => {
            const [result] = await db.select({ totalSize: sum(imageCache.size) }).from(imageCache);
            return Number(result?.totalSize || 0);
        },

        clearImageCache: async () => {
            await db.delete(imageCache);
        },
    };

    mod = {
        getDeleteArchiveAfterExtract: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_delete_archive_after_extract"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "mod_delete_archive_after_extract", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setDeleteArchiveAfterExtract: async (enabled: boolean) => {
            await db
                .insert(setting)
                .values({ key: "mod_delete_archive_after_extract", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
        },

        getMoveFolderInsteadOfCopy: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_move_folder_instead_of_copy"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "mod_move_folder_instead_of_copy", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setMoveFolderInsteadOfCopy: async (enabled: boolean) => {
            await db
                .insert(setting)
                .values({ key: "mod_move_folder_instead_of_copy", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
        },

        getVirtualizationEnabled: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_virtualization_enabled"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "mod_virtualization_enabled", value: "true" });
                return true;
            }

            return qr.value === "true";
        },

        setVirtualizationEnabled: async (enabled: boolean) => {
            await db
                .insert(setting)
                .values({ key: "mod_virtualization_enabled", value: String(enabled) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(enabled) },
                });
        },

        getVirtualizationThreshold: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_virtualization_threshold"),
            });

            if (!qr) {
                await db
                    .insert(setting)
                    .values({ key: "mod_virtualization_threshold", value: "30" });
                return 30;
            }

            return parseInt(qr.value as string) || 30;
        },

        setVirtualizationThreshold: async (threshold: number) => {
            await db
                .insert(setting)
                .values({ key: "mod_virtualization_threshold", value: String(threshold) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: String(threshold) },
                });
        },

        getSearchModPreview: async () => {
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "mod_search_mod_preview"),
            });

            if (!qr) {
                await db.insert(setting).values({ key: "mod_search_mod_preview", value: "false" });
                return false;
            }

            return qr.value === "true";
        },

        setSearchModPreview: async (enabled: boolean) => {
            await db
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
            const qr = await db.query.setting.findFirst({
                where: (t, { eq }) => eq(t.key, "net_proxy"),
            });

            if (!qr) {
                const defaultProxy = { type: "disabled" };
                await db
                    .insert(setting)
                    .values({ key: "net_proxy", value: JSON.stringify(defaultProxy) });
                return defaultProxy;
            }

            return JSON.parse(qr.value as string);
        },

        // biome-ignore lint/suspicious/noExplicitAny: <>
        setProxy: async (settings: any) => {
            await db
                .insert(setting)
                .values({ key: "net_proxy", value: JSON.stringify(settings) })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: JSON.stringify(settings) },
                });

            await this.desktop.updateProxy();
        },
    };

    advanced = {
        getAll: async () => {
            return await db.select().from(setting);
        },

        set: async (key: string, value: string) => {
            await db.update(setting).set({ value }).where(eq(setting.key, key));
            this.desktop.ipc.broadcast("setting:update", { key, value });
        },
    };
}

export default Setting;
