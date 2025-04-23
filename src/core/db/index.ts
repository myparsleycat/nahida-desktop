import { fileTypeFromBuffer } from "file-type";
import { Database } from "sqlite3";

interface StorageKeyValues {
  sess: string | null;
  language: string;
  img_cache_on: boolean;
  mods_resizable_default: number;
}

interface ImageCacheItem {
  id: string;
  image: Buffer;
  size: number;
  mimeType: string;
  createdAt: string;
  lastUsedAt: string;
}

interface ModFolders {
  id: string;          // id를 PK로 사용
  path: string;        // path는 unique 필드로 변경
  name: string;        // name도 unique 필드로 변경
  parentId: string | null; // 부모 폴더 ID 추가
  createdAt: string;   // NOT NULL
}

type TableName = "LocalStorage" | "ImageCache" | "ModFolders";

type LocalStorageKey = keyof StorageKeyValues;
type ImageCacheKey = string;
type ModFoldersKey = string;  // id를 키로 사용

type LocalStorageValue<K extends LocalStorageKey> = StorageKeyValues[K];
type ImageCacheValue = Omit<ImageCacheItem, "id">;
type ModFoldersValue = Omit<ModFolders, "id">;  // id 대신 나머지 필드들

interface TableSchema {
  name: string;
  createStatement: string;
}

