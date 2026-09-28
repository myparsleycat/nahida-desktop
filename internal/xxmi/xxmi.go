package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi/inject"
)

const (
	xxmiPathKey    = "xxmi.path"
	xxmiConfigName = "XXMI Launcher Config.json"
)

var libsRepo = github.Repo{Owner: "SpectrumQT", Name: "XXMI-Libs-Package"}

type Options struct {
	HTTP        *infra.Client
	Log         *infra.Log
	Download    *infra.Download
	Archive     *infra.Archive
	EventEmit   func(name string, data ...any)
	SearchRoots func() ([]string, error)
	// GitHub serves release lists and files; nil builds one from HTTP and Download.
	GitHub   *github.Client
	Elevated elevatedLauncher
}

type elevatedLauncher interface {
	Acquire(context.Context) (func(), error)
	LaunchXXMI(context.Context, inject.LaunchSpec) (inject.LaunchResult, error)
	HelperImageName() string
}

type PackageInfo struct {
	LatestVersion        string  `json:"latest_version"`
	SkippedVersion       string  `json:"skipped_version"`
	DeployedVersion      string  `json:"deployed_version"`
	UpdateCheckTime      float64 `json:"update_check_time"`
	LatestReleaseNotes   string  `json:"latest_release_notes"`
	DeployedReleaseNotes string  `json:"deployed_release_notes"`
}

type EnabledImporter struct {
	Key              string      `json:"key"`
	Mode             RuntimeMode `json:"mode"`
	ImporterFolder   string      `json:"importerFolder"`
	InstalledVersion *string     `json:"installedVersion"`
	PackageInfo      PackageInfo `json:"packageInfo"`
}

type Data struct {
	XXMIPath         *string           `json:"xxmiPath"`
	DLLVersion       *string           `json:"dllVersion"`
	EnabledImporters []EnabledImporter `json:"enabledImporters"`
	XXMIConfig       map[string]any    `json:"xxmiConfig"`
}

// HuntingRuntime is the resolved on-disk and process metadata a high-level
// hunting session needs. It is deliberately not part of the Wails surface.
type HuntingRuntime struct {
	ImporterKey    string
	ImporterFolder string
	INIPath        string
	GameEXENames   []string
}

type parsedConfig struct {
	Launcher struct {
		StartTimeout float64 `json:"start_timeout"`
	} `json:"Launcher"`
	Packages struct {
		Packages map[string]PackageInfo `json:"packages"`
	} `json:"Packages"`
	Importers map[string]struct {
		Importer struct {
			GameEXENames   []string `json:"game_exe_names"`
			GameFolder     string   `json:"game_folder"`
			ImporterFolder string   `json:"importer_folder"`
			OverwriteINI   bool     `json:"overwrite_ini"`
		} `json:"Importer"`
	} `json:"Importers"`
	Security map[string]any `json:"Security"`
}

type XXMI struct {
	mu          sync.RWMutex
	client      *db.Client
	log         *infra.Log
	github      *github.Client
	archive     *infra.Archive
	elevated    elevatedLauncher
	eventEmit   func(string, ...any)
	searchRoots func() ([]string, error)
	path        *string
	config      map[string]any
	parsed      parsedConfig
	busy        bool
}

func New() *XXMI {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *XXMI {
	searchRoots := opts.SearchRoots
	if searchRoots == nil {
		searchRoots = xxmiSearchRoots
	}
	githubClient := opts.GitHub
	if githubClient == nil {
		githubClient = github.New(github.Options{HTTP: opts.HTTP, Download: opts.Download, Log: opts.Log})
	}
	return &XXMI{
		log: opts.Log, github: githubClient, archive: opts.Archive,
		eventEmit: opts.EventEmit, searchRoots: searchRoots, elevated: opts.Elevated,
	}
}

//wails:ignore
func (x *XXMI) UseClient(client *db.Client) {
	x.mu.Lock()
	x.client = client
	x.mu.Unlock()
}

// EnsureLauncherClosed closes the launcher before DLL replacement operations.
//
//wails:ignore
func (x *XXMI) EnsureLauncherClosed(ctx context.Context) error {
	return ensureLauncherClosed(ctx)
}

func (x *XXMI) GetXXMIPath(ctx context.Context) (*string, error) {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil, errors.New("XXMI settings store is not configured")
	}
	root, err := client.Settings.GetValue(ctx, "xxmi_root")
	if err != nil {
		return nil, err
	}
	if root != nil && *root != "" {
		return root, nil
	}
	fallback, err := xxmiCacheRoot()
	return &fallback, err
}

func (x *XXMI) externalLauncherPath(ctx context.Context) (*string, error) {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil, errors.New("XXMI settings store is not configured")
	}
	value, err := client.Settings.GetValue(ctx, xxmiPathKey)
	if err != nil || value == nil || strings.TrimSpace(*value) == "" {
		return nil, err
	}
	cleaned := filepath.Clean(*value)
	return &cleaned, nil
}

