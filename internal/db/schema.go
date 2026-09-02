package db

const AppSchemaVersion = 4

const (
	SchemaKeyAppVersion                  = "app_schema_version"
	SchemaKeyGamePathsNTELauncher        = "game_paths_nte_launcher_path"
	SchemaKeyToggleViewerArtifactDropped = "toggle_viewer_artifact_dropped"
	NTEImporter                          = "NTE"
	NTEGameExeKeepSuffix                 = "%htgame.exe"
)

type ColumnType string

const (
	TypeText    ColumnType = "TEXT"
	TypeInteger ColumnType = "INTEGER"
	TypeBlob    ColumnType = "BLOB"
)

type ForeignKeyAction string

const (
	FKCascade  ForeignKeyAction = "cascade"
	FKNoAction ForeignKeyAction = "no action"
)

type ColumnSpec struct {
	Name       string
	Type       ColumnType
	NotNull    bool
	PrimaryKey bool
	DefaultSQL *string
	Aliases    []string
	Boolean    bool
}

type IndexSpec struct {
	Name    string
	Columns []string
	Unique  bool
}

type ForeignKeySpec struct {
	Columns    []string
	RefTable   string
	RefColumns []string
	OnDelete   ForeignKeyAction
	OnUpdate   ForeignKeyAction
}

type TableSpec struct {
	Name                string
	Aliases             []string
	Columns             []ColumnSpec
	CompositePrimaryKey []string
	Indexes             []IndexSpec
	ForeignKeys         []ForeignKeySpec
}

func sqlDefault(value string) *string {
	return &value
}

