package xxmi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSavePathLoadsConfigManifestAndEnabledImporters(t *testing.T) {
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	manifestDir := filepath.Join(root, "Resources", "Packages", "XXMI")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestDir, "Manifest.json"), []byte(`{"version":"v1.2.3"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := service.SaveXXMIPath(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	data, err := service.GetXXMIData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data.XXMIPath == nil || *data.XXMIPath != root || data.DLLVersion == nil || *data.DLLVersion != "v1.2.3" {
		t.Fatalf("data = %+v", data)
	}
	if len(data.EnabledImporters) != 1 || data.EnabledImporters[0].Key != "GIMI" || data.EnabledImporters[0].ImporterFolder != filepath.Join(root, "GIMI") {
		t.Fatalf("enabled importers = %+v", data.EnabledImporters)
	}
}

func TestSavePathEmitsRendererReloadAfterStateUpdate(t *testing.T) {
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	var events []string
	service := NewWithOptions(Options{EventEmit: func(name string, _ ...any) {
		events = append(events, name)
	}})
	service.UseClient(client)
	if err := service.SaveXXMIPath(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0] != "renderer:reload" {
		t.Fatalf("events = %v", events)
	}
	data, err := service.GetXXMIData(context.Background())
	if err != nil || data.XXMIPath == nil || *data.XXMIPath != root {
		t.Fatalf("data = %+v, err=%v", data, err)
	}
}

func TestFindXXMIPathPrefersValidAppDataCandidate(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	candidate := filepath.Join(appData, "XXMI Launcher")
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	writeXXMITestConfig(t, candidate)
	searchCalled := false
	service := NewWithOptions(Options{SearchRoots: func() ([]string, error) {
		searchCalled = true
		return nil, nil
	}})
	result, err := service.FindXXMIPath(context.Background())
	if err != nil || result == nil || *result != candidate {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if searchCalled {
		t.Fatal("drive scan ran despite valid APPDATA candidate")
	}
}

func TestFindXXMIPathScansRootsAndExcludesBackups(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	root := t.TempDir()
	backup := filepath.Join(root, "Backups", "Old")
	wanted := filepath.Join(root, "Games", "XXMI Launcher")
	for _, directory := range []string{backup, wanted} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, xxmiConfigName), []byte("found"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	service := NewWithOptions(Options{SearchRoots: func() ([]string, error) {
		return []string{root}, nil
	}})
	result, err := service.FindXXMIPath(context.Background())
	if err != nil || result == nil || *result != wanted {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestFindFileAcrossRootsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := findFileAcrossRoots(ctx, []string{t.TempDir()}, xxmiConfigName, nil)
	if !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("result=%v err=%v", result, err)
	}
}

func TestSavePathRejectsInvalidConfig(t *testing.T) {
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, xxmiConfigName), []byte(`{"Launcher":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := service.SaveXXMIPath(context.Background(), root); err == nil {
		t.Fatal("expected invalid config error")
	}
}

func TestLoadTreatsCorruptSavedConfigAsUnconfigured(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	service := New()
	service.UseClient(client)
	if err := service.SaveXXMIPath(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, xxmiConfigName), []byte(`{"Launcher":`), 0o644); err != nil {
		t.Fatal(err)
	}

	data, err := service.GetXXMIData(ctx)
	if err != nil {
		t.Fatalf("GetXXMIData returned corrupt-config error: %v", err)
	}
	if data.XXMIPath == nil || *data.XXMIPath != root || data.XXMIConfig != nil || len(data.EnabledImporters) != 0 {
		t.Fatalf("data after corrupt config = %#v", data)
	}
	config, err := service.GetXXMIConfig(ctx)
	if err != nil || config != nil {
		t.Fatalf("GetXXMIConfig = %#v, %v; want nil, nil", config, err)
	}
}

func TestValidateXXMIConfigMatchesRequiredElectronFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{
			name: "launcher start timeout",
			mutate: func(config map[string]any) {
				delete(config["Launcher"].(map[string]any), "start_timeout")
			},
		},
		{
			name: "security user signature",
			mutate: func(config map[string]any) {
				delete(config["Security"].(map[string]any), "user_signature")
			},
		},
		{
			name: "all standard importers",
			mutate: func(config map[string]any) {
				delete(config["Importers"].(map[string]any), "HIMI")
			},
		},
		{
			name: "base importer field type",
			mutate: func(config map[string]any) {
				gimi := config["Importers"].(map[string]any)["GIMI"].(map[string]any)
				gimi["Importer"].(map[string]any)["package_name"] = false
			},
		},
		{
			name: "WWMI system setting scalar",
			mutate: func(config map[string]any) {
				wwmi := config["Importers"].(map[string]any)["WWMI"].(map[string]any)
				perf := wwmi["Importer"].(map[string]any)["perf_tweaks"].(map[string]any)
				perf["SystemSettings"] = map[string]any{"invalid": []string{"array"}}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := xxmiTestConfig()
			test.mutate(config)
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if err := validateXXMIConfig(decoded); err == nil {
				t.Fatal("invalid config was accepted")
			}
		})
	}
}

