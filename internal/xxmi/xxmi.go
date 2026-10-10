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
	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/reshade"
	"nahida.live/desktop/internal/xxmi/gameplatform"
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
	// ReShade prepares the module a launch injects for importers that turn ReShade on.
	ReShade reshadeProvider
}

type reshadeProvider interface {
	PrepareLaunch(ctx context.Context, importer string) (string, error)
	LaunchPresetEffects(ctx context.Context, importer string) (reshade.PresetEffects, error)
}

type elevatedLauncher interface {
	Acquire(context.Context) (func(), error)
	LaunchXXMI(context.Context, inject.LaunchSpec) (inject.LaunchResult, error)
	ApplyFiles(context.Context, []elevated.FileOp) error
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
	GameFolder       string      `json:"gameFolder"`
	Running          bool        `json:"running"`
	UpdateAvailable  bool        `json:"updateAvailable"`
	InstalledVersion *string     `json:"installedVersion"`
	PackageInfo      PackageInfo `json:"packageInfo"`
	// CustomDLL reports that the built-in runtime launches with a user-provided d3d11.dll instead of the signed one.
	CustomDLL bool `json:"customDll"`
}

type Data struct {
	Mode             LauncherMode      `json:"mode"`
	XXMIPath         *string           `json:"xxmiPath"`
	DLLVersion       *string           `json:"dllVersion"`
	EnabledImporters []EnabledImporter `json:"enabledImporters"`
	// DisabledImporters lists external launcher importers the user turned off for this app.
	DisabledImporters []EnabledImporter `json:"disabledImporters"`
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
		Importer externalImporter `json:"Importer"`
	} `json:"Importers"`
	Security map[string]any `json:"Security"`
}

// externalImporter holds the external launcher importer settings this app reads.
// The process and launch fields exist only in XXMI Launcher 2.3 and later configs.
type externalImporter struct {
	GameEXENames          []string `json:"game_exe_names"`
	ProcessEXENames       []string `json:"process_exe_names"`
	GameProcessEXEEnabled bool     `json:"game_process_exe_enabled"`
	GameProcessEXE        string   `json:"game_process_exe"`
	GameLaunch            string   `json:"game_launch"`
	GameFolder            string   `json:"game_folder"`
	ImporterFolder        string   `json:"importer_folder"`
	OverwriteINI          bool     `json:"overwrite_ini"`
}

type XXMI struct {
	mu        sync.RWMutex
	packageMu sync.Mutex
	// disabledMu serializes read-modify-write updates of the disabled external importer list.
	disabledMu sync.Mutex
	// externalConfigMu serializes read-modify-write updates of the external launcher's Config.json.
	externalConfigMu sync.Mutex
	// customDLLMu serializes imports, selection writes, and cleanup of the custom DLL cache.
	customDLLMu sync.Mutex
	// libsProviderMu keeps shared provider selections in the order they were made.
	libsProviderMu sync.Mutex
	// launcherScanMu guards the remembered result of the drive-wide search for an external launcher.
	launcherScanMu  sync.Mutex
	launcherScanned bool
	launcherScan    *string
	// importedCustomDLLs protects this session's imports, including drafts that have not been saved yet.
	importedCustomDLLs map[string]bool

	client      *db.Client
	log         *infra.Log
	github      *github.Client
	archive     *infra.Archive
	elevated    elevatedLauncher
	reshade     reshadeProvider
	eventEmit   func(string, ...any)
	searchRoots func() ([]string, error)
	busy        map[string]bool
	// launching holds importers whose game is being started; unlike busy, it is reported as running.
	launching                map[string]bool
	runningWatchMu           sync.Mutex
	runningCancel            context.CancelFunc
	runningDone              chan struct{}
	runningWake              chan struct{}
	processSnapshot          func(context.Context) (map[string]bool, error)
	externalImportersChanged func(context.Context)
	importerMaintenance      func(context.Context) (func([]ImportedImporter) error, error)
	// findProcess queries running games; tests replace it to stay independent of the host process list.
	findProcess func(context.Context, string) (int, error)
	// killExecutableProcess returns a terminator for processes running from one executable; tests
	// replace it so no host process is opened or terminated.
	killExecutableProcess func(executable string) func(pid int) error
	// launchSettings accesses registry and driver settings; tests replace it to isolate host state.
	launchSettings launchFixer
	// installImporter installs an importer package; tests replace it to avoid signed GitHub releases.
	installImporter func(context.Context, importerPackageSpec, ImporterConfig, InstallImporterPackageInput) error
	// prepareImportLibraries prepares a verified cache; tests replace it to avoid signed GitHub releases.
	prepareImportLibraries func(context.Context, string, string, string) error
	// renameUserData moves imported user data; tests replace it to simulate folders on different volumes.
	renameUserData func(from, to string) error
	// findSteam, epicManifest, runPlatformClient, and openLaunchURI reach the store clients; tests
	// replace them with fixtures so no installed client is read or started.
	findSteam         func() (gameplatform.Steam, bool)
	epicManifest      func() string
	runPlatformClient func(exe string, args ...string) error
	openLaunchURI     func(uri string) error
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
	x := &XXMI{
		log: opts.Log, github: githubClient, archive: opts.Archive,
		eventEmit: opts.EventEmit, searchRoots: searchRoots, elevated: opts.Elevated, reshade: opts.ReShade,
		runningWake: make(chan struct{}, 1), processSnapshot: snapshotProcessNames,
	}
	x.installImporter = x.installBuiltinImporterPackage
	x.prepareImportLibraries = x.cacheImportLibraries
	x.findProcess = findProcessPID
	x.killExecutableProcess = killProcessForExecutable
	x.launchSettings = x
	x.renameUserData = os.Rename
	x.findSteam = gameplatform.FindSteam
	x.epicManifest = gameplatform.EpicManifestPath
	x.runPlatformClient = startPlatformClient
	x.openLaunchURI = openShellURI
	return x
}

