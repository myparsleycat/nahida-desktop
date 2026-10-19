package modruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type fakeGamePaths []db.GamePathRow

func (f fakeGamePaths) List(context.Context) ([]db.GamePathRow, error) { return f, nil }

type renamingMods struct{}

func (renamingMods) Disable(_ context.Context, path string) (string, error) {
	disabled := filepath.Join(filepath.Dir(path), "DISABLED "+filepath.Base(path))
	return disabled, os.Rename(path, disabled)
}

func (renamingMods) Enable(_ context.Context, path string) (string, error) {
	enabled := filepath.Join(filepath.Dir(path), strings.TrimPrefix(filepath.Base(path), "DISABLED "))
	return enabled, os.Rename(path, enabled)
}

const (
	seedFile     = "surface/Loader.ini"
	bundledProbe = "namespace = Probe\r\n[Constants]\r\nglobal $strength = 3\r\n[KeyToggle]\r\nkey = VK_F1\r\n"
)

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	modsRoot := t.TempDir()
	importer := "EFMI"
	manager := New(Options{
		GamePaths: fakeGamePaths{{Game: "endfield", ModFolderPath: modsRoot, Importer: &importer}},
		Mod:       renamingMods{},
		Specs: []Spec{{
			Name:      "Probe",
			Version:   "2",
			Importers: []string{"EFMI"},
			Namespace: "Probe",
			Files: fstest.MapFS{
				"Probe.ini":      {Data: []byte(bundledProbe)},
				"shaders/a.hlsl": {Data: []byte("// a")},
			},
			Seed: func(string, string) (map[string][]byte, error) {
				return map[string][]byte{seedFile: []byte("; seeded")}, nil
			},
			Legacy: []Legacy{{Dir: "Probe/Probe", Manifest: ".legacy.json"}},
			Settings: []Setting{
				{
					ID: "toggleKey", Kind: SettingKey, Default: "VK_F1",
					Targets: []Target{{File: "Probe.ini", Section: "KeyToggle", Name: "key"}},
				},
				{
					ID: "strength", Kind: SettingInt, Default: "3", Min: 0, Max: 7,
					Targets: []Target{{File: "Probe.ini", Section: "Constants", Name: "global $strength"}},
				},
			},
		}},
	})
	return manager, modsRoot
}

func (m *Manager) state(t *testing.T) Status {
	t.Helper()
	listing, err := m.List(context.Background(), "efmi")
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Runtimes) != 1 {
		t.Fatalf("listing = %+v", listing)
	}
	return listing.Runtimes[0]
}

func TestStates(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	ctx := context.Background()
	root := installRoot(modsRoot, "Probe")
	if got := manager.state(t).State; got != StateMissing {
		t.Fatalf("fresh Mods folder = %s", got)
	}

	// A first install cut off after Probe.ini must not pass for a foreign copy.
	failing := errors.New("disk full")
	manager.writeFile = func(name string, data []byte) error {
		if filepath.Base(name) == "a.hlsl" {
			return failing
		}
		return writeAtomic(name, data)
	}
	if _, err := manager.Install(ctx, "EFMI", "Probe"); !errors.Is(err, failing) {
		t.Fatalf("interrupted install = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Probe.ini")); err != nil {
		t.Fatalf("interrupted install wrote no runtime file: %v", err)
	}
	if got := manager.state(t).State; got != StateOutdated {
		t.Fatalf("after an interrupted install = %s", got)
	}
	manager.writeFile = writeAtomic
	if status, err := manager.Install(ctx, "EFMI", "Probe"); err != nil || status.State != StateCurrent {
		t.Fatalf("install = %+v (err=%v)", status, err)
	}

	// A repair restores runtime files and keeps what the feature owns.
	if err := os.WriteFile(
		filepath.Join(root, "Probe.ini"),
		[]byte("namespace = Probe\n; edited\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(seedFile)), []byte("; feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := manager.state(t).State; got != StateModified {
		t.Fatalf("after editing Probe.ini = %s", got)
	}
	if status, err := manager.Install(ctx, "EFMI", "Probe"); err != nil || status.State != StateCurrent {
		t.Fatalf("repair = %+v (err=%v)", status, err)
	}
	if seeded, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(seedFile))); string(seeded) != "; feature" {
		t.Fatalf("repair replaced the feature's file: %q", seeded)
	}

	if status, err := manager.Disable(ctx, "EFMI", "Probe"); err != nil || status.State != StateDisabled {
		t.Fatalf("disable = %+v (err=%v)", status, err)
	}
	if status, err := manager.Install(ctx, "EFMI", "Probe"); err != nil || status.State != StateCurrent ||
		!strings.EqualFold(status.Path, root) {
		t.Fatalf("install of a disabled runtime = %+v (err=%v)", status, err)
	}
}

