import { TableSchema } from "./index";

export const tableSchemas: TableSchema[] = [
    {
        name: "LocalStorage",
        version: 1,
        createStatement: `
      CREATE TABLE IF NOT EXISTS LocalStorage (
        key TEXT PRIMARY KEY,
        value TEXT
      )
    `,
        columns: [
            { name: "key", type: "TEXT", constraints: "PRIMARY KEY" },
            { name: "value", type: "TEXT" }
        ]
    },
    {
        name: "ImageCache",
        version: 1,
        createStatement: `
      CREATE TABLE IF NOT EXISTS ImageCache (
        id TEXT PRIMARY KEY,
        image BLOB,
        size INTEGER,
        mimeType TEXT,
        createdAt TEXT,
        lastUsedAt TEXT
      )
    `,
        columns: [
            { name: "id", type: "TEXT", constraints: "PRIMARY KEY" },
            { name: "image", type: "BLOB" },
            { name: "size", type: "INTEGER" },
            { name: "mimeType", type: "TEXT" },
            { name: "createdAt", type: "TEXT" },
            { name: "lastUsedAt", type: "TEXT" }
        ]
    },
    {
        name: "ModFolders",
        version: 1,
        createStatement: `
      CREATE TABLE IF NOT EXISTS ModFolders (
        id TEXT PRIMARY KEY,
        path TEXT UNIQUE,
        name TEXT UNIQUE,
        parentId TEXT NULL,
        createdAt TEXT NOT NULL,
        seq INTEGER NOT NULL DEFAULT 1,
        FOREIGN KEY (parentId) REFERENCES ModFolders(id) 
          ON DELETE SET NULL 
          ON UPDATE CASCADE
      )
    `,
        columns: [
            { name: "id", type: "TEXT", constraints: "PRIMARY KEY" },
            { name: "path", type: "TEXT", constraints: "UNIQUE" },
            { name: "name", type: "TEXT", constraints: "UNIQUE" },
            { name: "parentId", type: "TEXT" },
            { name: "createdAt", type: "TEXT", constraints: "NOT NULL" },
            { name: "seq", type: "INTEGER", constraints: "NOT NULL DEFAULT 1" }
        ]
    }
];