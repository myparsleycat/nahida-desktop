import path from "node:path";
import type { drizzle } from "drizzle-orm/better-sqlite3";
import { migrate } from "drizzle-orm/better-sqlite3/migrator";

export async function InitDB(db: ReturnType<typeof drizzle>) {
    const migrationsFolder = path.join(__dirname, "../../drizzle");
    migrate(db, { migrationsFolder });
}