const defaultValues: StorageKeyValues = {
  sess: null,
  language: "en",
  img_cache_on: true,
  mods_resizable_default: 25
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
  private db: Database | null = null;

  async init(): Promise<Database> {
    return new Promise<Database>(async (resolve, reject) => {
      if (this.db) {
        console.log("DB already initialized");
        resolve(this.db);
        return;
      }

      try {
        this.db = await this.openDatabase('./database.db');
        console.log("Connected to the SQLite database");

        await this.createTables(tableSchemas);
        await this.initializeDefaultValues();

        resolve(this.db);
      } catch (err) {
        console.error("Error during database initialization:", err);
        reject(err);
      }
    });
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

  private openDatabase(path: string): Promise<Database> {
    return new Promise<Database>((resolve, reject) => {
      const database = new Database(path, (err) => {
        if (err) {
          console.error("Error opening database:", err.message);
          reject(err);
          return;
        }
        resolve(database);
      });
    });
  }

  private async createTables(schemas: TableSchema[]): Promise<void> {
    for (const schema of schemas) {
      await this.createTable(schema);
      console.log(`${schema.name} table ready`);
    }
  }

  private createTable(schema: TableSchema): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      this.db!.run(schema.createStatement, (err) => {
        if (err) {
          console.error(`Error creating ${schema.name} table:`, err.message);
          reject(err);
          return;
        }
        resolve();
      });
    });
  }

  private async initializeDefaultValues(): Promise<void> {
    const keys = Object.keys(defaultValues) as (keyof StorageKeyValues)[];

    for (const key of keys) {
      const existingRow = await this.checkIfKeyExists(key);

      if (!existingRow) {
        const value = defaultValues[key];
        await this.insert("LocalStorage", key, value);
        console.log(`Initialized key '${key}' with default value:`, value);
      }
    }
  }

  private checkIfKeyExists<K extends LocalStorageKey>(key: K): Promise<boolean> {
    return new Promise((resolve, reject) => {
      this.db!.get(
        "SELECT * FROM LocalStorage WHERE key = ?",
        [key],
        (err, row) => {
          if (err) {
            reject(err);
            return;
          }
          resolve(!!row);
        }
      );
    });
  }

  async getDb(): Promise<Database> {
    if (!this.db) {
      return await this.init();
    }
    return this.db;
  }

  async get<K extends LocalStorageKey>(table: "LocalStorage", key: K): Promise<LocalStorageValue<K> | null>;
  async get(table: "ImageCache", key: ImageCacheKey): Promise<ImageCacheItem | null>;
  async get(table: "ModFolders", key: ModFoldersKey): Promise<ModFolders | null>;
  async get<T = any>(table: TableName = "LocalStorage", key: string): Promise<T | null> {
    return new Promise((resolve, reject) => {
      this.getDb().then(db => {
        let query: string;
        let params: any[];

        if (table === "LocalStorage") {
          query = "SELECT value FROM LocalStorage WHERE key = ?";
          params = [key];
        } else if (table === "ImageCache") {
          query = "SELECT id, image, size, mimeType, createdAt, lastUsedAt FROM ImageCache WHERE id = ?";
          params = [key];
        } else if (table === "ModFolders") {
          query = "SELECT id, path, name, parentId, createdAt FROM ModFolders WHERE id = ?";
          params = [key];
        } else {
          reject(new Error(`Unsupported table: ${table}`));
          return;
        }

        db.get(query, params, (err, row: any) => {
          if (err) {
            reject(err);
            return;
          }

          if (!row) {
            if (table === "LocalStorage" && key in defaultValues) {
              resolve((defaultValues as any)[key] as T);
            } else {
              resolve(null);
            }
            return;
          }

          if (table === "LocalStorage") {
            if (row.value === null) {
              resolve(null);
              return;
            }

            try {
              const value = JSON.parse(row.value);
              resolve(value as T);
            } catch (e) {
              resolve(row.value as T);
            }
          } else {
            // ImageCache나 ModFolders 테이블의 경우 row 객체 자체를 반환
            resolve(row as T);
          }
        });
      }).catch(reject);
    });
  }

  async insert<K extends LocalStorageKey>(table: "LocalStorage" | undefined, key: K, value: LocalStorageValue<K>): Promise<void>;
  async insert(table: "ImageCache", key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void>;
  async insert(table: "ModFolders", key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void>;
  async insert(table: TableName = "LocalStorage", key: string, value: any): Promise<void> {
    return new Promise((resolve, reject) => {
      this.getDb().then(async (db) => {
        let query: string;
        let params: any[];

        // 먼저 해당 키가 존재하는지 확인
        const exists = await this.keyExists(table, key);
        if (exists) {
          reject(new Error(`Key '${key}' already exists in table '${table}'. Use update method instead.`));
          return;
        }

        if (table === "LocalStorage") {
          if (value === null) {
            query = "INSERT INTO LocalStorage (key, value) VALUES (?, NULL)";
            params = [key];
          } else {
            const stringValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
            query = "INSERT INTO LocalStorage (key, value) VALUES (?, ?)";
            params = [key, stringValue];
          }
        } else if (table === "ImageCache") {
          const now = new Date().toISOString();

          if (Buffer.isBuffer(value)) {
            const filetype = await fileTypeFromBuffer(value);
            query = "INSERT INTO ImageCache (id, image, size, mimeType, createdAt, lastUsedAt) VALUES (?, ?, ?, ?, ?, ?)";
            params = [key, value, value.byteLength, filetype?.mime || 'application/octet-stream', now, now];
          } else if (typeof value === 'object' && 'image' in value && Buffer.isBuffer(value.image)) {
            query = "INSERT INTO ImageCache (id, image, size, mimeType, createdAt, lastUsedAt) VALUES (?, ?, ?, ?, ?, ?)";
            params = [
              key,
              value.image,
              value.size,
              value.mimeType || 'application/octet-stream',
              value.createdAt || now,
              value.lastUsedAt || now
            ];
          } else {
            reject(new Error(`Invalid image value for ImageCache`));
            return;
          }
        } else if (table === "ModFolders") {
          const now = new Date().toISOString();
          query = "INSERT INTO ModFolders (id, path, name, parentId, createdAt) VALUES (?, ?, ?, ?, ?)";
          params = [
            key,
            value.path || '',
            value.name || '',
            value.parentId || null,
            value.createdAt || now
          ];
        } else {
          reject(new Error(`Unsupported table: ${table}`));
          return;
        }

        db.run(query, params, function (err) {
          if (err) {
            reject(err);
            return;
          }
          resolve();
        });
      }).catch(reject);
    });
  }

  async update<K extends LocalStorageKey>(table: "LocalStorage" | undefined, key: K, value: LocalStorageValue<K>): Promise<void>;
  async update(table: "ImageCache", key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void>;
  async update(table: "ModFolders", key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void>;
  async update(table: TableName = "LocalStorage", key: string, value: any): Promise<void> {
    return new Promise((resolve, reject) => {
      this.getDb().then(async (db) => {
        // 먼저 해당 키가 존재하는지 확인
        const exists = await this.keyExists(table, key);
        if (!exists) {
          reject(new Error(`Key '${key}' does not exist in table '${table}'. Use insert method instead.`));
          return;
        }

        let query: string;
        let params: any[];

        if (table === "LocalStorage") {
          if (value === null) {
            query = "UPDATE LocalStorage SET value = NULL WHERE key = ?";
            params = [key];
          } else {
            const stringValue = typeof value === 'object' ? JSON.stringify(value) : String(value);
            query = "UPDATE LocalStorage SET value = ? WHERE key = ?";
            params = [stringValue, key];
          }
        } else if (table === "ImageCache") {
          const now = new Date().toISOString();

          if (Buffer.isBuffer(value)) {
            const filetype = await fileTypeFromBuffer(value);
            query = "UPDATE ImageCache SET image = ?, size = ?, mimeType = ?, lastUsedAt = ? WHERE id = ?";
            params = [value, value.byteLength, filetype?.mime || 'application/octet-stream', now, key];
          } else if (typeof value === 'object') {
            const setClauses: string[] = [];
            params = [];

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
              reject(new Error(`No valid fields to update for ImageCache`));
              return;
            }

            // 마지막에 WHERE 조건의 매개변수 추가
            params.push(key);
            query = `UPDATE ImageCache SET ${setClauses.join(", ")} WHERE id = ?`;
          } else {
            reject(new Error(`Invalid image value for ImageCache`));
            return;
          }
        } else if (table === "ModFolders") {
          const setClauses: string[] = [];
          params = [];

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
            reject(new Error(`No valid fields to update for ModFolders`));
            return;
          }

          // 마지막에 WHERE 조건의 매개변수 추가
          params.push(key);
          query = `UPDATE ModFolders SET ${setClauses.join(", ")} WHERE id = ?`;
        } else {
          reject(new Error(`Unsupported table: ${table}`));
          return;
        }

        db.run(query, params, function (err) {
          if (err) {
            reject(err);
            return;
          }

          if (this.changes === 0) {
            console.warn(`No rows were updated for key '${key}' in table '${table}'`);
          }

          resolve();
        });
      }).catch(reject);
    });
  }

  // 기존 set 메서드도 유지하되 update로 리다이렉션 (하위 호환성을 위해)
  async set<K extends LocalStorageKey>(table: "LocalStorage" | undefined, key: K, value: LocalStorageValue<K>): Promise<void>;
  async set(table: "ImageCache", key: ImageCacheKey, value: Buffer | Partial<ImageCacheValue>): Promise<void>;
  async set(table: "ModFolders", key: ModFoldersKey, value: Partial<ModFoldersValue>): Promise<void>;
  async set(table: TableName = "LocalStorage", key: string, value: any): Promise<void> {
    console.warn("DbHandler.set() is deprecated. Use insert() for new entries or update() for existing entries.");

    // 키가 존재하는지 확인
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

  // 키가 존재하는지 확인하는 헬퍼 메서드
  private async keyExists(table: TableName, key: string): Promise<boolean> {
    return new Promise<boolean>((resolve, reject) => {
      this.getDb().then(db => {
        let query: string;

        if (table === "LocalStorage") {
          query = "SELECT 1 FROM LocalStorage WHERE key = ?";
        } else if (table === "ImageCache") {
          query = "SELECT 1 FROM ImageCache WHERE id = ?";
        } else if (table === "ModFolders") {
          query = "SELECT 1 FROM ModFolders WHERE id = ?";  // path 대신 id로 변경
        } else {
          reject(new Error(`Unsupported table: ${table}`));
          return;
        }

        db.get(query, [key], (err, row) => {
          if (err) {
            reject(err);
            return;
          }
          resolve(!!row);
        });
      }).catch(reject);
    });
  }

  async query<T>(sql: string, params: any[] = []): Promise<T[]> {
    return new Promise<T[]>((resolve, reject) => {
      this.getDb().then(db => {
        db.all(sql, params, (err, rows) => {
          if (err) {
            reject(err);
            return;
          }
          resolve(rows as T[]);
        });
      }).catch(reject);
    });
  }

  async exec(sql: string, params: any[] = []): Promise<number> {
    return new Promise<number>((resolve, reject) => {
      this.getDb().then(db => {
        db.run(sql, params, function (err) {
          if (err) {
            reject(err);
            return;
          }
          resolve(this.changes);
        });
      }).catch(reject);
    });
  }

  async del(table: Exclude<TableName, "LocalStorage">, key: string): Promise<boolean> {
    // @ts-ignore
    if (table === "LocalStorage") {
      throw new Error("Delete operation is not supported for LocalStorage table");
    }

    return new Promise<boolean>((resolve, reject) => {
      this.getDb().then(db => {
        let query: string;
        let params: any[];

        if (table === "ImageCache") {
          query = "DELETE FROM ImageCache WHERE id = ?";
          params = [key];
        } else if (table === "ModFolders") {
          query = "DELETE FROM ModFolders WHERE id = ?";  // path 대신 id로 변경
          params = [key];
        } else {
          reject(new Error(`Unsupported table for delete operation: ${table}`));
          return;
        }

        db.run(query, params, function (err) {
          if (err) {
            console.error(`Error deleting item with key ${key} from ${table}:`, err.message);
            reject(err);
            return;
          }
          const deleted = this.changes > 0;
          resolve(deleted);
        });
      }).catch(reject);
    });
  }
}

const db = new DbHandler();

export { db };