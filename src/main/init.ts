import { db } from "@core/db";
import server from "@core/server";
import { app } from "electron";
import { createTray } from "./tray";

let initialized = false;

export async function startInit() {
    if (initialized) return;

    await db.init();

    // init lang
    const lang = await db.get('LocalStorage', 'language');
    if (!lang) {
        const locale = app.getLocale();
        if (locale.startsWith('en')) await db.update('LocalStorage', 'language', 'en');
        else if (locale === 'ko') await db.update('LocalStorage', 'language', 'ko');
        else if (locale.startsWith('zh')) await db.update('LocalStorage', 'language', 'zh');
    }

    // make server
    server.listen(14327, ({ hostname, port }) => {
        console.info(`server is running at ${hostname}:${port}`);
    });

    // make tray
    createTray();

    initialized = true;
}