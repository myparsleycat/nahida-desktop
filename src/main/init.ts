import { InitDB, db } from "@main/internal/db";
import { app, protocol } from "electron";
import { setting } from "./internal/db/schema";
import { NahidaProtocolHandler } from "./internal/protocol";
import { NahidaDesktop } from "./index";
import { startServer } from "./server";

export async function startInit(desktop: NahidaDesktop) {
    if (desktop.initialized) return;

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

    // register custom protocol
    protocol.handle("nahida", async (req) => await NahidaProtocolHandler(req));

    desktop.initialized = true;
}
