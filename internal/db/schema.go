package db

const AppSchemaVersion = 6

const (
	SchemaKeyAppVersion                  = "app_schema_version"
	SchemaKeyGamePathsNTELauncher        = "game_paths_nte_launcher_path"
	SchemaKeyToggleViewerArtifactDropped = "toggle_viewer_artifact_dropped"
	SchemaKeyBlenderMCPDefaultSeeded     = "blender_mcp_default_seeded"
	NTEImporter                          = "NTE"
	NTEGameExeKeepSuffix                 = "%htgame.exe"
	// BlenderMCPTransport names the built-in Blender compatibility layer. It is the transport value
	// the agent package recognizes for servers that Nahida hosts itself instead of launching.
	BlenderMCPTransport = "blender"
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
		Name: "agent_session",
		Columns: []ColumnSpec{
			{Name: "id", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "scope_type", Type: TypeText, NotNull: true},
			{Name: "mod_path", Type: TypeText},
			{Name: "mod_name", Type: TypeText},
			{Name: "title", Type: TypeText, NotNull: true},
			{Name: "durable_summary", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "revert", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "created_at", Type: TypeText, NotNull: true},
			{Name: "updated_at", Type: TypeText, NotNull: true},
		},
		Indexes: []IndexSpec{
			{Name: "agent_session_scope_updated_idx", Columns: []string{"scope_type", "updated_at"}},
		},
	},
	{
		Name: "agent_event",
		Columns: []ColumnSpec{
			{Name: "session_id", Type: TypeText, NotNull: true},
			{Name: "sequence", Type: TypeInteger, NotNull: true},
			{Name: "turn_id", Type: TypeText, NotNull: true},
			{Name: "event_type", Type: TypeText, NotNull: true},
			{Name: "payload", Type: TypeText, NotNull: true},
			{Name: "created_at", Type: TypeText, NotNull: true},
		},
		CompositePrimaryKey: []string{"session_id", "sequence"},
		ForeignKeys: []ForeignKeySpec{{
			Columns: []string{"session_id"}, RefTable: "agent_session", RefColumns: []string{"id"},
			OnDelete: FKCascade, OnUpdate: FKNoAction,
		}},
	},
	{
		Name: "agent_approval",
		Columns: []ColumnSpec{
			{Name: "id", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "session_id", Type: TypeText, NotNull: true},
			{Name: "turn_id", Type: TypeText, NotNull: true},
			{Name: "tool_call_id", Type: TypeText, NotNull: true},
			{Name: "action_id", Type: TypeText, NotNull: true},
			{Name: "arguments", Type: TypeText, NotNull: true},
			{Name: "summary", Type: TypeText, NotNull: true},
			{Name: "target", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "impact", Type: TypeText, NotNull: true},
			{Name: "status", Type: TypeText, NotNull: true},
			{Name: "result", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "error", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "created_at", Type: TypeText, NotNull: true},
			{Name: "decided_at", Type: TypeText},
			{Name: "completed_at", Type: TypeText},
		},
		Indexes: []IndexSpec{
			{Name: "agent_approval_session_status_idx", Columns: []string{"session_id", "status"}},
			{Name: "agent_approval_tool_call_idx", Columns: []string{"session_id", "tool_call_id"}, Unique: true},
		},
		ForeignKeys: []ForeignKeySpec{{
			Columns: []string{"session_id"}, RefTable: "agent_session", RefColumns: []string{"id"},
			OnDelete: FKCascade, OnUpdate: FKNoAction,
		}},
	},
	{
		Name: "agent_mcp_server",
		Columns: []ColumnSpec{
			{Name: "id", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "name", Type: TypeText, NotNull: true},
			{Name: "transport", Type: TypeText, NotNull: true},
			{Name: "public_config", Type: TypeText, NotNull: true},
			{Name: "secret_blob", Type: TypeText, NotNull: true, DefaultSQL: sqlDefault("''")},
			{Name: "enabled", Type: TypeInteger, NotNull: true, Boolean: true, DefaultSQL: sqlDefault("0")},
			{Name: "created_at", Type: TypeText, NotNull: true},
			{Name: "updated_at", Type: TypeText, NotNull: true},
		},
		Indexes: []IndexSpec{{Name: "agent_mcp_server_name_idx", Columns: []string{"name"}, Unique: true}},
	},
	{
		Name: "backup_custom_path",
		Columns: []ColumnSpec{
			{Name: "id", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "path", Type: TypeText, NotNull: true},
			{Name: "label", Type: TypeText, NotNull: true},
			{Name: "order", Type: TypeInteger, NotNull: true, DefaultSQL: sqlDefault("0")},
		},
		Indexes: []IndexSpec{{Name: "backup_custom_path_path_idx", Columns: []string{"path"}, Unique: true}},
	},
	{
		Name: "backup_file_cache",
		Columns: []ColumnSpec{
			{Name: "path", Type: TypeText, PrimaryKey: true, NotNull: true},
			{Name: "size", Type: TypeInteger, NotNull: true},
			{Name: "mtime", Type: TypeInteger, NotNull: true},
			{Name: "sha256", Type: TypeText, NotNull: true},
		},
	},
	{
		Name: "backup_committed",
		Columns: []ColumnSpec{
			{Name: "target_key", Type: TypeText, NotNull: true},
			{Name: "rel_path", Type: TypeText, NotNull: true},
			{Name: "sha256", Type: TypeText, NotNull: true},
			{Name: "size", Type: TypeInteger},
			{Name: "mtime", Type: TypeInteger},
		},
		CompositePrimaryKey: []string{"target_key", "rel_path"},
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

type BackupCustomPathRow struct {
	ID    string
	Path  string
	Label string
	Order int64
}

type BackupFileCacheRow struct {
	Path   string
	Size   int64
	Mtime  int64
	SHA256 string
}

type BackupCommittedRow struct {
	TargetKey string
	RelPath   string
	SHA256    string
	Size      *int64
	Mtime     *int64
}

type ModScanCacheRow struct {
	Path      string
	Mtime     int64
	Payload   string
	UpdatedAt string
}

type AgentSessionRow struct {
	ID             string
	ScopeType      string
	ModPath        *string
	ModName        *string
	Title          string
	DurableSummary string
	Revert         string
	CreatedAt      string
	UpdatedAt      string
}

type AgentEventRow struct {
	SessionID string
	Sequence  int64
	TurnID    string
	EventType string
	Payload   string
	CreatedAt string
}

type AgentApprovalRow struct {
	ID          string
	SessionID   string
	TurnID      string
	ToolCallID  string
	ActionID    string
	Arguments   string
	Summary     string
	Target      string
	Impact      string
	Status      string
	Result      string
	Error       string
	CreatedAt   string
	DecidedAt   *string
	CompletedAt *string
}

type AgentMCPServerRow struct {
	ID           string
	Name         string
	Transport    string
	PublicConfig string
	SecretBlob   string
	Enabled      bool
	CreatedAt    string
	UpdatedAt    string
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
