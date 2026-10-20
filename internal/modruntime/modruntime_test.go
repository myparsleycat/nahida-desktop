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

// A runtime ships one spec per importer under the same name, and each game's
// Mods folder must get its own files.
func TestSpecPerImporter(t *testing.T) {
	efmiRoot, gimiRoot := t.TempDir(), t.TempDir()
	efmi, gimi := "EFMI", "GIMI"
	spec := func(importer string) Spec {
		probe := "namespace = Probe\r\n; " + importer + "\r\n"
		return Spec{
			Name: "Probe", Version: "1", Importers: []string{importer}, Namespace: "Probe",
			Files: fstest.MapFS{"Probe.ini": {Data: []byte(probe)}},
		}
	}
	manager := New(Options{
		GamePaths: fakeGamePaths{
			{Game: "endfield", ModFolderPath: efmiRoot, Importer: &efmi},
			{Game: "genshin", ModFolderPath: gimiRoot, Importer: &gimi},
		},
		Mod:   renamingMods{},
		Specs: []Spec{spec(efmi), spec(gimi)},
	})

	for importer, modsRoot := range map[string]string{efmi: efmiRoot, gimi: gimiRoot} {
		if _, err := manager.Install(context.Background(), importer, "Probe"); err != nil {
			t.Fatalf("install for %s: %v", importer, err)
		}
		probe, err := os.ReadFile(filepath.Join(installRoot(modsRoot, "Probe"), "Probe.ini"))
		if err != nil || !strings.Contains(string(probe), "; "+importer) {
			t.Fatalf("%s got Probe.ini %q (err=%v)", importer, probe, err)
		}
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

// An update nobody asked for must not replace files the user edited or added,
// yet must finish one an earlier update left half written.
func TestRefreshKeepsUserChanges(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	ctx := context.Background()
	root := installRoot(modsRoot, "Probe")
	caps := Capabilities{}
	manager.capabilities = func(context.Context, string) (Capabilities, error) { return caps, nil }
	if _, err := manager.Install(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, manifestFile)
	rewriteManifest := func(edit func(*manifest)) {
		t.Helper()
		found, err := readManifest(manifestPath)
		if err != nil {
			t.Fatal(err)
		}
		edit(found)
		encoded, err := encodeJSON(found)
		if err == nil {
			err = os.WriteFile(manifestPath, encoded, 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	shader := filepath.Join(root, "shaders", "a.hlsl")
	if err := os.WriteFile(shader, []byte("// user edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	rewriteManifest(func(found *manifest) { found.RuntimeVersion = "1" })
	if err := manager.Refresh(ctx, "EFMI"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(shader); string(data) != "// user edit" {
		t.Fatalf("refresh replaced the user's shader: %q", data)
	}
	if got := manager.state(t).State; got != StateOutdated {
		t.Fatalf("edited runtime after a refresh = %s", got)
	}

	// The bundled file under an old hash is what an interrupted update wrote.
	if err := os.WriteFile(shader, []byte("// a"), 0o644); err != nil {
		t.Fatal(err)
	}
	rewriteManifest(func(found *manifest) { found.Files["shaders/a.hlsl"] = digest([]byte("// old a")) })
	if err := manager.Refresh(ctx, "EFMI"); err != nil {
		t.Fatal(err)
	}
	if got := manager.state(t).State; got != StateCurrent {
		t.Fatalf("interrupted update after a refresh = %s", got)
	}

	// A file the user added where the enhanced variant puts one of its own.
	extra := filepath.Join(root, "extra", "Extra.ini")
	if err := os.MkdirAll(filepath.Dir(extra), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("; user file"), 0o644); err != nil {
		t.Fatal(err)
	}
	caps.ShaderSamplerSlots = true
	if err := manager.Refresh(ctx, "EFMI"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(extra); string(data) != "; user file" {
		t.Fatalf("refresh replaced the user's file: %q", data)
	}
	if got := manager.state(t).State; got != StateOutdated {
		t.Fatalf("runtime under the user's file after a refresh = %s", got)
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

// 3DMigoto recompiles a custom shader only when its own source is newer than
// the cached .bin, so an update that changes just an included file must drop
// the caches of every shader that includes it, also when it is retried after
// an interruption, and never a cache outside the install folder.
func TestUpdateDropsCachesOfIncludingShaders(t *testing.T) {
	manager, modsRoot := newTestManager(t)
	ctx := context.Background()
	root := installRoot(modsRoot, "Probe")
	files := manager.specs[0].Files.(fstest.MapFS)
	files["shaders/Common.hlsl"] = &fstest.MapFile{Data: []byte("// v1")}
	files["shaders/b.hlsl"] = &fstest.MapFile{Data: []byte("#include \"common.hlsl\"\n")}
	files["shaders/c.hlsl"] = &fstest.MapFile{Data: []byte("#define X\n#include \"b.hlsl\"\n")}
	files["shaders/z.ini"] = &fstest.MapFile{Data: []byte("; v1")}
	if _, err := manager.Install(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	caches := map[string]bool{
		"a.ps_5_0.8000.bin": true,
		"b.ps_5_0.8000.bin": false,
		"b.vs_5_0.8000.bin": false,
		"c.ps_5_0.8000.bin": false,
		"bb.ps_5_0.bin":     true,
	}
	for name := range caches {
		if err := os.WriteFile(filepath.Join(root, "shaders", name), []byte("cache"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A shader the last install listed below a junction to another folder.
	outside := t.TempDir()
	outsideCache := filepath.Join(outside, "keep.ps_5_0.8000.bin")
	if err := os.WriteFile(outsideCache, []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Logf("filesystem cannot create a junction, skipping that case: %v (%s)", err, output)
	} else {
		manifestPath := filepath.Join(root, manifestFile)
		previous, err := readManifest(manifestPath)
		if err != nil || previous == nil {
			t.Fatalf("manifest = %+v (err=%v)", previous, err)
		}
		previous.Files["link/keep.hlsl"] = "stale"
		encoded, err := encodeJSON(previous)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestPath, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// The update stops after Common.hlsl is replaced, so the retry finds it
	// up to date while the caches built from its old content remain.
	files["shaders/Common.hlsl"] = &fstest.MapFile{Data: []byte("// v2")}
	files["shaders/z.ini"] = &fstest.MapFile{Data: []byte("; v2")}
	manager.specs[0].Version = "3"
	failing := errors.New("disk full")
	manager.writeFile = func(name string, data []byte) error {
		if filepath.Base(name) == "z.ini" {
			return failing
		}
		return writeAtomic(name, data)
	}
	if _, err := manager.Install(ctx, "EFMI", "Probe"); !errors.Is(err, failing) {
		t.Fatalf("interrupted update = %v", err)
	}
	manager.writeFile = writeAtomic
	if _, err := manager.Install(ctx, "EFMI", "Probe"); err != nil {
		t.Fatal(err)
	}
	for name, kept := range caches {
		_, err := os.Stat(filepath.Join(root, "shaders", name))
		if exists := err == nil; exists != kept {
			t.Errorf("%s exists = %v, want %v (err=%v)", name, exists, kept, err)
		}
	}
	if _, err := os.Stat(outsideCache); err != nil {
		t.Errorf("a cache outside the install folder was removed: %v", err)
	}

	// A repair of the same version stops after Common.hlsl is restored; the
	// manifest then matches the disk, so only the recorded progress can tell
	// the retry that the cache came from the edited include.
	shaders := filepath.Join(root, "shaders")
	for name, data := range map[string]string{"Common.hlsl": "// edited", "z.ini": "; edited"} {
		if err := os.WriteFile(filepath.Join(shaders, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	editedCache := filepath.Join(shaders, "b.ps_5_0.8000.bin")
	if err := os.WriteFile(editedCache, []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.writeFile = func(name string, data []byte) error {
		if filepath.Base(name) == "z.ini" {
			return failing
		}
		return writeAtomic(name, data)
	}
	if _, err := manager.Install(ctx, "EFMI", "Probe"); !errors.Is(err, failing) {
		t.Fatalf("interrupted repair = %v", err)
	}
	manager.writeFile = writeAtomic
	if status, err := manager.Install(ctx, "EFMI", "Probe"); err != nil || status.State != StateCurrent {
		t.Fatalf("repair = %+v (err=%v)", status, err)
	}
	if _, err := os.Stat(editedCache); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the cache of the edited include survived the repair (err=%v)", err)
	}
}
