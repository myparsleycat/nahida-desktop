import { relations, sql } from "drizzle-orm";
import { blob, integer, primaryKey, sqliteTable, text, uniqueIndex } from "drizzle-orm/sqlite-core";

export const setting = sqliteTable("setting", {
    key: text().primaryKey(),
    value: text(),
});

export const gamePaths = sqliteTable("game_paths", {
    game: text().primaryKey(),
    modFolderPath: text().notNull(),
    importer: text(),
});

export const modPresets = sqliteTable(
    "mod_presets",
    {
        id: text().primaryKey(),
        game: text()
            .notNull()
            .references(() => gamePaths.game, { onDelete: "cascade" }),
        name: text().notNull(),
        description: text(),
        itemCount: integer("item_count").notNull().default(0),
        createdAt: text("created_at").notNull(),
        updatedAt: text("updated_at").notNull(),
        version: integer().notNull().default(1),
    },
    (t) => [uniqueIndex("mod_presets_game_name_idx").on(t.game, t.name)],
);

export const modPresetItems = sqliteTable(
    "mod_preset_items",
    {
        presetId: text("preset_id")
            .notNull()
            .references(() => modPresets.id, { onDelete: "cascade" }),
        modKey: text("mod_key").notNull(),
        relativePath: text("relative_path").notNull(),
        groupRelativePath: text("group_relative_path").notNull(),
        folderName: text("folder_name").notNull(),
        isEnabled: integer("is_enabled", { mode: "boolean" }).notNull(),
        itemOrder: integer("item_order").notNull(),
    },
    (t) => [primaryKey({ columns: [t.presetId, t.modKey] })],
);

export const imageCache = sqliteTable("image_cache", {
    hash: text().primaryKey(),
    image: blob({ mode: "buffer" }).notNull(),
    size: integer().notNull(),
});

export const script = sqliteTable("script", {
    id: text("id").primaryKey(),
    name: text("name").notNull().unique(),
    source: blob("source", { mode: "buffer" }).notNull(),
    isSrcZstd: integer("is_src_zstd", { mode: "boolean" }).notNull().default(false),
    type: text("type", { enum: ["python", "exec"] }).notNull(),
    size: integer("size").notNull(),
    zstdSize: integer("zstd_size").default(sql`NULL`),
    sha256: text("sha256").notNull(),
    zstdSha256: text("zstd_sha256").default(sql`NULL`),
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

export const toggleViewerArtifact = sqliteTable("toggle_viewer_artifact", {
    id: text("id").primaryKey(),
    targetIniPath: text("target_ini_path").notNull().unique(),
    toggleTxtPath: text("toggle_txt_path").notNull(),
    toggleIniPath: text("toggle_ini_path").notNull(),
    toggleTxtHash: text("toggle_txt_hash").notNull(),
    toggleIniHash: text("toggle_ini_hash").notNull(),
    updatedAt: text("updated_at").notNull(),
});