func TestSavePathAcceptsEmptyImporterFolderAndResolvesItToRoot(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	config := xxmiTestConfig()
	gimi := config["Importers"].(map[string]any)["GIMI"].(map[string]any)
	gimi["Importer"].(map[string]any)["importer_folder"] = ""
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, xxmiConfigName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := service.SaveXXMIPath(ctx, root); err != nil {
		t.Fatal(err)
	}
	data, err := service.GetXXMIData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.EnabledImporters) != 1 || data.EnabledImporters[0].ImporterFolder != root {
		t.Fatalf("enabled importers = %#v, want GIMI rooted at %q", data.EnabledImporters, root)
	}
}

func TestGetLibsReleasesUsesCurrentGitHubHeadersAndCaches(t *testing.T) {
	var requests atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.Path != "/repos/SpectrumQT/XXMI-Libs-Package/releases" || request.URL.RawQuery != "" {
			t.Fatalf("URL = %s", request.URL)
		}
		if request.Header.Get("Accept") != "application/vnd.github+json" || request.Header.Get("X-GitHub-Api-Version") != "2026-03-10" {
			t.Fatalf("headers = %v", request.Header)
		}
		if !strings.Contains(request.Header.Get("User-Agent"), "Chrome/138.0.0.0") {
			t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`[{"tag_name":"v2"},{"tag_name":"main"},{"tag_name":"v1"}]`)),
			Request:    request,
		}, nil
	})}
	service := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})})
	first, err := service.GetLibsReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.GetLibsReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(first, ",") != "v2,v1" || strings.Join(second, ",") != "v2,v1" || requests.Load() != 1 {
		t.Fatalf("first = %v, second = %v, requests = %d", first, second, requests.Load())
	}
	service.mu.Lock()
	service.fetched = time.Now().Add(-2 * releaseCacheTimeout)
	service.mu.Unlock()
	if _, err := service.GetLibsReleases(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("process cache refetched after cooldown: %d", requests.Load())
	}
	if err := service.UpdateLibsReleases(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("explicit refresh requests = %d, want 2", requests.Load())
	}
}

