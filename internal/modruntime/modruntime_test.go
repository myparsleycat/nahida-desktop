package modruntime

import (
	"context"
	"errors"
	"io/fs"
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
	seedFile      = "surface/Loader.ini"
	bundledProbe  = "namespace = Probe\r\n[Constants]\r\nglobal $strength = 3\r\n[KeyToggle]\r\nkey = VK_F1\r\n"
	enhancedProbe = bundledProbe + "[ShaderOverrideFluid]\r\nps-s7 = ResourceFluid\r\n"
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
				"Probe.ini":                {Data: []byte(bundledProbe)},
				"shaders/a.hlsl":           {Data: []byte("// a")},
				"enhanced/Probe.ini":       {Data: []byte(enhancedProbe)},
				"enhanced/extra/Extra.ini": {Data: []byte("; extra")},
			},
			Variants: []Variant{{
				Name:     "enhanced",
				Requires: func(caps Capabilities) bool { return caps.ShaderSamplerSlots },
				Dir:      "enhanced",
			}},
			Seed: map[string]func(string, string) ([]byte, error){
				seedFile: func(string, string) ([]byte, error) { return []byte("; seeded"), nil },
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
	seedPath := filepath.Join(root, filepath.FromSlash(seedFile))
	if seeded, _ := os.ReadFile(seedPath); string(seeded) != "; feature" {
		t.Fatalf("repair replaced the feature's file: %q", seeded)
	}

	// The runtime files alone do not make an install current.
	if err := os.Remove(seedPath); err != nil {
		t.Fatal(err)
	}
	if got := manager.state(t).State; got != StateModified {
		t.Fatalf("without the seed file = %s", got)
	}
	if status, err := manager.Install(ctx, "EFMI", "Probe"); err != nil || status.State != StateCurrent {
		t.Fatalf("repair of the seed file = %+v (err=%v)", status, err)
	}
	if seeded, _ := os.ReadFile(seedPath); string(seeded) != "; seeded" {
		t.Fatalf("repair did not seed the file again: %q", seeded)
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

// A d3d11.dll without a variant's capabilities must never get its files, and
// a DLL change must swap the files of the last install for the new ones.
func TestVariantFollowsCapabilities(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	ctx := context.Background()
	root := installRoot(modsRoot, "Probe")
	caps := Capabilities{}
	manager.capabilities = func(context.Context, string) (Capabilities, error) { return caps, nil }
	probe := filepath.Join(root, "Probe.ini")
	extra := filepath.Join(root, "extra", "Extra.ini")
	absent := func(name string) {
		t.Helper()
		if _, err := os.Stat(name); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s is installed (err=%v)", name, err)
		}
	}
	content := func(name, want string) {
		t.Helper()
		if data, _ := os.ReadFile(name); string(data) != want {
			t.Fatalf("%s:\n%q", name, data)
		}
	}

	if status, err := manager.Install(ctx, "EFMI", "Probe"); err != nil || status.State != StateCurrent {
		t.Fatalf("base install = %+v (err=%v)", status, err)
	}
	absent(extra)
	absent(filepath.Join(root, "enhanced"))
	content(probe, bundledProbe)

	caps.ShaderSamplerSlots = true
	if got := manager.state(t).State; got != StateOutdated {
		t.Fatalf("base install under a capable DLL = %s", got)
	}
	if err := manager.Refresh(ctx, "EFMI"); err != nil {
		t.Fatal(err)
	}
	content(extra, "; extra")
	content(probe, enhancedProbe)
	if got := manager.state(t).State; got != StateCurrent {
		t.Fatalf("after the refresh = %s", got)
	}

	caps = Capabilities{}
	if err := manager.Refresh(ctx, "EFMI"); err != nil {
		t.Fatal(err)
	}
	absent(extra)
	absent(filepath.Dir(extra))
	content(probe, bundledProbe)
	content(filepath.Join(root, filepath.FromSlash(seedFile)), "; seeded")
	if got := manager.state(t).State; got != StateCurrent {
		t.Fatalf("after switching back = %s", got)
	}

	// A refresh brings nothing back the user turned off.
	if _, err := manager.Disable(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	caps.ShaderSamplerSlots = true
	if err := manager.Refresh(ctx, "EFMI"); err != nil {
		t.Fatal(err)
	}
	if got := manager.state(t).State; got != StateDisabled {
		t.Fatalf("disabled runtime after a refresh = %s", got)
	}
}

// A manifest entry may be edited by hand or point through a junction; the
// stale-file cleanup of an update must stay inside the install folder.
func TestUpdateRemovesStaleFilesOnlyBelowRoot(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	ctx := context.Background()
	root := installRoot(modsRoot, "Probe")
	if _, err := manager.Install(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(filepath.Dir(root), "escape.txt")
	if err := os.WriteFile(escape, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	junction := true
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Logf("filesystem cannot create a junction, skipping that case: %v (%s)", err, output)
		junction = false
	}

	manifestPath := filepath.Join(root, manifestFile)
	previous, err := readManifest(manifestPath)
	if err != nil || previous == nil {
		t.Fatalf("manifest = %+v (err=%v)", previous, err)
	}
	previous.RuntimeVersion = "0"
	for _, name := range []string{`..\escape.txt`, "../escape.txt", "./../escape.txt", escape, "link/keep.txt"} {
		previous.Files[name] = "stale"
	}
	encoded, err := encodeJSON(previous)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	if status, err := manager.Install(ctx, "EFMI", "Probe"); err != nil || status.State != StateCurrent {
		t.Fatalf("update = %+v (err=%v)", status, err)
	}
	for _, name := range []string{escape, keep} {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("%s was removed by the update: %v", name, err)
		}
	}
	if junction {
		if _, err := os.Lstat(link); err != nil {
			t.Errorf("the junction was removed by the update: %v", err)
		}
	}
}
