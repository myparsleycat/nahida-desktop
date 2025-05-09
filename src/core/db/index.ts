import { fileTypeFromBuffer } from "file-type";
import Database from "better-sqlite3";
import log from 'electron-log';
import type BetterSqlite3 from 'better-sqlite3';

interface StorageKeyValues {
  sess: string | null;
  language: string;
  img_cache_on: boolean;
  mods_resizable_default: number;
  bounds: {
    x: number | null;
    y: number | null;
    width: number;
    height: number;
  }
}

// type ObjectStorageKeys = 'bounds';

interface ImageCacheItem {
  id: string;
  image: Buffer;
  size: number;
  mimeType: string;
  createdAt: string;
  lastUsedAt: string;
}

interface ModFolders {
  id: string;
  path: string;
  name: string;
  parentId: string | null;
  createdAt: string;
}

type TableName = "LocalStorage" | "ImageCache" | "ModFolders";

type LocalStorageKey = keyof StorageKeyValues;
type ImageCacheKey = string;
type ModFoldersKey = string;

type LocalStorageValue<K extends LocalStorageKey> = StorageKeyValues[K];
type ImageCacheValue = Omit<ImageCacheItem, "id">;
type ModFoldersValue = Omit<ModFolders, "id">;

interface TableSchema {
  name: string;
  createStatement: string;
}

const defaultValues: StorageKeyValues = {
  sess: null,
  language: "en",
  img_cache_on: true,
  mods_resizable_default: 25,
  bounds: {
    x: null,
    y: null,
    width: 1000,
    height: 670
  }
};

const tableSchemas: TableSchema[] = [
  {
    name: "LocalStorage",
    createStatement: `
      CREATE TABLE IF NOT EXISTS LocalStorage (
        key TEXT PRIMARY KEY,
        value TEXT
      )
    `
  },
  {
    name: "ImageCache",
    createStatement: `
      CREATE TABLE IF NOT EXISTS ImageCache (
        id TEXT PRIMARY KEY,
        image BLOB,
        size INTEGER,
        mimeType TEXT,
        createdAt TEXT,
        lastUsedAt TEXT
      )
    `
  },
  {
    name: "ModFolders",
    createStatement: `
      CREATE TABLE IF NOT EXISTS ModFolders (
        id TEXT PRIMARY KEY,
        path TEXT UNIQUE,
        name TEXT,
        parentId TEXT NULL,
        createdAt TEXT NOT NULL,
        FOREIGN KEY (parentId) REFERENCES ModFolders(id) 
          ON DELETE SET NULL 
          ON UPDATE CASCADE
      )
    `
  }
];

// function isObjectKey(key: LocalStorageKey): key is ObjectStorageKeys {
//   return ['bounds'].includes(key as string);
// }

interface TableHandler<K, V> {
  get(key: K): Promise<V | null>;
  insert(key: K, value: V): Promise<void>;
  update(key: K, value: V): Promise<void>;
  del?(key: K): Promise<boolean>;
}

export class LocalStorageTableHandler implements TableHandler<LocalStorageKey, any> {
  constructor(private dbHandler: DbHandler) { }

  async get<K extends LocalStorageKey>(key: K): Promise<LocalStorageValue<K> | null> {
    return this.dbHandler.get("LocalStorage", key);
  }

  async insert<K extends LocalStorageKey>(key: K, value: LocalStorageValue<K>): Promise<void> {
    return this.dbHandler.insert("LocalStorage", key, value);
  }

  async update<K extends LocalStorageKey>(key: K, value: LocalStorageValue<K>): Promise<void> {
    return this.dbHandler.update("LocalStorage", key, value);
  }
}

class ImageCacheTableHandler implements TableHandler<ImageCacheKey, ImageCacheItem | Buffer | Partial<ImageCacheValue>> {
  constructor(private dbHandler: DbHandler) { }

  async get(key: ImageCacheKey): Promise<ImageCacheItem | null> {
    return this.dbHandler.get("ImageCache", key);
  }

