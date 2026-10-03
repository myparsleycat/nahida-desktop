package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

type RuntimeMode string

const (
	RuntimeXXMI   RuntimeMode = "xxmi"
	RuntimeLegacy RuntimeMode = "legacy"
)

const (
	// followShared makes an importer use the XXMI libraries version selected for the whole runtime.
	followShared         = "shared"
	sharedLibsVersionKey = "xxmi_libs_version"
)

type VersionPin struct {
	Follow string `json:"follow,omitempty"`
	Pinned string `json:"pinned,omitempty"`
	// Notify keeps announcing newer releases for a pinned XXMI libraries version; installing one moves the pin.
	Notify bool `json:"notify,omitempty"`
}

// UnmarshalJSON replaces the whole pin so a stored pin cannot merge with a default follow value.
func (p *VersionPin) UnmarshalJSON(data []byte) error {
	type rawVersionPin VersionPin
	var pin rawVersionPin
	if err := json.Unmarshal(data, &pin); err != nil {
		return err
	}
	*p = VersionPin(pin)
	return nil
}

type CommandHook struct {
	Enabled bool   `json:"enabled"`
	Command string `json:"command"`
	Wait    bool   `json:"wait"`
}

// CustomLaunch is the command that starts the game when GameLaunch is "Custom".
type CustomLaunch struct {
	Command string `json:"command"`
}

type ExtraLibraries struct {
	Enabled bool     `json:"enabled"`
	Paths   []string `json:"paths"`
}

type MigotoOptions struct {
	// LogLevel is one of "Disabled", "Warning", "Info", or "Debug".
	LogLevel             string `json:"logLevel"`
	EnforceRendering     bool   `json:"enforceRendering"`
	EnableHunting        bool   `json:"enableHunting"`
	DumpShaders          bool   `json:"dumpShaders"`
	MuteWarnings         bool   `json:"muteWarnings"`
	ClearUnknownSettings bool   `json:"clearUnknownSettings"`
	Input                bool   `json:"input"`
	// InputDisableMode selects what turning Input off disables: "Mods" or "All".
	InputDisableMode string `json:"inputDisableMode"`
	ToggleInput      string `json:"toggleInput"`
	UnsafeMode       bool   `json:"unsafeMode"`
}

type IniOptimizerOptions struct {
	Enabled    bool `json:"enabled"`
	ResetCache bool `json:"resetCache"`
}

type GIMIOptions struct {
	UnlockFPS      bool `json:"unlockFPS"`
	UnlockFPSValue int  `json:"unlockFPSValue"`
	EnableHDR      bool `json:"enableHDR"`
}

type SRMIOptions struct {
	UnlockFPS bool `json:"unlockFPS"`
}

type HIMIOptions struct {
	UnlockFPS      bool `json:"unlockFPS"`
	UnlockFPSValue int  `json:"unlockFPSValue"`
}

type WWMIOptions struct {
	UnlockFPS              bool `json:"unlockFPS"`
	ForceMaxLODBias        bool `json:"forceMaxLODBias"`
	DisableWoundedFX       bool `json:"disableWoundedFX"`
	MeshLODDistanceBaseFOV int  `json:"meshLODDistanceBaseFOV"`
	// ResourceTier is the downloaded resource quality passed to the game: "UHD", "HD", or "SD".
	ResourceTier        string `json:"resourceTier"`
	ResourceTierDecided bool   `json:"resourceTierDecided"`
}

// importerConfigSchema is the stored ImporterConfig layout. Version 2 follows XXMI Launcher 2.3.
const importerConfigSchema = 2

