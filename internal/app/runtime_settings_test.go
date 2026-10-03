package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/xxmi"
)

func TestNamespaceIsolationSettingPreservesPendingToggleSave(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	root := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(root, "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = settings.Close() })
	if err := settings.SetPersistToggles(ctx, true); err != nil {
		t.Fatal(err)
	}
	importers := xxmi.New()
	importers.UseClient(settings.Client())
	if err := importers.SetLauncherMode(ctx, xxmi.LauncherBuiltin); err != nil {
		t.Fatal(err)
	}
	cfg, err := xxmi.DefaultImporterConfig("GIMI", root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	iniPath := filepath.Join(cfg.ImporterFolder, "Mods", "Character", "Only", "main.ini")
	if err := os.MkdirAll(filepath.Dir(iniPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		iniPath,
		[]byte("namespace = Unique\n[Constants]\nglobal persist $Toggle = 0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	d3dxPath := filepath.Join(cfg.ImporterFolder, "d3dx_user.ini")
	if err := os.WriteFile(d3dxPath, []byte("[Constants]\n$\\Unique\\Toggle = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := importers.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	mods := mod.NewWithOptions(mod.Options{Settings: settings, XXMI: importers})
	mods.UseClient(settings.Client())
	t.Cleanup(func() { _ = mods.ServiceShutdown() })
	key := "GIMI"
	if err := mods.AddGame(ctx, "Game", filepath.Join(cfg.ImporterFolder, "Mods"), &key, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	saved := make(chan struct{}, 1)
	toolService := tools.NewWithOptions(tools.Options{
		Settings: settings, XXMI: importers,
		EventEmit: func(name string, data ...any) {
			if name != "setting:xxmi:persistLogs" || len(data) != 1 {
				return
			}
			logs, ok := data[0].([]string)
			if ok && strings.Contains(strings.Join(logs, "\n"), "Updated persist variable $Toggle") {
				select {
				case saved <- struct{}{}:
				default:
				}
			}
		},
	})
	t.Cleanup(func() { _ = toolService.ServiceShutdown() })
	mods.UseNamespaceIsolationHooks(mod.NamespaceIsolationHooks{
		CheckStopped:   func(context.Context, string) error { return nil },
		WithMutation:   func(_ context.Context, _ string, change func() error) error { return change() },
		SuspendPersist: toolService.SuspendPersistWatcher,
	})
	settings.UseHooks(runtimeSettingHooks(nil, nil, nil, toolService, nil, nil, nil, nil, nil, mods))
	if err := toolService.StartPersistWatcher(ctx); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(d3dxPath, []byte("[Constants]\n$\\Unique\\Toggle = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		if err := settings.SetNamespaceIsolation(ctx, enabled); err != nil {
			t.Fatal(err)
		}
	}
	logs := strings.Join(toolService.GetPersistLogs(), "\n")
	if strings.Contains(logs, "Stopped persist watcher") || strings.Count(logs, "Started watching") != 1 {
		t.Fatalf("isolation setting restarted persistence: %s", logs)
	}
	select {
	case <-saved:
	case <-time.After(10 * time.Second):
		t.Fatalf("pending toggle was not saved: %v", toolService.GetPersistLogs())
	}
	content, err := os.ReadFile(iniPath)
	if err != nil || !strings.Contains(string(content), "$Toggle = 1") {
		t.Fatalf("saved INI = %q, %v", content, err)
	}
}