// The Mods folder is often a junction, which filepath.WalkDir does not enter.
func TestForeignCopyBelowJunction(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	ctx := context.Background()
	link := filepath.Join(t.TempDir(), "LinkedMods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, modsRoot).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	importer := "EFMI"
	manager.games = fakeGamePaths{{Game: "endfield", ModFolderPath: link, Importer: &importer}}
	if _, err := manager.Install(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(modsRoot, "Effects", "Probe")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(foreign, "Probe.ini"),
		[]byte("\xef\xbb\xbfnamespace = Probe\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if status := manager.state(t); status.State != StateForeign {
		t.Fatalf("second copy below the junction = %+v", status)
	}
	if _, err := manager.Install(ctx, "EFMI", "Probe"); !hasCode(err, ErrorForeign) {
		t.Fatalf("install beside a second copy = %v", err)
	}
	// The other copy is not this app's to delete.
	if _, err := manager.Remove(ctx, "EFMI", "Probe"); !hasCode(err, ErrorForeign) {
		t.Fatalf("remove beside a second copy = %v", err)
	}
	if _, err := os.Stat(filepath.Join(foreign, "Probe.ini")); err != nil {
		t.Fatalf("second copy was touched: %v", err)
	}
}

// A runtime the user disabled at its old location stays disabled, with the
// data its feature kept there.
func TestMigratesDisabledLegacyInstall(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	legacy := filepath.Join(modsRoot, "Probe", "DISABLED_Probe")
	if err := os.MkdirAll(filepath.Join(legacy, "Profiles"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		".legacy.json":       `{"runtimeVersion":"1","files":{}}`,
		"Probe.ini":          "namespace = Probe\r\n",
		"Profiles/keep.json": "{}",
	} {
		if err := os.WriteFile(filepath.Join(legacy, filepath.FromSlash(name)), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	status := manager.state(t)
	moved := filepath.Join(modsRoot, GroupDir, "DISABLED_Probe")
	if status.State != StateDisabled || !strings.EqualFold(status.Path, moved) || status.Version != "1" {
		t.Fatalf("after moving the legacy install = %+v", status)
	}
	if _, err := os.Stat(filepath.Join(moved, "Profiles", "keep.json")); err != nil {
		t.Fatalf("feature data lost in the move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(modsRoot, "Probe")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy group left behind (err=%v)", err)
	}

	if _, err := manager.Remove(context.Background(), "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(modsRoot, GroupDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime group left behind (err=%v)", err)
	}
}

func hasCode(err error, code string) bool {
	var contract infra.ContractError
	return errors.As(err, &contract) && strings.HasPrefix(string(contract), code+":")
}

// Settings are written into managed files, so an update must carry them over
// instead of restoring the bundled values.
func TestSettingsSurviveUpdate(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	ctx := context.Background()
	probe := filepath.Join(installRoot(modsRoot, "Probe"), "Probe.ini")
	if _, err := manager.Install(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	if installed, _ := os.ReadFile(probe); string(installed) != bundledProbe {
		t.Fatalf("default settings changed the bundled file:\n%q", installed)
	}

	if _, err := manager.SetSettings(
		ctx,
		"EFMI",
		"Probe",
		map[string]string{"strength": "9"},
	); !hasCode(
		err,
		ErrorInvalid,
	) {
		t.Fatalf("out-of-range strength = %v", err)
	}
	// A value must not start a line of its own, such as a new section.
	if _, err := manager.SetSettings(
		ctx,
		"EFMI",
		"Probe",
		map[string]string{"toggleKey": "VK_F7\r\n[Loader]"},
	); err != nil {
		t.Fatal(err)
	}
	if injected, _ := os.ReadFile(probe); strings.Contains(string(injected), "\n[Loader]") {
		t.Fatalf("a setting value added a line:\n%q", injected)
	}
	status, err := manager.SetSettings(ctx, "EFMI", "Probe", map[string]string{
		"toggleKey": "ctrl  VK_F7", "strength": "5",
	})
	if err != nil || status.State != StateCurrent {
		t.Fatalf("set settings = %+v (err=%v)", status, err)
	}
	want := "namespace = Probe\r\n[Constants]\r\nglobal $strength = 5\r\n[KeyToggle]\r\nkey = ctrl VK_F7\r\n"
	if written, _ := os.ReadFile(probe); string(written) != want {
		t.Fatalf("settings not written:\n%q", written)
	}

	manifestPath := filepath.Join(installRoot(modsRoot, "Probe"), manifestFile)
	data, _ := os.ReadFile(manifestPath)
	if err := os.WriteFile(manifestPath, []byte(strings.Replace(string(data), `"2"`, `"1"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := manager.state(t).State; got != StateOutdated {
		t.Fatalf("after downgrading the manifest = %s", got)
	}
	if _, err := manager.Install(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	if updated, _ := os.ReadFile(probe); string(updated) != want {
		t.Fatalf("update lost the settings:\n%q", updated)
	}
	if status := manager.state(t); status.Settings[0].Value != "ctrl VK_F7" {
		t.Fatalf("settings after update = %+v", status.Settings)
	}
}
