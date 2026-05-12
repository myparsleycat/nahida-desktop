export type SettingRow = {
    key: string;
    value: string | null;
};

export type AppStateRow = {
    key: string;
    value: string;
    updatedAt: string;
};

export type GamePathRow = {
    game: string;
    modFolderPath: string;
    importer: string | null;
    order: number;
};

export type ModPresetRow = {
    id: string;
    game: string;
    name: string;
    description: string | null;
    itemCount: number;
    createdAt: string;
    updatedAt: string;
    version: number;
};

export type ModPresetItemRow = {
    presetId: string;
    modKey: string;
    relativePath: string;
    groupRelativePath: string;
    folderName: string;
    isEnabled: boolean;
    itemOrder: number;
};

export type ImageCacheRow = {
    hash: string;
    image: Buffer;
    size: number;
};

export type ScriptType = "python" | "exec";

export type ScriptRow = {
    id: string;
    name: string;
    source: Buffer;
    isSrcZstd: boolean;
    type: ScriptType;
    size: number;
    zstdSize: number | null;
    sha256: string;
    zstdSha256: string | null;
};

export type ScriptPresetRow = {
    id: string;
    name: string;
};

export type ScriptPresetItemRow = {
    presetId: string;
    scriptId: string;
    order: number;
};

export type ToggleViewerArtifactRow = {
    id: string;
    targetIniPath: string;
    toggleTxtPath: string;
    toggleIniPath: string;
    toggleTxtHash: string;
    toggleIniHash: string;
    updatedAt: string;
};

export type SchemaStateRow = {
    key: string;
    value: string;
    updatedAt: string;
};

export type TableColumnSpec = {
    name: string;
    type: "TEXT" | "INTEGER" | "BLOB";
    notNull?: boolean;
    primaryKey?: boolean;
    defaultSql?: string;
    aliases?: string[];
    boolean?: boolean;
};

export type TableIndexSpec = {
    name: string;
    columns: string[];
    unique?: boolean;
};

export type TableForeignKeySpec = {
    columns: string[];
    refTable: string;
    refColumns: string[];
    onDelete?: "cascade" | "no action";
    onUpdate?: "cascade" | "no action";
};

export type TableSpec = {
    name: string;
    aliases?: string[];
    columns: TableColumnSpec[];
    compositePrimaryKey?: string[];
    indexes?: TableIndexSpec[];
    foreignKeys?: TableForeignKeySpec[];
};

export const APP_SCHEMA_VERSION = 1;

