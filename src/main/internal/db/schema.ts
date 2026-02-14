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

export const fixTool = sqliteTable("fix_tool", {
    id: text("id").primaryKey(),
    name: text("name").notNull().unique(),
    source: text("source").notNull(),
    type: text("type", { enum: ["python", "batch"] }).notNull(),
    size: integer("size").notNull(),
    sha256: text("sha256").notNull(),
});

export const fixToolRelations = relations(fixTool, ({ many }) => ({
    presets: many(fixToolPresetItem),
}));

export const fixToolPreset = sqliteTable("fix_tool_preset", {
    id: text("id").primaryKey(),
    name: text("name").notNull().unique(),
});

export const fixToolPresetRelations = relations(fixToolPreset, ({ many }) => ({
    tools: many(fixToolPresetItem),
}));

export const fixToolPresetItem = sqliteTable(
    "fix_tool_preset_item",
    {
        presetId: text("preset_id")
            .notNull()
            .references(() => fixToolPreset.id, { onDelete: "cascade" }),
        toolId: text("tool_id")
            .notNull()
            .references(() => fixTool.id, { onDelete: "cascade" }),
        order: integer("order").notNull(),
    },
    (t) => [primaryKey({ columns: [t.presetId, t.toolId] })],
);

export const fixToolPresetItemRelations = relations(fixToolPresetItem, ({ one }) => ({
    preset: one(fixToolPreset, {
        fields: [fixToolPresetItem.presetId],
        references: [fixToolPreset.id],
    }),
    tool: one(fixTool, {
        fields: [fixToolPresetItem.toolId],
        references: [fixTool.id],
    }),
}));
