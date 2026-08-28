package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

const (
	xxmiPathKey         = "xxmi.path"
	xxmiConfigName      = "XXMI Launcher Config.json"
	releaseCacheTimeout = time.Minute
)

type Options struct {
	HTTP        *infra.Client
	Log         *infra.Log
	Download    *infra.Download
	Archive     *infra.Archive
	EventEmit   func(name string, data ...any)
	SearchRoots func() ([]string, error)
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
	Key            string      `json:"key"`
	ImporterFolder string      `json:"importerFolder"`
	PackageInfo    PackageInfo `json:"packageInfo"`
}

type Data struct {
	XXMIPath         *string           `json:"xxmiPath"`
	DLLVersion       *string           `json:"dllVersion"`
	EnabledImporters []EnabledImporter `json:"enabledImporters"`
	XXMIConfig       map[string]any    `json:"xxmiConfig"`
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
			ImporterFolder string   `json:"importer_folder"`
		} `json:"Importer"`
	} `json:"Importers"`
	Security map[string]any `json:"Security"`
}

type XXMI struct {
	mu            sync.RWMutex
	client        *db.Client
	http          *infra.Client
	log           *infra.Log
	download      *infra.Download
	archive       *infra.Archive
	eventEmit     func(string, ...any)
	searchRoots   func() ([]string, error)
	path          *string
	config        map[string]any
	parsed        parsedConfig
	busy          bool
	releases      []string
	releasesReady bool
	fetched       time.Time
	releaseCall   *releaseFetchCall
}

type releaseFetchCall struct {
	done chan struct{}
	err  error
}

func New() *XXMI {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *XXMI {
	searchRoots := opts.SearchRoots
	if searchRoots == nil {
		searchRoots = xxmiSearchRoots
	}
	return &XXMI{
		http: opts.HTTP, log: opts.Log, download: opts.Download, archive: opts.Archive,
		eventEmit: opts.EventEmit, searchRoots: searchRoots,
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
			return errors.New("XXMI Launcher Config.json not found")
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
	result, err := findFileAcrossRoots(ctx, roots, xxmiConfigName, map[string]struct{}{"Backups": {}})
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
	if err := x.load(ctx); err != nil {
		return Data{}, err
	}
	x.mu.RLock()
	path := cloneString(x.path)
	config := cloneMap(x.config)
	enabled := x.enabledImportersLocked()
	x.mu.RUnlock()
	return Data{XXMIPath: path, DLLVersion: dllVersion(path), EnabledImporters: enabled, XXMIConfig: config}, nil
}

func (x *XXMI) GetEnabledImporters(ctx context.Context) ([]EnabledImporter, error) {
	if err := x.load(ctx); err != nil {
		return nil, err
	}
	x.mu.RLock()
	out := x.enabledImportersLocked()
	x.mu.RUnlock()
	return out, nil
}

func (x *XXMI) GetLibsReleases(ctx context.Context) ([]string, error) {
	return x.getLibsReleases(ctx, false)
}

func (x *XXMI) UpdateLibsReleases(ctx context.Context) error {
	_, err := x.getLibsReleases(ctx, true)
	return err
}

func (x *XXMI) getLibsReleases(ctx context.Context, refresh bool) ([]string, error) {
	x.mu.Lock()
	if x.releasesReady && (!refresh || time.Since(x.fetched) < releaseCacheTimeout) {
		cached := slices.Clone(x.releases)
		x.mu.Unlock()
		return cached, nil
	}
	if x.releaseCall != nil {
		call := x.releaseCall
		x.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
		}
		x.mu.RLock()
		cached := slices.Clone(x.releases)
		x.mu.RUnlock()
		return cached, call.err
	}
	httpClient := x.http
	if httpClient == nil {
		x.mu.Unlock()
		return nil, errors.New("XXMI HTTP client is not configured")
	}
	call := &releaseFetchCall{done: make(chan struct{})}
	x.releaseCall = call
	x.mu.Unlock()

	rawURL := "https://api.github.com/repos/SpectrumQT/XXMI-Libs-Package/releases"
	header := make(http.Header)
	header.Set("Accept", "application/vnd.github+json")
	header.Set("X-GitHub-Api-Version", "2026-03-10")
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	response, err := httpClient.Fetch(ctx, rawURL, infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true})
	if err != nil {
		return x.finishReleaseFetch(call, nil, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return x.finishReleaseFetch(call, nil, fmt.Errorf("failed to fetch XXMI libs releases: %s", response.Status))
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&releases); err != nil {
		return x.finishReleaseFetch(call, nil, err)
	}
	out := make([]string, 0, len(releases))
	for _, release := range releases {
		tag := strings.TrimSpace(release.TagName)
		if tag == "" || strings.EqualFold(tag, "main") || strings.EqualFold(tag, "master") {
			continue
		}
		out = append(out, tag)
	}
	return x.finishReleaseFetch(call, out, nil)
}

func (x *XXMI) finishReleaseFetch(call *releaseFetchCall, releases []string, err error) ([]string, error) {
	x.mu.Lock()
	if err == nil {
		x.releases = slices.Clone(releases)
		x.releasesReady = true
		x.fetched = time.Now()
	}
	call.err = err
	if x.releaseCall == call {
		x.releaseCall = nil
	}
	close(call.done)
	cached := slices.Clone(x.releases)
	x.mu.Unlock()
	return cached, err
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
			x.log.Error(fmt.Sprintf("Failed to initialize XXMI: %v", err), "XXMI.initialize")
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

func (x *XXMI) enabledImportersLocked() []EnabledImporter {
	out := make([]EnabledImporter, 0, len(x.parsed.Importers))
	keys := make([]string, 0, len(x.parsed.Importers))
	for key := range x.parsed.Importers {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		packageInfo, ok := x.parsed.Packages.Packages[key]
		if !ok || strings.TrimSpace(packageInfo.LatestVersion) == "" {
			continue
		}
		folder := x.parsed.Importers[key].Importer.ImporterFolder
		if !filepath.IsAbs(folder) && x.path != nil {
			folder = filepath.Join(*x.path, folder)
		}
		out = append(out, EnabledImporter{Key: key, ImporterFolder: folder, PackageInfo: packageInfo})
	}
	return out
}

func readAndValidateConfig(path string) (map[string]any, parsedConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, parsedConfig{}, err
	}
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