type ImporterConfig struct {
	SchemaVersion      int               `json:"schemaVersion"`
	Enabled            bool              `json:"enabled"`
	Mode               RuntimeMode       `json:"mode"`
	PackageVersion     VersionPin        `json:"packageVersion"`
	XXMIVersion        VersionPin        `json:"xxmiVersion"`
	LegacyRuntime      string            `json:"legacyRuntime"`
	DeployedSignatures map[string]string `json:"deployedSignatures,omitempty"`
	ImporterFolder     string            `json:"importerFolder"`
	GameFolder         string            `json:"gameFolder"`
	// GameLaunch selects what starts the game: "Direct", "Steam", "Epic", "Custom", or "Manual".
	GameLaunch string `json:"gameLaunch"`
	// GameProcessExe overrides the image name of the game process; empty resolves it from the game folder.
	GameProcessExe                 string `json:"gameProcessExe"`
	ConfigurePlatformLaunchOptions bool   `json:"configurePlatformLaunchOptions"`
	SkipPlatformGameLauncher       bool   `json:"skipPlatformGameLauncher"`
	UseLaunchOptions               bool   `json:"useLaunchOptions"`
	LaunchOptions                  string `json:"launchOptions"`
	// ProcessStartMethod is "Native" or "Shell".
	ProcessStartMethod string `json:"processStartMethod"`
	InjectionMethod    string `json:"injectionMethod"`
	ProcessPriority    string `json:"processPriority"`
	ProcessTimeout     int    `json:"processTimeout"`
	XXMIDLLInitDelay   int    `json:"xxmiDLLInitDelay"`
	// XXMIDLLInjectMode is "Hook", "Inject", or "Bypass" and applies to every GameLaunch.
	XXMIDLLInjectMode string              `json:"xxmiDLLInjectMode"`
	WindowMode        string              `json:"windowMode"`
	RunPreLaunch      CommandHook         `json:"runPreLaunch"`
	CustomLaunch      CustomLaunch        `json:"customLaunch"`
	RunPostLoad       CommandHook         `json:"runPostLoad"`
	ExtraLibraries    ExtraLibraries      `json:"extraLibraries"`
	OverwriteINI      bool                `json:"overwriteINI"`
	ConfigureGame     bool                `json:"configureGame"`
	Migoto            MigotoOptions       `json:"migoto"`
	IniOptimizer      IniOptimizerOptions `json:"iniOptimizer"`
	LaunchCount       int                 `json:"launchCount"`
	ShortcutPath      string              `json:"shortcutPath"`
	WoundedFXDecided  bool                `json:"woundedFXDecided"`
	GIMI              *GIMIOptions        `json:"gimi,omitempty"`
	SRMI              *SRMIOptions        `json:"srmi,omitempty"`
	HIMI              *HIMIOptions        `json:"himi,omitempty"`
	WWMI              *WWMIOptions        `json:"wwmi,omitempty"`
}

func xxmiCacheRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, appdata.RootDirName, "xxmi"), nil
}

func (x *XXMI) SetRoot(ctx context.Context, path string) error {
	path = strings.TrimSpace(path)
	if err := validateLocalFolder("xxmi root", path, true); err != nil {
		return err
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	return client.Settings.Upsert(ctx, "xxmi_root", &path)
}

func DefaultImporterConfig(key, root string) (ImporterConfig, error) {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return ImporterConfig{}, fmt.Errorf("unknown importer %q", key)
	}
	cfg := ImporterConfig{
		SchemaVersion: importerConfigSchema, Mode: RuntimeXXMI,
		PackageVersion: VersionPin{Follow: "latest"}, XXMIVersion: VersionPin{Follow: followShared},
		ImporterFolder: filepath.Join(root, key), GameLaunch: "Direct",
		ConfigurePlatformLaunchOptions: true, SkipPlatformGameLauncher: true,
		ProcessStartMethod: "Native", InjectionMethod: "Default", XXMIDLLInjectMode: "Inject",
		ProcessPriority: "Normal", ProcessTimeout: 30, WindowMode: "Borderless",
		UseLaunchOptions: true, OverwriteINI: true, ConfigureGame: true,
		RunPreLaunch: CommandHook{Wait: true}, RunPostLoad: CommandHook{Wait: true},
		Migoto: MigotoOptions{
			LogLevel: "Disabled", EnforceRendering: true, MuteWarnings: true, ClearUnknownSettings: true,
			Input: true, InputDisableMode: "Mods", ToggleInput: "ctrl alt shift VK_END",
		},
		LaunchCount: -1,
	}
	if spec.useHook {
		cfg.XXMIDLLInjectMode = "Hook"
	}
	switch spec.key {
	case "GIMI":
		cfg.ProcessStartMethod = "Shell"
		cfg.GIMI = &GIMIOptions{UnlockFPSValue: 120}
	case "SRMI":
		cfg.SRMI = &SRMIOptions{}
	case "HIMI":
		cfg.HIMI = &HIMIOptions{UnlockFPSValue: 120}
	case "WWMI":
		cfg.WWMI = &WWMIOptions{MeshLODDistanceBaseFOV: 165, ResourceTier: "HD"}
		cfg.XXMIDLLInitDelay = 500
		cfg.UseLaunchOptions = false
		cfg.LaunchOptions = "-SkipSplash"
	case "EFMI":
		cfg.ProcessTimeout = 60
	}
	return cfg, nil
}

