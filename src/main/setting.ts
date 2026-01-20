import { eq } from "drizzle-orm";
import { db } from "@main/internal/db";
import { setting } from "@main/internal/db/schema";
import AutoLaunch from "auto-launch";
import { app } from "electron";
import type { NahidaDesktop } from "@main/index";

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
    };
}

export default Setting;
