import path from "node:path";
import { app } from "electron";
import { DB_FILE_NAME } from "../const";
import { DatabaseClient } from "./client";

export * from "./schema";

const dbPath = !app.isPackaged ? DB_FILE_NAME : path.join(app.getPath("userData"), "data.db");
export const db = new DatabaseClient(dbPath);

export async function InitDB() {
    await db.reconcile();
}