  async insert(key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void> {
    return this.dbHandler.insert("ImageCache", key, value);
  }

  async update(key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void> {
    return this.dbHandler.update("ImageCache", key, value);
  }

  async del(key: ImageCacheKey): Promise<boolean> {
    return this.dbHandler.del("ImageCache", key);
  }
}

class ModFoldersTableHandler implements TableHandler<ModFoldersKey, ModFolders | Partial<ModFoldersValue>> {
  constructor(private dbHandler: DbHandler) { }

  async get(key: ModFoldersKey): Promise<ModFolders | null> {
    return this.dbHandler.get("ModFolders", key);
  }

  async insert(key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void> {
    return this.dbHandler.insert("ModFolders", key, value);
  }

  async update(key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void> {
    return this.dbHandler.update("ModFolders", key, value);
  }

  async del(key: ModFoldersKey): Promise<boolean> {
    return this.dbHandler.del("ModFolders", key);
  }
}

class DbHandler {
  private db: BetterSqlite3.Database | null = null;

  async init(): Promise<BetterSqlite3.Database> {
    if (this.db) {
      console.log("DB already initialized");
      log.info("DB already initialized");
      return this.db;
    }

    try {
      this.db = new Database('./database.db');
      console.log("Connected to the SQLite database");
      log.info("Connected to the SQLite database");

      this.createTables(tableSchemas);
      await this.initializeDefaultValues();

      return this.db;
    } catch (err) {
      console.error("Error during database initialization:", err);
      log.error("Error during database initialization", err);
      throw err;
    }
  }

  table<T extends TableName>(tableName: T)
    : T extends "LocalStorage"
    ? LocalStorageTableHandler
    : T extends "ImageCache"
    ? ImageCacheTableHandler
    : T extends "ModFolders"
    ? ModFoldersTableHandler
    : never {

    if (tableName === "LocalStorage") {
      return new LocalStorageTableHandler(this) as any;
    } else if (tableName === "ImageCache") {
      return new ImageCacheTableHandler(this) as any;
    } else if (tableName === "ModFolders") {
      return new ModFoldersTableHandler(this) as any;
    }

    throw new Error(`Unsupported table: ${tableName}`);
  }

  private createTables(schemas: TableSchema[]) {
    for (const schema of schemas) {
      this.createTable(schema);
      console.log(`${schema.name} table ready`);
    }
  }

  private createTable(schema: TableSchema): void {
    this.db!.exec(schema.createStatement);
  }

  private async initializeDefaultValues() {
    const keys = Object.keys(defaultValues) as (keyof StorageKeyValues)[];

    for (const key of keys) {
      const existingRow = this.checkIfKeyExists(key);

      if (!existingRow) {
        const value = defaultValues[key];
        await this.insert("LocalStorage", key, value);
        console.log(`Initialized key '${key}' with default value:`, value);
      }
    }
  }

  private checkIfKeyExists<K extends LocalStorageKey>(key: K) {
    try {
      const row = this.db!.prepare("SELECT 1 FROM LocalStorage WHERE key = ?").get(key);
      return !!row;
    } catch (err) {
      throw err;
    }
  }

  async getDb(): Promise<BetterSqlite3.Database> {
    if (!this.db) {
      return await this.init();
    }
    return this.db;
  }

  async get<K extends LocalStorageKey>(table: "LocalStorage", key: K): Promise<LocalStorageValue<K> | null>;
  async get(table: "ImageCache", key: ImageCacheKey): Promise<ImageCacheItem | null>;
  async get(table: "ModFolders", key: ModFoldersKey): Promise<ModFolders | null>;
  async get<T = any>(table: TableName = "LocalStorage", key: string): Promise<T | null> {
    try {
      const db = await this.getDb();
      let query: string;

      if (table === "LocalStorage") {
        query = "SELECT value FROM LocalStorage WHERE key = ?";
      } else if (table === "ImageCache") {
        query = "SELECT id, image, size, mimeType, createdAt, lastUsedAt FROM ImageCache WHERE id = ?";
      } else if (table === "ModFolders") {
        query = "SELECT id, path, name, parentId, createdAt FROM ModFolders WHERE id = ?";
      } else {
        throw new Error(`Unsupported table: ${table}`);
      }

      const stmt = db.prepare(query);
      const row: any = stmt.get(key);

      if (!row) {
        if (table === "LocalStorage" && key in defaultValues) {
          return (defaultValues as any)[key] as T;
        } else {
          return null;
        }
      }

      if (table === "LocalStorage") {
        if (row.value === null) {
          return null;
        }

        try {
          const value = JSON.parse(row.value);
          return value as T;
        } catch {
          return row.value as T;
        }
      } else {
        return row as T;
      }
    } catch (err) {
      log.error(err);
      throw err;
    }
  }

  async insert<K extends LocalStorageKey>(table: "LocalStorage" | undefined, key: K, value: LocalStorageValue<K>): Promise<void>;
  async insert(table: "ImageCache", key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void>;
  async insert(table: "ModFolders", key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void>;
  async insert(table: TableName = "LocalStorage", key: string, value: any): Promise<void> {
    try {
      const db = await this.getDb();
      let query: string;
      let params: any[];

      const exists = await this.keyExists(table, key);
      if (exists) {
        throw new Error(`Key '${key}' already exists in table '${table}'. Use update method instead.`);
      }

      if (table === "LocalStorage") {
        const insertStmt = db.prepare("INSERT INTO LocalStorage (key, value) VALUES (?, ?)");

        if (value === null) {
          insertStmt.run(key, null);
        } else {
          const stringValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
          insertStmt.run(key, stringValue);
        }
      } else if (table === "ImageCache") {
        const now = new Date().toISOString();

        if (Buffer.isBuffer(value)) {
          const filetype = await fileTypeFromBuffer(value);
          const insertStmt = db.prepare("INSERT INTO ImageCache (id, image, size, mimeType, createdAt, lastUsedAt) VALUES (?, ?, ?, ?, ?, ?)");
          insertStmt.run(key, value, value.byteLength, filetype?.mime || 'application/octet-stream', now, now);
        } else if (typeof value === 'object' && 'image' in value && Buffer.isBuffer(value.image)) {
          const insertStmt = db.prepare("INSERT INTO ImageCache (id, image, size, mimeType, createdAt, lastUsedAt) VALUES (?, ?, ?, ?, ?, ?)");
          insertStmt.run(
            key,
            value.image,
            value.size,
            value.mimeType || 'application/octet-stream',
            value.createdAt || now,
            value.lastUsedAt || now
          );
        } else {
          throw new Error(`Invalid image value for ImageCache`);
        }
      } else if (table === "ModFolders") {
        const now = new Date().toISOString();
        const insertStmt = db.prepare("INSERT INTO ModFolders (id, path, name, parentId, createdAt) VALUES (?, ?, ?, ?, ?)");
        insertStmt.run(
          key,
          value.path || '',
          value.name || '',
          value.parentId || null,
          value.createdAt || now
        );
      } else {
        throw new Error(`Unsupported table: ${table}`);
      }
    } catch (err) {
      log.error(err);
      throw err;
    }
  }

  async update<K extends LocalStorageKey>(table: "LocalStorage" | undefined, key: K, value: LocalStorageValue<K>): Promise<void>;
  async update(table: "ImageCache", key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void>;
  async update(table: "ModFolders", key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void>;
  async update(table: TableName = "LocalStorage", key: string, value: any): Promise<void> {
    try {
      const db = await this.getDb();

      const exists = await this.keyExists(table, key);
      if (!exists) {
        throw new Error(`Key '${key}' does not exist in table '${table}'. Use insert method instead.`);
      }

      if (table === "LocalStorage") {
        const updateStmt = db.prepare("UPDATE LocalStorage SET value = ? WHERE key = ?");

        if (value === null) {
          updateStmt.run(null, key);
        } else {
          const stringValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
          updateStmt.run(stringValue, key);
        }
      } else if (table === "ImageCache") {
        const now = new Date().toISOString();

        if (Buffer.isBuffer(value)) {
          const filetype = await fileTypeFromBuffer(value);
          const updateStmt = db.prepare("UPDATE ImageCache SET image = ?, size = ?, mimeType = ?, lastUsedAt = ? WHERE id = ?");
          updateStmt.run(value, value.byteLength, filetype?.mime || 'application/octet-stream', now, key);
        } else if (typeof value === 'object') {
          const setClauses: string[] = [];
          const params: any[] = [];

          if ('image' in value && Buffer.isBuffer(value.image)) {
            setClauses.push("image = ?");
            params.push(value.image);

            if (!('size' in value)) {
              setClauses.push("size = ?");
              params.push(value.image.byteLength);
            }

            if (!('mimeType' in value)) {
              const filetype = await fileTypeFromBuffer(value.image);
              setClauses.push("mimeType = ?");
              params.push(filetype?.mime || 'application/octet-stream');
            }
          }

          if ('size' in value) {
            setClauses.push("size = ?");
            params.push(value.size);
          }

          if ('mimeType' in value) {
            setClauses.push("mimeType = ?");
            params.push(value.mimeType);
          }

          if ('lastUsedAt' in value) {
            setClauses.push("lastUsedAt = ?");
            params.push(value.lastUsedAt);
          } else {
            setClauses.push("lastUsedAt = ?");
            params.push(now);
          }

          if (setClauses.length === 0) {
            throw new Error(`No valid fields to update for ImageCache`);
          }

          params.push(key);
          const updateStmt = db.prepare(`UPDATE ImageCache SET ${setClauses.join(", ")} WHERE id = ?`);
          const info = updateStmt.run(...params);

          if (info.changes === 0) {
            console.warn(`No rows were updated for key '${key}' in table '${table}'`);
          }
        } else {
          throw new Error(`Invalid image value for ImageCache`);
        }
      } else if (table === "ModFolders") {
        const setClauses: string[] = [];
        const params: any[] = [];

        if ('path' in value) {
          setClauses.push("path = ?");
          params.push(value.path);
        }

        if ('name' in value) {
          setClauses.push("name = ?");
          params.push(value.name);
        }

        if ('parentId' in value) {
          setClauses.push("parentId = ?");
          params.push(value.parentId);
        }

        if ('createdAt' in value) {
          setClauses.push("createdAt = ?");
          params.push(value.createdAt);
        }

        if (setClauses.length === 0) {
          throw new Error(`No valid fields to update for ModFolders`);
        }

        params.push(key);
        const updateStmt = db.prepare(`UPDATE ModFolders SET ${setClauses.join(", ")} WHERE id = ?`);
        const info = updateStmt.run(...params);

        if (info.changes === 0) {
          console.warn(`No rows were updated for key '${key}' in table '${table}'`);
        }
      } else {
        throw new Error(`Unsupported table: ${table}`);
      }
    } catch (err) {
      log.error(err);
      throw err;
    }
  }

  async set<K extends LocalStorageKey>(table: "LocalStorage" | undefined, key: K, value: LocalStorageValue<K>): Promise<void>;
  async set(table: "ImageCache", key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void>;
  async set(table: "ModFolders", key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void>;
  async set(table: TableName = "LocalStorage", key: string, value: any): Promise<void> {
    console.warn("DbHandler.set() is deprecated. Use insert() for new entries or update() for existing entries.");
    const exists = await this.keyExists(table, key);

    if (exists) {
      if (table === "LocalStorage") {
        return this.update("LocalStorage", key as LocalStorageKey, value);
      } else if (table === "ImageCache") {
        return this.update("ImageCache", key, value);
      } else if (table === "ModFolders") {
        return this.update("ModFolders", key, value);
      }
    } else {
      if (table === "LocalStorage") {
        return this.insert("LocalStorage", key as LocalStorageKey, value);
      } else if (table === "ImageCache") {
        return this.insert("ImageCache", key, value);
      } else if (table === "ModFolders") {
        return this.insert("ModFolders", key, value);
      }
    }

    throw new Error(`Unsupported table: ${table}`);
  }

  private async keyExists(table: TableName, key: string): Promise<boolean> {
    try {
      const db = await this.getDb();
      let query: string;

      if (table === "LocalStorage") {
        query = "SELECT 1 FROM LocalStorage WHERE key = ?";
      } else if (table === "ImageCache") {
        query = "SELECT 1 FROM ImageCache WHERE id = ?";
      } else if (table === "ModFolders") {
        query = "SELECT 1 FROM ModFolders WHERE id = ?";
      } else {
        throw new Error(`Unsupported table: ${table}`);
      }

      const stmt = db.prepare(query);
      const row = stmt.get(key);
      return !!row;
    } catch (err) {
      log.error(err);
      throw err;
    }
  }

  async query<T>(sql: string, params: any[] = []): Promise<T[]> {
    try {
      const db = await this.getDb();
      const stmt = db.prepare(sql);
      return stmt.all(...params) as T[];
    } catch (err) {
      log.error(err);
      throw err;
    }
  }

  async exec(sql: string, params: any[] = []): Promise<number> {
    try {
      const db = await this.getDb();
      const stmt = db.prepare(sql);
      const info = stmt.run(...params);
      return info.changes;
    } catch (err) {
      log.error(err);
      throw err;
    }
  }

  async del(table: Exclude<TableName, "LocalStorage">, key: string): Promise<boolean> {
    // @ts-ignore
    if (table === "LocalStorage") {
      throw new Error("Delete operation is not supported for LocalStorage table");
    }

    try {
      const db = await this.getDb();
      let query: string;

      if (table === "ImageCache") {
        query = "DELETE FROM ImageCache WHERE id = ?";
      } else if (table === "ModFolders") {
        query = "DELETE FROM ModFolders WHERE id = ?";
      } else {
        throw new Error(`Unsupported table for delete operation: ${table}`);
      }

      const stmt = db.prepare(query);
      const info = stmt.run(key);
      const deleted = info.changes > 0;

      if (!deleted) {
        console.error(`No item with key ${key} was found in ${table}`);
      }

      return deleted;
    } catch (err) {
      console.error(`Error deleting item with key ${key} from ${table}:`, err);
      log.error(err);
      throw err;
    }
  }
}

const db = new DbHandler();

export { db };