// decodeImporterConfig applies a stored config over the importer defaults and upgrades older layouts.
func decodeImporterConfig(key, root, stored string) (ImporterConfig, error) {
	cfg, err := DefaultImporterConfig(key, root)
	if err != nil {
		return ImporterConfig{}, err
	}
	defaults := cfg
	if err := json.Unmarshal([]byte(stored), &cfg); err != nil {
		return ImporterConfig{}, fmt.Errorf("decode importer %s config: %w", key, err)
	}
	if cfg.SchemaVersion != 1 {
		return cfg, nil
	}

	// Schema 1 tied the injection mode to custom launches, used a "Manual" start method,
	// and stored logging as two switches. The mapping follows the launcher's own 2.3.0 config patch.
	var legacy struct {
		ProcessStartMethod string `json:"processStartMethod"`
		XXMIDLLInjectMode  string `json:"xxmiDLLInjectMode"`
		CustomLaunch       struct {
			Enabled    bool   `json:"enabled"`
			InjectMode string `json:"injectMode"`
		} `json:"customLaunch"`
		Migoto struct {
			LogLevel     string `json:"logLevel"`
			CallsLogging bool   `json:"callsLogging"`
			DebugLogging bool   `json:"debugLogging"`
		} `json:"migoto"`
	}
	if err := json.Unmarshal([]byte(stored), &legacy); err != nil {
		return ImporterConfig{}, fmt.Errorf("decode importer %s config: %w", key, err)
	}
	cfg.SchemaVersion = importerConfigSchema
	cfg.GameLaunch = "Direct"
	if legacy.CustomLaunch.Enabled {
		if strings.TrimSpace(cfg.CustomLaunch.Command) != "" {
			cfg.GameLaunch = "Custom"
		}
		if legacy.XXMIDLLInjectMode == "" && legacy.CustomLaunch.InjectMode != "" {
			cfg.XXMIDLLInjectMode = legacy.CustomLaunch.InjectMode
		}
	}
	if legacy.ProcessStartMethod == "Manual" {
		cfg.GameLaunch = "Manual"
		cfg.ProcessStartMethod = defaults.ProcessStartMethod
	}
	switch {
	case legacy.Migoto.LogLevel != "":
		cfg.Migoto.LogLevel = externalEnumValue(legacy.Migoto.LogLevel, "Disabled", "Warning", "Info", "Debug")
	case legacy.Migoto.DebugLogging:
		cfg.Migoto.LogLevel = "Debug"
	case legacy.Migoto.CallsLogging:
		cfg.Migoto.LogLevel = "Info"
	default:
		cfg.Migoto.LogLevel = "Disabled"
	}
	return cfg, nil
}

func (x *XXMI) GetImporterConfig(ctx context.Context, key string) (ImporterConfig, error) {
	key = strings.ToUpper(strings.TrimSpace(key))
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return ImporterConfig{}, errors.New("XXMI settings store is not configured")
	}
	row, err := client.XXMIImporters.Get(ctx, key)
	if err != nil {
		return ImporterConfig{}, err
	}
	root, err := client.Settings.GetValue(ctx, "xxmi_root")
	if err != nil {
		return ImporterConfig{}, err
	}
	rootPath := ""
	if root != nil {
		rootPath = *root
	}
	if rootPath == "" {
		rootPath, err = xxmiCacheRoot()
		if err != nil {
			return ImporterConfig{}, err
		}
	}
	if row == nil {
		return DefaultImporterConfig(key, rootPath)
	}
	return decodeImporterConfig(key, rootPath, row.Config)
}