export const TABLE_SPECS: TableSpec[] = [
    {
        name: "setting",
        columns: [
            { name: "key", type: "TEXT", primaryKey: true, notNull: true },
            { name: "value", type: "TEXT" },
        ],
    },
    {
        name: "app_state",
        columns: [
            { name: "key", type: "TEXT", primaryKey: true, notNull: true },
            { name: "value", type: "TEXT", notNull: true },
            { name: "updated_at", type: "TEXT", notNull: true },
        ],
    },
    {
        name: "game_paths",
        columns: [
            { name: "game", type: "TEXT", primaryKey: true, notNull: true },
            { name: "modFolderPath", type: "TEXT", notNull: true },
            { name: "importer", type: "TEXT" },
            { name: "order", type: "INTEGER", notNull: true, defaultSql: "0" },
        ],
    },
    {
        name: "mod_presets",
        columns: [
            { name: "id", type: "TEXT", primaryKey: true, notNull: true },
            { name: "game", type: "TEXT", notNull: true },
            { name: "name", type: "TEXT", notNull: true },
            { name: "description", type: "TEXT" },
            { name: "item_count", type: "INTEGER", notNull: true, defaultSql: "0" },
            { name: "created_at", type: "TEXT", notNull: true, defaultSql: "''" },
            { name: "updated_at", type: "TEXT", notNull: true, defaultSql: "''" },
            { name: "version", type: "INTEGER", notNull: true, defaultSql: "1" },
        ],
        indexes: [{ name: "mod_presets_game_name_idx", columns: ["game", "name"], unique: true }],
        foreignKeys: [
            {
                columns: ["game"],
                refTable: "game_paths",
                refColumns: ["game"],
                onDelete: "cascade",
                onUpdate: "no action",
            },
        ],
    },
    {
        name: "mod_preset_items",
        columns: [
            { name: "preset_id", type: "TEXT", notNull: true },
            { name: "mod_key", type: "TEXT", notNull: true },
            { name: "relative_path", type: "TEXT", notNull: true },
            { name: "group_relative_path", type: "TEXT", notNull: true },
            { name: "folder_name", type: "TEXT", notNull: true },
            { name: "is_enabled", type: "INTEGER", notNull: true, boolean: true },
            { name: "item_order", type: "INTEGER", notNull: true },
        ],
        compositePrimaryKey: ["preset_id", "mod_key"],
        foreignKeys: [
            {
                columns: ["preset_id"],
                refTable: "mod_presets",
                refColumns: ["id"],
                onDelete: "cascade",
                onUpdate: "no action",
            },
        ],
    },
    {
        name: "image_cache",
        columns: [
            { name: "hash", type: "TEXT", primaryKey: true, notNull: true },
            { name: "image", type: "BLOB", notNull: true },
            { name: "size", type: "INTEGER", notNull: true, defaultSql: "0" },
        ],
    },
    {
        name: "script",
        aliases: ["fix_tool"],
        columns: [
            { name: "id", type: "TEXT", primaryKey: true, notNull: true },
            { name: "name", type: "TEXT", notNull: true },
            { name: "source", type: "BLOB", notNull: true },
            {
                name: "is_src_zstd",
                type: "INTEGER",
                notNull: true,
                boolean: true,
                defaultSql: "0",
            },
            { name: "type", type: "TEXT", notNull: true },
            { name: "size", type: "INTEGER", notNull: true },
            { name: "zstd_size", type: "INTEGER", defaultSql: "NULL" },
            { name: "sha256", type: "TEXT", notNull: true, defaultSql: "''" },
            { name: "zstd_sha256", type: "TEXT", defaultSql: "NULL" },
        ],
        indexes: [{ name: "script_name_unique", columns: ["name"], unique: true }],
    },
    {
        name: "script_preset",
        aliases: ["fix_tool_preset"],
        columns: [
            { name: "id", type: "TEXT", primaryKey: true, notNull: true },
            { name: "name", type: "TEXT", notNull: true },
        ],
        indexes: [{ name: "script_preset_name_unique", columns: ["name"], unique: true }],
    },
    {
        name: "script_preset_item",
        aliases: ["fix_tool_preset_item"],
        columns: [
            { name: "preset_id", type: "TEXT", notNull: true },
            { name: "script_id", type: "TEXT", notNull: true, aliases: ["tool_id"] },
            { name: "order", type: "INTEGER", notNull: true },
        ],
        compositePrimaryKey: ["preset_id", "script_id"],
        foreignKeys: [
            {
                columns: ["preset_id"],
                refTable: "script_preset",
                refColumns: ["id"],
                onDelete: "cascade",
                onUpdate: "no action",
            },
            {
                columns: ["script_id"],
                refTable: "script",
                refColumns: ["id"],
                onDelete: "cascade",
                onUpdate: "no action",
            },
        ],
    },
    {
        name: "toggle_viewer_artifact",
        columns: [
            { name: "id", type: "TEXT", primaryKey: true, notNull: true },
            { name: "target_ini_path", type: "TEXT", notNull: true },
            { name: "toggle_txt_path", type: "TEXT", notNull: true },
            { name: "toggle_ini_path", type: "TEXT", notNull: true },
            { name: "toggle_txt_hash", type: "TEXT", notNull: true },
            { name: "toggle_ini_hash", type: "TEXT", notNull: true },
            { name: "updated_at", type: "TEXT", notNull: true },
        ],
        indexes: [
            {
                name: "toggle_viewer_artifact_target_ini_path_unique",
                columns: ["target_ini_path"],
                unique: true,
            },
        ],
    },
    {
        name: "_schema_state",
        columns: [
            { name: "key", type: "TEXT", primaryKey: true, notNull: true },
            { name: "value", type: "TEXT", notNull: true },
            { name: "updated_at", type: "TEXT", notNull: true },
        ],
    },
];
