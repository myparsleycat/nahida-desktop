import { relations } from "drizzle-orm";
import { blob, integer, primaryKey, sqliteTable, text } from "drizzle-orm/sqlite-core";

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

export const script = sqliteTable("script", {
    id: text("id").primaryKey(),
    name: text("name").notNull().unique(),
    source: blob("source", { mode: "buffer" }).notNull(),
    type: text("type", { enum: ["python", "exec"] }).notNull(),
    size: integer("size").notNull(),
    sha256: text("sha256").notNull(),
});

export const scriptRelations = relations(script, ({ many }) => ({
    presets: many(scriptPresetItem),
}));

export const scriptPreset = sqliteTable("script_preset", {
    id: text("id").primaryKey(),
    name: text("name").notNull().unique(),
});

export const scriptPresetRelations = relations(scriptPreset, ({ many }) => ({
    scripts: many(scriptPresetItem),
}));

export const scriptPresetItem = sqliteTable(
    "script_preset_item",
    {
        presetId: text("preset_id")
            .notNull()
            .references(() => scriptPreset.id, { onDelete: "cascade" }),
        scriptId: text("script_id")
            .notNull()
            .references(() => script.id, { onDelete: "cascade" }),
        order: integer("order").notNull(),
    },
    (t) => [primaryKey({ columns: [t.presetId, t.scriptId] })],
);

export const scriptPresetItemRelations = relations(scriptPresetItem, ({ one }) => ({
    preset: one(scriptPreset, {
        fields: [scriptPresetItem.presetId],
        references: [scriptPreset.id],
    }),
    script: one(script, {
        fields: [scriptPresetItem.scriptId],
        references: [script.id],
    }),
}));
