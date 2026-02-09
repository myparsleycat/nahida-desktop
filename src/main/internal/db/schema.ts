import { blob, integer, sqliteTable, text } from "drizzle-orm/sqlite-core";

export const setting = sqliteTable("setting", {
    key: text().primaryKey(),
    value: text(),
});

export const gamePaths = sqliteTable("game_paths", {
    game: text().primaryKey(),
    modFolderPath: text().notNull(),
});

export const modPresets = sqliteTable("mod_presets", {
    id: text().primaryKey(),
    game: text()
        .notNull()
        .references(() => gamePaths.game),
    name: text().notNull().unique(),
    mods: text().notNull(),
});

export const imageCache = sqliteTable("image_cache", {
    hash: text().primaryKey(),
    image: blob({ mode: "buffer" }).notNull(),
    size: integer().notNull(),
});
