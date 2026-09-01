package app

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
)

func TestRuntimeSettingHooksEmitLanguageAndSettingUpdate(t *testing.T) {
	t.Parallel()

	type event struct {
		name string
		data any
	}
	var got []event
	hooks := runtimeSettingHooks(nil, nil, nil, nil, nil, nil, func(name string, data ...any) {
		payload := any(nil)
		if len(data) == 1 {
			payload = data[0]
		} else if len(data) > 1 {
			payload = data
		}
		got = append(got, event{name: name, data: payload})
	})
	hooks.AfterLanguageChanged("ko")
	hooks.AfterSet(setting.KeyGeneralLanguage, "ko")
	hooks.AfterRendererReload()

	if len(got) != 3 {
		t.Fatalf("events = %#v, want 3", got)
	}
	if got[0].name != "language:update" || got[0].data != "ko" {
		t.Fatalf("language event = %#v", got[0])
	}
	update, ok := got[1].data.(map[string]any)
	if !ok || got[1].name != "setting:update" || update["key"] != setting.KeyGeneralLanguage || update["value"] != "ko" {
		t.Fatalf("setting event = %#v", got[1])
	}
	if got[2].name != "renderer:reload" || got[2].data != nil {
		t.Fatalf("reload event = %#v", got[2])
	}
}

func TestRuntimeSettingHooksApplyOpenConsoleToWindow(t *testing.T) {
	window := NewWindow()
	hooks := runtimeSettingHooks(nil, nil, nil, nil, window, nil, nil)

	hooks.AfterOpenConsoleChanged(true)
	window.mu.Lock()
	got := window.consoleOpen
	window.mu.Unlock()
	if !got {
		t.Fatal("openConsole hook did not enable the window console state")
	}
}

func TestRuntimeRegistersMenuMakerService(t *testing.T) {
	rt := newRuntime()
	if rt.menuMaker == nil {
		t.Fatal("Menu Maker service is not initialized")
	}
	if len(rt.services()) != 17 {
		t.Fatalf("services = %d, want 17 including Menu Maker", len(rt.services()))
	}
}

func TestRuntimeInitOpensAndSeedsLanguage(t *testing.T) {
	t.Parallel()

	rt := newRuntime()
	path := filepath.Join(t.TempDir(), "data.db")
	ctx := context.Background()
	if err := rt.Init(ctx, path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = rt.Close() }()

	got, err := rt.setting.Get(ctx, setting.KeyGeneralLanguage)
	if err != nil {
		t.Fatalf("Get language: %v", err)
	}
	if _, ok := got.(string); !ok || got == "" {
		t.Fatalf("language = %#v", got)
	}
}

