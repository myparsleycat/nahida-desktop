// Package reshade installs the ReShade add-on build, its effect packages, and the per-game folders
// the launcher injects it from.
package reshade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

const (
	cacheDir   = "xxmi/packages/reshade"
	versionKey = "reshade_version"
	moduleName = "ReShade64.dll"
	// A module with this extension keeps its settings beside itself instead of beside the game executable.
	gameModuleName = "ReShade64.asi"
)

var importerKeyRE = regexp.MustCompile(`^[A-Z0-9]{2,16}$`)

type Options struct {
	GitHub    *github.Client
	Download  *infra.Download
	Archive   *infra.Archive
	Transfer  *transfer.Transfer
	Log       *infra.Log
	EventEmit func(name string, data ...any)
	// Root returns the folder the user keeps XXMI data in; ReShade's own data lives in a folder below it.
	Root func(context.Context) (string, error)
}

type ReShade struct {
	github    *github.Client
	download  *infra.Download
	archive   *infra.Archive
	transfer  *transfer.Transfer
	log       *infra.Log
	eventEmit func(string, ...any)
	root      func(context.Context) (string, error)

	mu      sync.RWMutex
	client  *db.Client
	appData *appdata.Store

	// binaryMu serializes downloads into the version cache and copies out of it.
	binaryMu sync.Mutex
	// effectsMu serializes changes to the shared effect folders and their record.
	effectsMu sync.Mutex

	// fileVersion reads a module's version resource; tests replace it because they cannot build one.
	fileVersion func(path string) (string, error)
}

func New(opts Options) *ReShade {
	return &ReShade{
		github: opts.GitHub, download: opts.Download, archive: opts.Archive, transfer: opts.Transfer, log: opts.Log,
		eventEmit: opts.EventEmit, root: opts.Root, fileVersion: moduleFileVersion,
	}
}

//wails:ignore
func (r *ReShade) UseClient(client *db.Client) {
	r.mu.Lock()
	r.client = client
	r.mu.Unlock()
}

//wails:ignore
func (r *ReShade) UseAppData(data *appdata.Store) {
	r.mu.Lock()
	r.appData = data
	r.mu.Unlock()
}

type Status struct {
	// Pinned is the version the user holds ReShade to; empty follows the latest one.
	Pinned    string   `json:"pinned"`
	Latest    string   `json:"latest"`
	Installed []string `json:"installed"`
	// Active is the installed version the next launch injects.
	Active          string `json:"active"`
	UpdateAvailable bool   `json:"updateAvailable"`
}

func (r *ReShade) Status(ctx context.Context) (Status, error) {
	pinned, err := r.pinnedVersion(ctx)
	if err != nil {
		return Status{}, r.report(err, "Status", "read-setting", nil)
	}
	installed, err := r.installedVersions()
	if err != nil {
		return Status{}, r.report(err, "Status", "read-cache", nil)
	}
	status := Status{Pinned: pinned, Installed: installed}

	// The version list comes from the network, and its absence must not hide what is installed.
	if versions, err := r.versions(ctx, false); err == nil && len(versions) > 0 {
		status.Latest = versions[0]
	}
	switch {
	case pinned != "":
		if containsVersion(installed, pinned) {
			status.Active = pinned
		}
	case len(installed) > 0:
		status.Active = installed[0]
	}
	status.UpdateAvailable = pinned == "" && status.Latest != "" && status.Latest != status.Active
	return status, nil
}

func (r *ReShade) Versions(ctx context.Context, refresh bool) ([]string, error) {
	versions, err := r.versions(ctx, refresh)
	if err != nil {
		return nil, r.report(err, "Versions", "tags", map[string]any{"refresh": refresh})
	}
	return versions, nil
}

// SetVersion pins ReShade to version after installing it; an empty version follows the latest one.
func (r *ReShade) SetVersion(ctx context.Context, version string) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version != "" {
		if !supportedVersion(version) {
			return errors.New("RESHADE_VERSION_UNSUPPORTED")
		}
		if _, err := r.ensureVersion(ctx, version); err != nil {
			return r.report(err, "SetVersion", "install", map[string]any{"version": version})
		}
	}
	client, err := r.settings()
	if err != nil {
		return err
	}
	if err := client.Settings.Upsert(ctx, versionKey, &version); err != nil {
		return r.report(err, "SetVersion", "write-setting", map[string]any{"version": version})
	}
	r.emitStatus()
	return nil
}

