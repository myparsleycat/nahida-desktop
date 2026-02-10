import { db, InitDB } from "@main/internal/db";
import { app, protocol } from "electron";
import type { NahidaDesktop } from "./index";
import { setting } from "./internal/db/schema";
import { NahidaProtocolHandler } from "./internal/protocol";
import { startServer } from "./server";

export async function startInit(desktop: NahidaDesktop) {
    if (desktop.initialized) return;

    desktop.lib.native.startTracking();

    // init db
    await InitDB();

    // init lang
    const lang = await db.query.setting.findFirst({
        where: (t, { eq }) => eq(t.key, "language"),
    });
    if (!lang) {
        const locale = app.getLocale();
        if (locale.startsWith("en"))
            await db.insert(setting).values({ key: "language", value: "en" });
        else if (locale === "ko") await db.insert(setting).values({ key: "language", value: "ko" });
        else if (locale.startsWith("zh"))
            await db.insert(setting).values({ key: "language", value: "zh" });
        else await db.insert(setting).values({ key: "language", value: "en" });
    }

    // make server
    try {
        await startServer();
    } catch (error) {
        desktop.logger.error(`Failed to start server on port 1027: ${error}`, "Server");
        throw error;
    }

    // make tray
    // createTray();
    // 로그인 후에 만들도록 순서 변경함

    const plist = await desktop.lib.native.getProcessList();
    const zenless = plist.find((p) => p.name.toLowerCase().includes("zenless"));
    if (zenless) {
        const topmostPid = desktop.lib.native.getTopmostPid([zenless.pid]);
        console.log("topmostPid", topmostPid);
    }

    // register custom protocol
    protocol.handle("nahida", async (req) => await NahidaProtocolHandler(req));

    desktop.initialized = true;
}