var TableSpecs = []TableSpec{
	{
		Name: "setting",
		Columns: []ColumnSpec{
			{Name: "key", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "value", Type: TypeText},
		},
	},
	{
		Name: "app_state",
		Columns: []ColumnSpec{
			{Name: "key", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "value", Type: TypeText, NotNull: true},
			{Name: "updated_at", Type: TypeText, NotNull: true},
		},
	},
	{
		Name: "game_paths",
		Columns: []ColumnSpec{
			{Name: "game", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "modFolderPath", Type: TypeText, NotNull: true},
			{Name: "importer", Type: TypeText},
			{Name: "linkedModFolderPath", Type: TypeText},
			{Name: "gameInstallPath", Type: TypeText},
			{Name: "gameExecutablePath", Type: TypeText},
			{Name: "nteLauncherPath", Type: TypeText},
			{Name: "order", Type: TypeInteger, NotNull: true, DefaultSQL: sqlDefault("0")},
		},
	},
	{
		Name: "mod_presets",
		Columns: []ColumnSpec{
			{Name: "id", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "game", Type: TypeText, NotNull: true},
			{Name: "name", Type: TypeText, NotNull: true},
			{Name: "description", Type: TypeText},
			{Name: "item_count", Type: TypeInteger, NotNull: true, DefaultSQL: sqlDefault("0")},
			{Name: "created_at", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "updated_at", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "version", Type: TypeInteger, NotNull: true, DefaultSQL: sqlDefault("1")},
		},
		Indexes: []IndexSpec{
			{Name: "mod_presets_game_name_idx", Columns: []string{"game", "name"}, Unique: true},
		},
		ForeignKeys: []ForeignKeySpec{
			{
				Columns:    []string{"game"},
				RefTable:   "game_paths",
				RefColumns: []string{"game"},
				OnDelete:   FKCascade,
				OnUpdate:   FKNoAction,
			},
		},
	},
	{
		Name: "mod_preset_items",
		Columns: []ColumnSpec{
			{Name: "preset_id", Type: TypeText, NotNull: true},
			{Name: "mod_key", Type: TypeText, NotNull: true},
			{Name: "relative_path", Type: TypeText, NotNull: true},
			{Name: "group_relative_path", Type: TypeText, NotNull: true},
			{Name: "folder_name", Type: TypeText, NotNull: true},
			{Name: "is_enabled", Type: TypeInteger, NotNull: true, Boolean: true},
			{Name: "item_order", Type: TypeInteger, NotNull: true},
		},
		CompositePrimaryKey: []string{"preset_id", "mod_key"},
		ForeignKeys: []ForeignKeySpec{
			{
				Columns:    []string{"preset_id"},
				RefTable:   "mod_presets",
				RefColumns: []string{"id"},
				OnDelete:   FKCascade,
				OnUpdate:   FKNoAction,
			},
		},
	},
	{
		Name: "image_cache",
		Columns: []ColumnSpec{
			{Name: "hash", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "image", Type: TypeBlob, NotNull: true},
			{Name: "size", Type: TypeInteger, NotNull: true, DefaultSQL: sqlDefault("0")},
		},
	},
	{
		Name: "touch_profile_vision_cache",
		Columns: []ColumnSpec{
			{Name: "cache_key", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "result", Type: TypeText, NotNull: true},
			{Name: "updated_at", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
		},
	},
	{
		Name: "mod_scan_cache",
		Columns: []ColumnSpec{
			{Name: "path", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "mtime", Type: TypeInteger, NotNull: true, DefaultSQL: sqlDefault("0")},
			{Name: "payload", Type: TypeText, NotNull: true},
			{Name: "updated_at", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
		},
	},
	{
		Name:    "script",
		Aliases: []string{"fix_tool"},
		Columns: []ColumnSpec{
			{Name: "id", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "name", Type: TypeText, NotNull: true},
			{Name: "source", Type: TypeBlob, NotNull: true},
			{Name: "is_src_zstd", Type: TypeInteger, NotNull: true, Boolean: true, DefaultSQL: sqlDefault("0")},
			{Name: "type", Type: TypeText, NotNull: true},
			{Name: "size", Type: TypeInteger, NotNull: true},
			{Name: "zstd_size", Type: TypeInteger, DefaultSQL: sqlDefault("NULL")},
			{Name: "sha256", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "zstd_sha256", Type: TypeText, DefaultSQL: sqlDefault("NULL")},
		},
		Indexes: []IndexSpec{
			{Name: "script_name_unique", Columns: []string{"name"}, Unique: true},
		},
	},
	{
		Name:    "script_preset",
		Aliases: []string{"fix_tool_preset"},
		Columns: []ColumnSpec{
			{Name: "id", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "name", Type: TypeText, NotNull: true},
		},
		Indexes: []IndexSpec{
			{Name: "script_preset_name_unique", Columns: []string{"name"}, Unique: true},
		},
	},
	{
		Name:    "script_preset_item",
		Aliases: []string{"fix_tool_preset_item"},
		Columns: []ColumnSpec{
			{Name: "preset_id", Type: TypeText, NotNull: true},
			{Name: "script_id", Type: TypeText, NotNull: true, Aliases: []string{"tool_id"}},
			{Name: "order", Type: TypeInteger, NotNull: true},
		},
		CompositePrimaryKey: []string{"preset_id", "script_id"},
		ForeignKeys: []ForeignKeySpec{
			{
				Columns:    []string{"preset_id"},
				RefTable:   "script_preset",
				RefColumns: []string{"id"},
				OnDelete:   FKCascade,
				OnUpdate:   FKNoAction,
			},
			{
				Columns:    []string{"script_id"},
				RefTable:   "script",
				RefColumns: []string{"id"},
				OnDelete:   FKCascade,
				OnUpdate:   FKNoAction,
			},
		},
	},
	{
		Name: "_schema_state",
		Columns: []ColumnSpec{
			{Name: "key", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "value", Type: TypeText, NotNull: true},
			{Name: "updated_at", Type: TypeText, NotNull: true},
		},
	},
}

type SettingRow struct {
	Key   string
	Value *string
}

type AppStateRow struct {
	Key       string
	Value     string
	UpdatedAt string
}

type GamePathRow struct {
	Game                string
	ModFolderPath       string
	Importer            *string
	LinkedModFolderPath *string
	GameInstallPath     *string
	GameExecutablePath  *string
	NteLauncherPath     *string
	Order               int64
}

type ModPresetRow struct {
	ID          string
	Game        string
	Name        string
	Description *string
	ItemCount   int64
	CreatedAt   string
	UpdatedAt   string
	Version     int64
}

type ModPresetItemRow struct {
	PresetID          string
	ModKey            string
	RelativePath      string
	GroupRelativePath string
	FolderName        string
	IsEnabled         bool
	ItemOrder         int64
}

type ImageCacheRow struct {
	Hash  string
	Image []byte
	Size  int64
}

type TouchProfileVisionCacheRow struct {
	CacheKey  string
	Result    string
	UpdatedAt string
}

type ModScanCacheRow struct {
	Path      string
	Mtime     int64
	Payload   string
	UpdatedAt string
}

type ScriptType string

const (
	ScriptTypePython ScriptType = "python"
	ScriptTypeExec   ScriptType = "exec"
)

type ScriptRow struct {
	ID         string
	Name       string
	Source     []byte
	IsSrcZstd  bool
	Type       ScriptType
	Size       int64
	ZstdSize   *int64
	SHA256     string
	ZstdSHA256 *string
}

type ScriptPresetRow struct {
	ID   string
	Name string
}

type ScriptPresetItemRow struct {
	PresetID string
	ScriptID string
	Order    int64
}

type ScriptPresetWithScripts struct {
	ScriptPresetRow
	Scripts []ScriptPresetItemRow
}

type ScriptPresetItemUsage struct {
	PresetID   string
	ScriptID   string
	Order      int64
	PresetName string
}

type SchemaStateRow struct {
	Key       string
	Value     string
	UpdatedAt string
}

type ScriptBasicRow struct {
	ID   string
	Name string
	Type string
	Size int64
}
