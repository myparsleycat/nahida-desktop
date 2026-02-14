import path from "node:path";
import type { drizzle } from "drizzle-orm/better-sqlite3";
import { migrate } from "drizzle-orm/better-sqlite3/migrator";
import { setting } from "./schema";

export function runMigrations(db: ReturnType<typeof drizzle>) {
    const migrationsFolder = path.join(__dirname, "../../drizzle");
    migrate(db, { migrationsFolder });
}

export async function InitDB(db: ReturnType<typeof drizzle>) {
    runMigrations(db);
    await db.insert(setting).values({ key: "token" }).onConflictDoNothing();
    await db.insert(setting).values({ key: "bounds" }).onConflictDoNothing();
}