// Install downloads the version the next launch uses: the pinned one, or the latest.
func (r *ReShade) Install(ctx context.Context) (string, error) {
	version, err := r.targetVersion(ctx, true)
	if err != nil {
		return "", r.report(err, "Install", "resolve-version", nil)
	}
	if _, err := r.ensureVersion(ctx, version); err != nil {
		return "", r.report(err, "Install", "install", map[string]any{"version": version})
	}
	r.emitStatus()
	return version, nil
}

type Paths struct {
	Root     string `json:"root"`
	Shaders  string `json:"shaders"`
	Textures string `json:"textures"`
	Addons   string `json:"addons"`
	Presets  string `json:"presets"`
	// Game is the importer's own folder, which holds its ReShade.ini, presets, and log.
	Game string `json:"game"`
}

// Paths returns the folders ReShade reads, creating them so they can be opened. An empty importer leaves Game empty.
func (r *ReShade) Paths(ctx context.Context, importer string) (Paths, error) {
	layout, err := r.layout(ctx)
	if err != nil {
		return Paths{}, r.report(err, "Paths", "resolve-root", map[string]any{"importer": importer})
	}
	if err := layout.ensureShared(); err != nil {
		return Paths{}, r.report(err, "Paths", "create-folders", map[string]any{"root": layout.root})
	}
	paths := Paths{
		Root: layout.root, Shaders: layout.shaders(), Textures: layout.textures(),
		Addons: layout.addons(), Presets: layout.presets(),
	}
	if importer == "" {
		return paths, nil
	}
	game, err := layout.game(importer)
	if err != nil {
		return Paths{}, err
	}
	if err := os.MkdirAll(game, 0o700); err != nil {
		return Paths{}, r.report(err, "Paths", "create-folders", map[string]any{"path": game})
	}
	paths.Game = game
	return paths, nil
}

type layout struct{ root string }

func (r *ReShade) layout(ctx context.Context) (layout, error) {
	if r.root == nil {
		return layout{}, errors.New("ReShade data folder is not configured")
	}
	root, err := r.root(ctx)
	if err != nil {
		return layout{}, err
	}
	if root == "" || !filepath.IsAbs(root) {
		return layout{}, errors.New("ReShade data folder is not configured")
	}
	return layout{root: filepath.Join(root, "ReShade")}, nil
}

func (l layout) effects() string  { return filepath.Join(l.root, "reshade-shaders") }
func (l layout) shaders() string  { return filepath.Join(l.effects(), "Shaders") }
func (l layout) textures() string { return filepath.Join(l.effects(), "Textures") }
func (l layout) addons() string   { return filepath.Join(l.effects(), "Addons") }
func (l layout) presets() string  { return filepath.Join(l.effects(), "Presets") }
func (l layout) record() string   { return filepath.Join(l.root, "packages.json") }

func (l layout) game(importer string) (string, error) {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	if !importerKeyRE.MatchString(importer) {
		return "", errors.New("invalid importer")
	}
	return filepath.Join(l.root, importer), nil
}

func (l layout) ensureShared() error {
	for _, dir := range []string{l.shaders(), l.textures(), l.addons(), l.presets()} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReShade) settings() (*db.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.client == nil {
		return nil, errors.New("ReShade settings store is not configured")
	}
	return r.client, nil
}

func (r *ReShade) cacheRoot() (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.appData == nil {
		return "", errors.New("ReShade cache folder is not configured")
	}
	return r.appData.Resolve(cacheDir)
}

func (r *ReShade) report(err error, action, stage string, fields map[string]any) error {
	return infra.ReportError(r.log, err, "ReShade."+action, infra.Diagnostic{
		Operation: "reshade", Stage: stage, Fields: fields,
	})
}

func (r *ReShade) emitStatus() {
	if r.eventEmit != nil {
		r.eventEmit("reshade:status")
	}
}

func (r *ReShade) emitProgress(kind, name, stage string, downloaded, total int64) {
	if r.eventEmit != nil {
		r.eventEmit("reshade:progress", map[string]any{
			"kind": kind, "name": name, "stage": stage, "downloaded": downloaded, "total": total,
		})
	}
}