func (x *XXMI) SaveImporterConfig(ctx context.Context, key string, cfg ImporterConfig) error {
	// Rows are stored under the canonical key so every reader and the importer locks agree on it.
	key = strings.ToUpper(strings.TrimSpace(key))
	if err := ValidateImporterSettings(key, cfg); err != nil {
		return err
	}
	if err := validateInstalledImporterPackage(key, cfg); err != nil {
		return infra.ReportError(x.log, err, "XXMI.SaveImporterConfig", infra.Diagnostic{
			Operation: "save-importer-config", Stage: "validate-package",
			Fields: map[string]any{"importer": key, "version": cfg.PackageVersion.Pinned,
				"importerFolder": cfg.ImporterFolder},
		})
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	previous, err := client.XXMIImporters.Get(ctx, key)
	if err != nil {
		return err
	}
	if previous != nil {
		var old ImporterConfig
		if err := json.Unmarshal([]byte(previous.Config), &old); err != nil {
			return err
		}
		if old.Mode != cfg.Mode {
			if !x.acquireImporter(key) {
				return errors.New("XXMI_GAME_RUNNING")
			}
			defer x.releaseImporter(key)
			spec, _ := lookupImporterPackage(key)
			for _, name := range append(append([]string{}, spec.gameExeNames...), spec.processNames...) {
				pid, err := x.findProcess(ctx, name)
				if err != nil {
					return err
				}
				if pid != 0 {
					return errors.New("XXMI_GAME_RUNNING")
				}
			}
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := client.XXMIImporters.Upsert(ctx, key, string(data)); err != nil {
		return err
	}
	x.wakeRunningWatch()
	return nil
}

func (x *XXMI) EnableImporter(ctx context.Context, key, folder string) error {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return err
	}
	if folder != "" {
		cfg.ImporterFolder = folder
	}
	cfg.Enabled = true
	return x.SaveImporterConfig(ctx, key, cfg)
}

func (x *XXMI) DisableImporter(ctx context.Context, key string) error {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return err
	}
	cfg.Enabled = false
	return x.SaveImporterConfig(ctx, key, cfg)
}

func (x *XXMI) SetImporterMode(ctx context.Context, key string, mode RuntimeMode) error {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return err
	}
	cfg.Mode = mode
	return x.SaveImporterConfig(ctx, key, cfg)
}

type ImporterVersions struct {
	Package *VersionPin `json:"package,omitempty"`
	XXMI    *VersionPin `json:"xxmi,omitempty"`
}

func (x *XXMI) SetImporterVersions(ctx context.Context, key string, versions ImporterVersions) error {
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return err
	}
	if versions.XXMI != nil {
		if versions.XXMI.Pinned != "" {
			if err := x.EnsureLibsVersion(ctx, versions.XXMI.Pinned); err != nil {
				return err
			}
		}
		cfg.XXMIVersion = *versions.XXMI
	}
	if versions.Package != nil {
		cfg.PackageVersion = *versions.Package
	}
	return x.SaveImporterConfig(ctx, key, cfg)
}

// SetSharedLibsVersion selects the XXMI libraries version for importers that follow the shared version.
// An empty version follows the latest release.
func (x *XXMI) SetSharedLibsVersion(ctx context.Context, version string) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	version = normalizeVersion(version)
	if version != "" {
		if err := x.EnsureLibsVersion(ctx, version); err != nil {
			return err
		}
	}
	return client.Settings.Upsert(ctx, sharedLibsVersionKey, &version)
}

// libsPin returns the XXMI libraries version an importer is held to, or "" when it follows the latest release.
// notify reports whether newer releases are still announced for that version.
func (x *XXMI) libsPin(ctx context.Context, cfg ImporterConfig) (version string, notify bool, err error) {
	if cfg.XXMIVersion.Follow != followShared {
		return normalizeVersion(cfg.XXMIVersion.Pinned), cfg.XXMIVersion.Notify, nil
	}
	client, err := x.settingsClient()
	if err != nil {
		return "", false, err
	}
	shared, err := client.Settings.GetValue(ctx, sharedLibsVersionKey)
	if err != nil || shared == nil {
		return "", false, err
	}
	return normalizeVersion(*shared), false, nil
}
