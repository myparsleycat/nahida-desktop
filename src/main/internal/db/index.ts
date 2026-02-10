import path from "node:path";
import Database from "better-sqlite3";
import { drizzle } from "drizzle-orm/better-sqlite3";
import { migrate } from "drizzle-orm/better-sqlite3/migrator";
import { app } from "electron";
import { DB_FILE_NAME } from "../const";
import * as schema from "./schema";
import { setting } from "./schema";

const isDev = !app.isPackaged;
export const dbPath = isDev ? DB_FILE_NAME : path.join(app.getPath("userData"), "data.db");

const sqlite = new Database(dbPath);
export const db = drizzle(sqlite, { schema });

export function runMigrations() {
    const migrationsFolder = path.join(__dirname, "../../drizzle");
    migrate(db, { migrationsFolder });
}

export async function InitDB() {
    runMigrations();
    await db.insert(setting).values({ key: "token" }).onConflictDoNothing();
    await db.insert(setting).values({ key: "bounds" }).onConflictDoNothing();
}