func TestBootRuntimeOpensDBAndFollowsLogLevel(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cwd := t.TempDir()
	var buf bytes.Buffer
	rt := newRuntime()
	rt.localHTTP = infra.NewLocalHTTPWithOptions(infra.LocalHTTPOptions{Address: "127.0.0.1:0"})
	rt.log.Configure(infra.LogOptions{Writer: &buf})

	ctx := context.Background()
	paths, err := bootRuntime(ctx, rt, runtimePathInput{
		HomeDir:  home,
		Cwd:      cwd,
		Packaged: false,
	})
	if err != nil {
		t.Fatalf("bootRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	wantDB := filepath.Join(cwd, "local.db")
	if paths.DB != wantDB {
		t.Fatalf("DB = %q, want unpackaged %q", paths.DB, wantDB)
	}
	wantRoot := filepath.Join(home, appdata.RootDirName)
	if paths.Root != wantRoot {
		t.Fatalf("Root = %q, want %q", paths.Root, wantRoot)
	}
	if info, err := os.Stat(paths.Root); err != nil || !info.IsDir() {
		t.Fatalf("app data root missing: %v", err)
	}
	if info, err := os.Stat(paths.Logs); err != nil || !info.IsDir() {
		t.Fatalf("Logs missing: %v", err)
	}
	if _, err := os.Stat(wantDB); err != nil {
		t.Fatalf("store file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Logs, "desktop.log")); !os.IsNotExist(err) {
		t.Fatalf("unpackaged boot must not create desktop.log: %v", err)
	}

	got, err := rt.setting.Get(ctx, setting.KeyGeneralLogLevel)
	if err != nil {
		t.Fatalf("Get logLevel: %v", err)
	}
	if got != "error" {
		t.Fatalf("Get logLevel = %#v, want error", got)
	}
	if rt.log.Level() != "error" {
		t.Fatalf("logger level = %q, want error", rt.log.Level())
	}

	// Re-attach dest after boot so we can assert filtering without a live app data file.
	dest := filepath.Join(t.TempDir(), "desktop.log")
	rt.log.Configure(infra.LogOptions{Dest: dest})
	if err := rt.setting.Set(ctx, setting.KeyGeneralLogLevel, "warn"); err != nil {
		t.Fatalf("Set logLevel: %v", err)
	}
	if rt.log.Level() != "warn" {
		t.Fatalf("after Set, logger level = %q, want warn", rt.log.Level())
	}

	rt.log.Debug("hidden", "Boot")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		data, _ := os.ReadFile(dest)
		if len(bytes.TrimSpace(data)) != 0 {
			t.Fatalf("Debug wrote %q", data)
		}
	}
	rt.log.Warn("visible", "Boot")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	line := string(bytes.TrimSpace(data))
	if strings.Contains(line, `"level"`) || strings.HasPrefix(line, "{") {
		t.Fatalf("wrote JSON: %q", line)
	}
	if !strings.Contains(line, " WARN ") || !strings.Contains(line, "[Boot] visible") {
		t.Fatalf("warn line = %q", line)
	}
}

func TestBootRuntimePackagedUsesAppDataWithoutLegacyMigration(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	legacyDB := filepath.Join(home, "AppData", "Roaming", "Nahida Desktop", appdata.DatabaseFile)
	if err := os.MkdirAll(filepath.Dir(legacyDB), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte("legacy database must remain untouched")
	if err := os.WriteFile(legacyDB, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	rt := newRuntime()
	rt.localHTTP = infra.NewLocalHTTPWithOptions(infra.LocalHTTPOptions{Address: "127.0.0.1:0"})
	paths, err := bootRuntime(context.Background(), rt, runtimePathInput{
		HomeDir:  home,
		Cwd:      t.TempDir(),
		Packaged: true,
	})
	if err != nil {
		t.Fatalf("bootRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	wantRoot := filepath.Join(home, appdata.RootDirName)
	wantDB := filepath.Join(wantRoot, appdata.DatabaseFile)
	if paths.Root != wantRoot || paths.DB != wantDB || paths.Logs != filepath.Join(wantRoot, appdata.LogsDir) {
		t.Fatalf("paths = %#v, want root %q and DB %q", paths, wantRoot, wantDB)
	}
	if _, err := os.Stat(wantDB); err != nil {
		t.Fatalf("new packaged DB missing: %v", err)
	}
	gotLegacy, err := os.ReadFile(legacyDB)
	if err != nil || !bytes.Equal(gotLegacy, legacy) {
		t.Fatalf("legacy DB changed: %q, %v", gotLegacy, err)
	}
}

func TestBootRuntimeDBOverrideWinsInDevelopment(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	override := filepath.Join(t.TempDir(), "override.db")
	rt := newRuntime()
	rt.localHTTP = infra.NewLocalHTTPWithOptions(infra.LocalHTTPOptions{Address: "127.0.0.1:0"})
	paths, err := bootRuntime(context.Background(), rt, runtimePathInput{
		HomeDir: t.TempDir(), DBOverride: override, Cwd: cwd, Packaged: false,
	})
	if err != nil {
		t.Fatalf("bootRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()
	if paths.DB != override {
		t.Fatalf("DB = %q, want override %q", paths.DB, override)
	}
	if _, err := os.Stat(override); err != nil {
		t.Fatalf("override DB missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "local.db")); !os.IsNotExist(err) {
		t.Fatalf("local.db should not be opened when overridden: %v", err)
	}
}

func TestBootRuntimeFailsBeforeInjectionWhenHomeIsNotDirectory(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "home-file")
	if err := os.WriteFile(home, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := newRuntime()
	_, err := bootRuntime(context.Background(), rt, runtimePathInput{
		HomeDir: home, Cwd: t.TempDir(), Packaged: false,
	})
	if err == nil {
		t.Fatal("bootRuntime unexpectedly succeeded")
	}
	if rt.appData != nil {
		t.Fatal("failed app data open was injected into runtime")
	}
}

func TestBootRuntimeDevLogsIgnoreLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	rt := newRuntime()
	rt.localHTTP = infra.NewLocalHTTPWithOptions(infra.LocalHTTPOptions{Address: "127.0.0.1:0"})

	ctx := context.Background()
	if _, err := bootRuntime(ctx, rt, runtimePathInput{
		HomeDir:  t.TempDir(),
		Cwd:      t.TempDir(),
		Packaged: false,
	}); err != nil {
		t.Fatalf("bootRuntime: %v", err)
	}
	defer func() { _ = rt.Close() }()

	rt.log.Configure(infra.LogOptions{Writer: &buf, Dev: true})
	if rt.log.Level() != "error" {
		t.Fatalf("logger level = %q, want error", rt.log.Level())
	}
	rt.log.Info("Starting model viewer load", "StaticGlb.loadForViewer")
	if !strings.Contains(buf.String(), "[StaticGlb.loadForViewer] Starting model viewer load") {
		t.Fatalf("dev console = %q", buf.String())
	}
}

func TestBootRuntimeFailsWhenLocalHTTPCannotBind(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()

	rt := newRuntime()
	rt.localHTTP = infra.NewLocalHTTPWithOptions(infra.LocalHTTPOptions{Address: listener.Addr().String()})
	defer func() { _ = rt.Close() }()
	_, err = bootRuntime(context.Background(), rt, runtimePathInput{
		HomeDir:  t.TempDir(),
		Cwd:      t.TempDir(),
		Packaged: false,
	})
	if err == nil || !strings.Contains(err.Error(), "listen local HTTP bridge") {
		t.Fatalf("bootRuntime error = %v", err)
	}
}

func TestRuntimeInitWiresAuthTokenStore(t *testing.T) {
	t.Parallel()

	rt := newRuntime()
	path := filepath.Join(t.TempDir(), "data.db")
	ctx := context.Background()
	if err := rt.Init(ctx, path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = rt.Close() }()

	enc, err := platform.NewCrypto().EncryptString("wired-token")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := rt.store.DB.Settings.Upsert(ctx, "token", &enc); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ok, err := rt.auth.HasToken(ctx)
	if err != nil || !ok {
		t.Fatalf("HasToken = %v, %v", ok, err)
	}
}