func (x *XXMI) SaveXXMIPath(ctx context.Context, inputPath string) error {
	absolute, err := filepath.Abs(strings.TrimSpace(inputPath))
	if err != nil {
		return err
	}
	config, parsed, err := readAndValidateConfig(filepath.Join(absolute, xxmiConfigName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return infra.WithCause(errors.New("XXMI Launcher Config.json not found"), err)
		}
		return fmt.Errorf("XXMI Launcher Config.json is invalid: %w", err)
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	if err := client.Settings.Upsert(ctx, xxmiPathKey, &absolute); err != nil {
		return err
	}
	x.mu.Lock()
	x.path = &absolute
	x.config = config
	x.parsed = parsed
	x.mu.Unlock()
	if x.eventEmit != nil {
		x.eventEmit("renderer:reload")
	}
	return nil
}

func (x *XXMI) FindXXMIPath(ctx context.Context) (*string, error) {
	appData := strings.TrimSpace(os.Getenv("APPDATA"))
	if appData != "" {
		candidate := filepath.Join(appData, "XXMI Launcher")
		if isValidConfig(filepath.Join(candidate, xxmiConfigName)) {
			return &candidate, nil
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	roots, err := x.searchRoots()
	if err != nil {
		return nil, err
	}
	var diagnostics infra.DiagnosticBatch
	defer diagnostics.Report(x.log, "XXMI", "find-config")
	result, err := findFileAcrossRoots(ctx, roots, xxmiConfigName, map[string]struct{}{"Backups": {}}, diagnostics.Add)
	if err != nil || result == nil {
		return nil, err
	}
	directory := filepath.Dir(*result)
	return &directory, nil
}

func isValidConfig(path string) bool {
	_, _, err := readAndValidateConfig(path)
	return err == nil
}

func (x *XXMI) GetXXMIConfig(ctx context.Context) (map[string]any, error) {
	if err := x.load(ctx); err != nil {
		return nil, err
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	return cloneMap(x.config), nil
}

func (x *XXMI) GetXXMIData(ctx context.Context) (Data, error) {
	builtin, err := x.builtinEnabledImporters(ctx)
	if err != nil {
		return Data{}, err
	}
	root, err := x.GetXXMIPath(ctx)
	if err != nil {
		return Data{}, err
	}
	return Data{XXMIPath: root, EnabledImporters: builtin, XXMIConfig: map[string]any{}}, nil
}

func (x *XXMI) GetEnabledImporters(ctx context.Context) ([]EnabledImporter, error) {
	return x.builtinEnabledImporters(ctx)
}

// ResolveHuntingRuntime returns the active importer's game and INI metadata.
//
//wails:ignore
func (x *XXMI) ResolveHuntingRuntime(ctx context.Context, importerKey string) (HuntingRuntime, error) {
	builtin, err := x.builtinEnabledImporters(ctx)
	if err != nil {
		return HuntingRuntime{}, err
	}
	for _, importer := range builtin {
		if !strings.EqualFold(importer.Key, importerKey) {
			continue
		}
		spec, ok := lookupImporterPackage(importer.Key)
		if !ok {
			return HuntingRuntime{}, fmt.Errorf("unknown importer %q", importerKey)
		}
		names := spec.processNames
		if len(names) == 0 {
			names = spec.gameExeNames
		}
		return HuntingRuntime{ImporterKey: importer.Key, ImporterFolder: importer.ImporterFolder,
			INIPath: filepath.Join(importer.ImporterFolder, "d3dx.ini"), GameEXENames: slices.Clone(names)}, nil
	}
	return HuntingRuntime{}, fmt.Errorf("unknown importer %q", importerKey)
}

func (x *XXMI) GetLibsReleases(ctx context.Context) ([]string, error) {
	return x.releaseTags(ctx, libsRepo, false)
}

func (x *XXMI) UpdateLibsReleases(ctx context.Context) error {
	_, err := x.releaseTags(ctx, libsRepo, true)
	return err
}

func (x *XXMI) GetImporterReleases(ctx context.Context, importer string) ([]string, error) {
	spec, ok := lookupImporterPackage(importer)
	if !ok {
		return nil, errors.New("unknown importer")
	}
	return x.releaseTags(ctx, spec.repo, false)
}

func (x *XXMI) releaseTags(ctx context.Context, repo github.Repo, refresh bool) ([]string, error) {
	tags, err := x.github.ReleaseTags(ctx, repo, refresh)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch %s releases: %w", repo, err)
	}
	return tags, nil
}

func (x *XXMI) load(ctx context.Context) error {
	path, err := x.GetXXMIPath(ctx)
	if err != nil {
		return err
	}
	if path == nil {
		x.mu.Lock()
		x.path = nil
		x.config = nil
		x.parsed = parsedConfig{}
		x.mu.Unlock()
		return nil
	}
	config, parsed, err := readAndValidateConfig(filepath.Join(*path, xxmiConfigName))
	if err != nil {
		if x.log != nil {
			_ = infra.ReportError(
				x.log,
				err,
				"XXMI.initialize",
				infra.Diagnostic{Operation: "initialize", Stage: "background"},
			)
		}
		x.mu.Lock()
		x.path = cloneString(path)
		x.config = nil
		x.parsed = parsedConfig{}
		x.mu.Unlock()
		return nil
	}
	x.mu.Lock()
	x.path = cloneString(path)
	x.config = config
	x.parsed = parsed
	x.mu.Unlock()
	return nil
}

func readAndValidateConfig(path string) (map[string]any, parsedConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, parsedConfig{}, err
	}
	return parseAndValidateConfig(raw)
}

func parseAndValidateConfig(raw []byte) (map[string]any, parsedConfig, error) {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, parsedConfig{}, err
	}
	if err := validateXXMIConfig(config); err != nil {
		return nil, parsedConfig{}, err
	}
	var parsed parsedConfig
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, parsedConfig{}, err
	}
	return config, parsed, nil
}

func dllVersion(path *string) *string {
	if path == nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(*path, "Resources", "Packages", "XXMI", "Manifest.json"))
	if err != nil {
		return nil
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &manifest) != nil || strings.TrimSpace(manifest.Version) == "" {
		return nil
	}
	return &manifest.Version
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	raw, _ := json.Marshal(value)
	var cloned map[string]any
	_ = json.Unmarshal(raw, &cloned)
	return cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (x *XXMI) reportCleanup(err error, operation string) {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = infra.ReportError(x.log, err, "XXMI", infra.Diagnostic{Operation: operation, Stage: "cleanup"})
}