func TestGetLibsReleasesDeduplicatesInitialInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`[{"tag_name":"v1"}]`)), Request: request,
		}, nil
	})}
	service := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})})
	results := make(chan error, 2)
	go func() { _, err := service.GetLibsReleases(context.Background()); results <- err }()
	<-started
	go func() { _, err := service.GetLibsReleases(context.Background()); results <- err }()
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestInstallDLLVersionStagesAndValidatesBeforeCopy(t *testing.T) {
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("package/d3d11.dll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("dll")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := []byte(`{"version":"v1.2.3"}`)
		if strings.HasSuffix(request.URL.Path, ".zip") {
			body = archive.Bytes()
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(infraClient)
	service := NewWithOptions(Options{HTTP: infraClient, Download: download, Archive: infra.NewArchive()})
	service.UseClient(client)
	if err := service.SaveXXMIPath(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := service.InstallDLLVersion(context.Background(), InstallDLLVersionInput{Version: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(filepath.Join(root, "Resources", "Packages", "XXMI", "d3d11.dll"))
	if err != nil || string(installed) != "dll" {
		t.Fatalf("installed = %q, error = %v", installed, err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, "Resources", "Packages", "XXMI", "Manifest.json"))
	if err != nil || !bytes.Contains(manifest, []byte("v1.2.3")) {
		t.Fatalf("manifest = %q, error = %v", manifest, err)
	}
	rawConfig, err := os.ReadFile(filepath.Join(root, xxmiConfigName))
	if err != nil || !bytes.Contains(rawConfig, []byte(`"auto_update": false`)) {
		t.Fatalf("config = %q, error = %v", rawConfig, err)
	}
}

func writeXXMITestConfig(t *testing.T, root string) {
	t.Helper()
	raw, err := json.MarshalIndent(xxmiTestConfig(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, xxmiConfigName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func xxmiTestConfig() map[string]any {
	importers := map[string]any{}
	for _, name := range []string{"GIMI", "SRMI", "WWMI", "ZZMI", "EFMI", "HIMI"} {
		importer := xxmiTestBaseImporter(name)
		switch name {
		case "GIMI":
			importer["unlock_fps"] = false
			importer["disable_dcr"] = false
			importer["enable_hdr"] = false
		case "WWMI":
			importer["apply_perf_tweaks"] = false
			importer["perf_tweaks"] = map[string]any{"SystemSettings": map[string]any{}}
			importer["mesh_lod_distance_scale"] = 1
			importer["mesh_lod_distance_offset"] = 0
			importer["texture_streaming_boost"] = 1
			importer["texture_streaming_min_boost"] = 0
			importer["texture_streaming_use_all_mips"] = false
			importer["texture_streaming_pool_size"] = 0
			importer["texture_streaming_limit_to_vram"] = false
			importer["texture_streaming_fixed_pool_size"] = false
		case "HIMI":
			importer["unlock_fps"] = false
			importer["unlock_fps_value"] = 60
			importer["disable_dcr"] = false
			importer["enable_hdr"] = false
		}
		importers[name] = map[string]any{
			"Importer": importer,
			"Migoto": map[string]any{
				"enforce_rendering": false, "enable_hunting": false, "dump_shaders": false,
				"mute_warnings": false, "calls_logging": false, "debug_logging": false,
				"unsafe_mode": false, "unsafe_mode_signature": "",
			},
		}
	}

	return map[string]any{
		"Launcher": map[string]any{
			"auto_update": false, "pre_release": false, "update_channel": "stable", "auto_close": false,
			"start_timeout": 60, "gui_theme": "default", "theme_mode": "system", "active_importer": "GIMI",
			"enabled_importers": []string{"GIMI"}, "log_level": "INFO", "config_version": "1",
			"theme_dev_mode": false, "github_token": "", "verify_ssl": true, "credits_shown": true, "locale": "en_US",
			"proxy": map[string]any{
				"enable": false, "type": "http", "host": "", "port": "", "use_credentials": false,
				"user": "", "password": "", "proxy_dns_via_socks5": false,
			},
		},
		"Packages": map[string]any{"packages": map[string]any{
			"GIMI": xxmiTestPackage("v1"),
			"SRMI": xxmiTestPackage(""),
		}},
		"Importers": importers,
		"Security":  map[string]any{"user_signature": ""},
	}
}

func xxmiTestPackage(version string) map[string]any {
	return map[string]any{
		"latest_version": version, "skipped_version": "", "deployed_version": version,
		"update_check_time": 0, "latest_release_notes": "", "deployed_release_notes": "",
	}
}

func xxmiTestBaseImporter(name string) map[string]any {
	return map[string]any{
		"game_exe_names": []string{name + ".exe"}, "game_folder_names": []string{},
		"game_folder_children": []string{}, "package_name": name, "importer_folder": name,
		"game_folder": "", "use_launch_options": false, "overwrite_ini": false,
		"process_start_method": "direct", "xxmi_dll_init_delay": 0, "process_priority": "Normal",
		"window_mode": "windowed", "run_pre_launch_enabled": false, "run_pre_launch": "",
		"run_pre_launch_signature": "", "run_pre_launch_wait": false, "custom_launch_enabled": false,
		"custom_launch": "", "custom_launch_signature": "", "custom_launch_inject_mode": "",
		"run_post_load_enabled": false, "run_post_load": "", "run_post_load_signature": "",
		"run_post_load_wait": false, "extra_libraries_enabled": false, "extra_libraries": "",
		"extra_libraries_signature": "", "deployed_migoto_signatures": map[string]string{},
		"shortcut_deployed": false, "configure_game": false, "launch_count": 0, "launch_options": "",
		"d3dx_ini": map[string]any{
			"core":              map[string]any{"Loader": map[string]any{"loader": "d3d11.dll"}},
			"enforce_rendering": map[string]any{"Rendering": map[string]any{"texture_hash": 0, "track_texture_updates": 0}},
			"calls_logging":     map[string]any{"Logging": map[string]any{"calls": map[string]any{"on": 1, "off": 0}}},
			"debug_logging":     map[string]any{"Logging": map[string]any{"debug": map[string]any{"on": 1, "off": 0}}},
			"mute_warnings":     map[string]any{"Logging": map[string]any{"show_warnings": map[string]any{"on": 1, "off": 0}}},
			"enable_hunting":    map[string]any{"Hunting": map[string]any{"hunting": map[string]any{"on": 1, "off": 0}}},
			"dump_shaders":      map[string]any{"Hunting": map[string]any{"marking_actions": map[string]any{"on": "mark", "off": "no_mark"}}},
		},
	}
}