//wails:ignore
func (x *XXMI) UseClient(client *db.Client) {
	x.mu.Lock()
	x.client = client
	x.mu.Unlock()
}

// UseExternalImportersChanged registers a backend consumer of external importer selection changes.
//
//wails:ignore
func (x *XXMI) UseExternalImportersChanged(changed func(context.Context)) {
	x.mu.Lock()
	x.externalImportersChanged = changed
	x.mu.Unlock()
}

// UseImporterMaintenance registers the backend pause/resume boundary for importer filesystem changes.
//
//wails:ignore
func (x *XXMI) UseImporterMaintenance(begin func(context.Context) (func([]ImportedImporter) error, error)) {
	x.mu.Lock()
	x.importerMaintenance = begin
	x.mu.Unlock()
}

// GetXXMIPath returns the external launcher folder in external mode and the built-in root otherwise.
func (x *XXMI) GetXXMIPath(ctx context.Context) (*string, error) {
	client, err := x.settingsClient()
	if err != nil {
		return nil, err
	}
	mode, err := launcherMode(ctx, client)
	if err != nil {
		return nil, err
	}
	if mode == LauncherExternal {
		return x.externalLauncherPath(ctx)
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
	client, err := x.settingsClient()
	if err != nil {
		return nil, err
	}
	value, err := client.Settings.GetValue(ctx, xxmiPathKey)
	if err != nil || value == nil || strings.TrimSpace(*value) == "" {
		return nil, err
	}
	cleaned := filepath.Clean(*value)
	return &cleaned, nil
}

func (x *XXMI) GetXXMIData(ctx context.Context) (Data, error) {
	mode, err := x.GetLauncherMode(ctx)
	if err != nil {
		return Data{}, err
	}
	importers, err := x.GetEnabledImporters(ctx)
	if err != nil {
		return Data{}, err
	}
	root, err := x.GetXXMIPath(ctx)
	if err != nil {
		return Data{}, err
	}
	data := Data{Mode: mode, XXMIPath: root, EnabledImporters: importers}
	if mode == LauncherExternal {
		data.DLLVersion = dllVersion(root)
		launcher, err := x.loadExternalLauncher(ctx)
		if err != nil {
			return Data{}, err
		}
		if launcher != nil {
			data.DisabledImporters = launcher.importers(func(key string) bool {
				return launcher.available(key) && !launcher.enabled(key)
			})
		}
	}
	return data, nil
}

func (x *XXMI) GetEnabledImporters(ctx context.Context) ([]EnabledImporter, error) {
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return nil, err
	}
	if external {
		return x.externalEnabledImporters(ctx)
	}
	return x.builtinEnabledImporters(ctx)
}

// ResolveHuntingRuntime returns the active importer's game and INI metadata.
//
//wails:ignore
func (x *XXMI) ResolveHuntingRuntime(ctx context.Context, importerKey string) (HuntingRuntime, error) {
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return HuntingRuntime{}, err
	}
	if external {
		return x.externalHuntingRuntime(ctx, importerKey)
	}
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

func (x *XXMI) GetLibsReleases(ctx context.Context) ([]ReleaseInfo, error) {
	return x.ListReleases(ctx, "xxmi-libs")
}

func (x *XXMI) GetImporterReleases(ctx context.Context, importer string) ([]ReleaseInfo, error) {
	spec, ok := lookupImporterPackage(importer)
	if !ok {
		return nil, errors.New("unknown importer")
	}
	return x.ListReleases(ctx, "importer:"+spec.key)
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

func (x *XXMI) reportCleanup(err error, operation string) {
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return
	}
	_ = infra.ReportError(x.log, err, "XXMI", infra.Diagnostic{Operation: operation, Stage: "cleanup"})
}
