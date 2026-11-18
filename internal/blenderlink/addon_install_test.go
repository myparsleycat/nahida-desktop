package blenderlink

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type addonTestEnv struct {
	m       *Manager
	root    string
	scripts []string
}

func newAddonTestEnv(t *testing.T, installed, running map[blenderVersion]string, series ...string) *addonTestEnv {
	t.Helper()
	env := &addonTestEnv{m: NewManager(Options{}), root: t.TempDir()}
	for _, name := range series {
		if err := os.MkdirAll(filepath.Join(env.root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env.m.addon = addonEnv{
		configRoot: func() (string, error) { return env.root, nil },
		installed:  func(context.Context) (map[blenderVersion]string, error) { return installed, nil },
		running:    func(context.Context) (map[blenderVersion]string, error) { return running, nil },
		run: func(_ context.Context, executable, script string) (string, error) {
			env.scripts = append(env.scripts, executable+"\n"+script)
			return addonDoneMarker + "\n", nil
		},
		portable: func(string) bool { return false },
		image:    func(uint32) (string, bool) { return "", false },
	}
	return env
}

func (env *addonTestEnv) target(t *testing.T, version string) AddonTarget {
	t.Helper()
	targets, err := env.m.AddonTargets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.Version == version {
			return target
		}
	}
	t.Fatalf("Blender %s not listed in %+v", version, targets)
	return AddonTarget{}
}

func writeTestFile(t *testing.T, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

func TestInstallAddonKeepsUserFilesAndRemovesOwnStaleOnes(t *testing.T) {
	ctx := context.Background()
	env := newAddonTestEnv(t, map[blenderVersion]string{{5, 2}: `C:\Blender\blender.exe`}, nil, "5.2", "4.1", "3.5")

	current := env.target(t, "5.2")
	if current.State != AddonMissing ||
		current.Path != filepath.Join(env.root, "5.2", "extensions", "user_default", addonID) {
		t.Fatalf("5.2 before install = %+v", current)
	}
	if legacy := env.target(t, "4.1"); legacy.Path != filepath.Join(env.root, "4.1", "scripts", "addons", addonID) {
		t.Fatalf("4.1 path = %s", legacy.Path)
	}
	targets, _ := env.m.AddonTargets(ctx)
	if len(targets) != 2 {
		t.Fatalf("targets = %+v, want 5.2 and 4.1 only", targets)
	}

	result, err := env.m.InstallAddon(ctx, "5.2")
	if err != nil || !result.Enabled {
		t.Fatalf("install 5.2 = %+v, %v", result, err)
	}
	if len(env.scripts) != 1 || !strings.Contains(env.scripts[0], `"bl_ext.user_default.nahida_live_link"`) {
		t.Fatalf("scripts = %q", env.scripts)
	}
	if state := env.target(t, "5.2").State; state != AddonCurrent {
		t.Fatalf("state after install = %s", state)
	}
	if result, err := env.m.InstallAddon(ctx, "4.1"); err != nil || !result.ManualEnable {
		t.Fatalf("install 4.1 = %+v, %v", result, err)
	}

	// A newer app installed a later version, which installing the bundle would downgrade.
	legacy := env.target(t, "4.1").Path
	newer, err := readAddonRecord(legacy)
	if err != nil {
		t.Fatal(err)
	}
	newer.Version = "99.0.0"
	newerData, err := json.Marshal(newer)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(legacy, addonRecord), string(newerData))
	if state := env.target(t, "4.1").State; state != AddonNewer {
		t.Fatalf("state with a newer record = %s", state)
	}
	if _, err := env.m.InstallAddon(ctx, "4.1"); !errors.Is(err, ErrAddonNewer) {
		t.Fatalf("install over a newer add-on: %v", err)
	}
	if kept, _ := readAddonRecord(legacy); kept == nil || kept.Version != "99.0.0" {
		t.Fatalf("record after a refused downgrade = %+v", kept)
	}

	// An older install listed a file the bundle no longer has; the user added one of their own.
	record, err := readAddonRecord(current.Path)
	if err != nil {
		t.Fatal(err)
	}
	record.Version, record.Files["old.py"] = "0.0.1", digest([]byte("old"))
	writeTestFile(t, filepath.Join(current.Path, "old.py"), "old")
	writeTestFile(t, filepath.Join(current.Path, "notes.txt"), "mine")
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(current.Path, addonRecord), string(data))
	if state := env.target(t, "5.2").State; state != AddonOutdated {
		t.Fatalf("state with old record = %s", state)
	}
	if _, err := env.m.InstallAddon(ctx, "5.2"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(current.Path, "old.py")) || !exists(filepath.Join(current.Path, "notes.txt")) {
		t.Fatal("update must remove the old install's file and keep the user's")
	}

	writeTestFile(t, filepath.Join(current.Path, "__init__.py"), "# edited")
	if state := env.target(t, "5.2").State; state != AddonModified {
		t.Fatalf("state after edit = %s", state)
	}

	if err := env.m.RemoveAddon(ctx, "5.2"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.scripts[len(env.scripts)-1], "addon_utils.disable") {
		t.Fatalf("remove did not disable: %q", env.scripts[len(env.scripts)-1])
	}
	if exists(filepath.Join(current.Path, "__init__.py")) || exists(filepath.Join(current.Path, addonRecord)) ||
		!exists(filepath.Join(current.Path, "notes.txt")) {
		t.Fatal("remove must delete the add-on's files and keep the user's")
	}
}

func TestInstallAddonHandlesLegacyCopies(t *testing.T) {
	ctx := context.Background()
	executable := `C:\Blender\blender.exe`
	env := newAddonTestEnv(t, map[blenderVersion]string{{5, 2}: executable}, nil, "5.2", "5.0")

	// A copy this app put in scripts/addons is replaced by the extension, whose module is enabled in place of the
	// legacy one; a copy it did not write blocks the install, because Blender would register the add-on twice.
	ours := legacyAddonPath(env.root, blenderVersion{5, 2})
	if err := writeAddon(ours, map[string][]byte{"__init__.py": []byte("old")}); err != nil {
		t.Fatal(err)
	}
	if target := env.target(t, "5.2"); !target.LegacyCopy || target.Conflict != "" {
		t.Fatalf("5.2 with the app's legacy copy = %+v", target)
	}

	// A failed enable leaves the legacy copy and its enabled module, so a retry replaces it the same way.
	run := env.m.addon.run
	env.m.addon.run = func(context.Context, string, string) (string, error) { return "", errors.New("timed out") }
	if _, err := env.m.InstallAddon(ctx, "5.2"); err == nil {
		t.Fatal("install with a failing enable succeeded")
	}
	if target := env.target(t, "5.2"); !target.LegacyCopy || !exists(filepath.Join(ours, "__init__.py")) {
		t.Fatalf("legacy copy after a failed enable = %+v", target)
	}
	env.m.addon.run = run
	if result, err := env.m.InstallAddon(ctx, "5.2"); err != nil || !result.Enabled {
		t.Fatalf("install 5.2 = %+v, %v", result, err)
	}
	if exists(ours) {
		t.Fatal("the app's legacy copy was not removed")
	}
	if len(env.scripts) != 1 ||
		!strings.Contains(env.scripts[0], `enable = ["bl_ext.user_default.nahida_live_link"]`) ||
		!strings.Contains(env.scripts[0], `disable = ["nahida_live_link"]`) {
		t.Fatalf("scripts = %q", env.scripts)
	}

	foreign := legacyAddonPath(env.root, blenderVersion{5, 0})
	writeTestFile(t, filepath.Join(foreign, "__init__.py"), "user zip")
	if target := env.target(t, "5.0"); target.Conflict != foreign {
		t.Fatalf("conflict = %q, want %q", target.Conflict, foreign)
	}
	if _, err := env.m.InstallAddon(ctx, "5.0"); !errors.Is(err, ErrAddonConflict) {
		t.Fatalf("install over a foreign legacy copy: %v", err)
	}
	if exists(addonPath(env.root, blenderVersion{5, 0})) || !exists(filepath.Join(foreign, "__init__.py")) {
		t.Fatal("a refused install must not write or remove anything")
	}

	// A junction to a development checkout must not lead the install into the checkout.
	checkout := filepath.Join(t.TempDir(), addonID)
	writeTestFile(t, filepath.Join(checkout, "__init__.py"), "source")
	linked := addonPath(env.root, blenderVersion{5, 2})
	if err := os.RemoveAll(linked); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", linked, checkout).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	if state := env.target(t, "5.2").State; state != AddonLinked {
		t.Fatalf("junction state = %s", state)
	}
	if _, err := env.m.InstallAddon(ctx, "5.2"); !errors.Is(err, ErrAddonLinked) {
		t.Fatalf("install over a junction: %v", err)
	}
	if err := env.m.RemoveAddon(ctx, "5.2"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(checkout, "__init__.py")); err != nil || string(data) != "source" ||
		exists(filepath.Join(checkout, addonRecord)) {
		t.Fatalf("checkout changed: %q, %v", data, err)
	}

	// A junction in scripts/addons is reported as a link, not as a copy to remove in Blender.
	linkedLegacy := legacyAddonPath(env.root, blenderVersion{4, 4})
	if err := os.MkdirAll(filepath.Dir(linkedLegacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", linkedLegacy, checkout).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v (%s)", err, output)
	}
	if target := env.target(t, "4.4"); target.ConflictState != AddonLinked || target.Blocked() != AddonBlockLinked {
		t.Fatalf("4.4 with a linked legacy copy = %+v", target)
	}
	if _, err := env.m.InstallAddon(ctx, "4.4"); !errors.Is(err, ErrAddonLinked) {
		t.Fatalf("install beside a linked legacy copy: %v", err)
	}
}

func TestInstallAddonWithRunningBlender(t *testing.T) {
	ctx := context.Background()
	executable := `C:\Blender\blender.exe`
	env := newAddonTestEnv(t, nil, map[blenderVersion]string{{5, 2}: executable}, "5.2")

	// Blender saves its preferences when it quits, which would drop an add-on enabled while it ran.
	if _, err := env.m.InstallAddon(ctx, "5.2"); !errors.Is(err, ErrBlenderRunning) {
		t.Fatalf("install while Blender runs: %v", err)
	}
	if exists(addonPath(env.root, blenderVersion{5, 2})) {
		t.Fatal("a refused install wrote files")
	}

	// A connection from a copy outside the target folder does not make a new copy there enabled.
	env.m.links[1] = LinkState{Link: Link{Session: 1, BlenderVersion: "5.2.2"}}
	if _, err := env.m.InstallAddon(ctx, "5.2"); !errors.Is(err, ErrBlenderRunning) {
		t.Fatalf("install into an empty target while connected: %v", err)
	}
	if exists(addonPath(env.root, blenderVersion{5, 2})) {
		t.Fatal("a refused install wrote files")
	}

	// A connected copy in the target folder is already enabled, so its files are replaced for the next start.
	if err := writeAddon(
		addonPath(env.root, blenderVersion{5, 2}),
		map[string][]byte{"__init__.py": []byte("old")},
	); err != nil {
		t.Fatal(err)
	}
	result, err := env.m.InstallAddon(ctx, "5.2")
	if err != nil || !result.Restart || len(env.scripts) != 0 {
		t.Fatalf("update while connected = %+v, %v, scripts %q", result, err, env.scripts)
	}
	if err := env.m.RemoveAddon(ctx, "5.2"); !errors.Is(err, ErrBlenderRunning) {
		t.Fatalf("remove while Blender runs: %v", err)
	}

	// Replacing a legacy copy needs the extension's module enabled, which the running Blender would undo.
	legacy := legacyAddonPath(env.root, blenderVersion{5, 2})
	if err := writeAddon(legacy, map[string][]byte{"__init__.py": []byte("old")}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.m.InstallAddon(ctx, "5.2"); !errors.Is(err, ErrBlenderRunning) {
		t.Fatalf("replace a legacy copy while Blender runs: %v", err)
	}
	if !exists(filepath.Join(legacy, "__init__.py")) {
		t.Fatal("a refused install removed the legacy copy")
	}
}

func TestRemoveAndUpdateLegacyCopies(t *testing.T) {
	ctx := context.Background()
	executable := `C:\Blender\blender.exe`
	env := newAddonTestEnv(t, map[blenderVersion]string{{5, 2}: executable}, nil, "5.2", "5.0")
	old := map[string][]byte{"__init__.py": []byte("old")}

	// Remove takes both copies the app installed and turns both modules off, or Blender keeps loading the legacy one.
	extension, legacy := addonPath(env.root, blenderVersion{5, 2}), legacyAddonPath(env.root, blenderVersion{5, 2})
	for _, dir := range []string{extension, legacy} {
		if err := writeAddon(dir, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.m.RemoveAddon(ctx, "5.2"); err != nil {
		t.Fatal(err)
	}
	if exists(extension) || exists(legacy) {
		t.Fatal("remove left a copy the app installed")
	}
	if len(env.scripts) != 1 ||
		!strings.Contains(env.scripts[0], `disable = ["bl_ext.user_default.nahida_live_link","nahida_live_link"]`) {
		t.Fatalf("scripts = %q", env.scripts)
	}

	// A legacy copy alone is removed too.
	if err := writeAddon(legacy, old); err != nil {
		t.Fatal(err)
	}
	if err := env.m.RemoveAddon(ctx, "5.2"); err != nil || exists(legacy) {
		t.Fatalf("remove a lone legacy copy: %v, left %v", err, exists(legacy))
	}

	// Without an executable the modules cannot be switched, so the legacy copy is updated where it is.
	stale := legacyAddonPath(env.root, blenderVersion{5, 0})
	if err := writeAddon(stale, old); err != nil {
		t.Fatal(err)
	}
	record, _ := readAddonRecord(stale)
	record.Version = "0.0.1"
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(stale, addonRecord), string(data))
	if target := env.target(t, "5.0"); target.LegacyState != AddonOutdated || target.LegacyVersion != "0.0.1" {
		t.Fatalf("5.0 legacy copy = %+v", target)
	}
	result, err := env.m.InstallAddon(ctx, "5.0")
	if err != nil || !result.Legacy {
		t.Fatalf("install 5.0 without an executable = %+v, %v", result, err)
	}
	if updated, _ := readAddonRecord(stale); updated == nil || updated.Version != AddonVersion() {
		t.Fatalf("legacy record after update = %+v", updated)
	}
	if exists(addonPath(env.root, blenderVersion{5, 0})) {
		t.Fatal("an in-place update wrote the extension")
	}
}

func TestAddonTargetsIgnoreOtherBlenders(t *testing.T) {
	ctx := context.Background()
	portable := `D:\Portable Blender\blender.exe`
	env := newAddonTestEnv(t, nil, map[blenderVersion]string{{2, 93}: `C:\Old\blender.exe`, {5, 2}: portable}, "5.2")
	env.m.addon.portable = func(executable string) bool { return executable == portable }

	// A release the add-on does not support, and a portable Blender with its own configuration, are not running
	// as far as the listing and its poll are concerned.
	if running, err := env.m.RunningBlenders(ctx); err != nil || len(running) != 0 {
		t.Fatalf("running = %q, %v", running, err)
	}
	if target := env.target(t, "5.2"); target.Running || target.Executable != "" {
		t.Fatalf("5.2 with only a portable Blender running = %+v", target)
	}
	if _, err := env.m.InstallAddon(ctx, "5.2"); err != nil {
		t.Fatalf("install beside a portable Blender: %v", err)
	}
}

// Blender 4.1 and older read the version from bl_info, 4.2 and later and the app from blender_manifest.toml; a
// mismatch would show every install as outdated.
func TestAddonVersionMatchesBlInfo(t *testing.T) {
	data, err := addonFS.ReadFile(addonSource + "/__init__.py")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`"version":\s*\((\d+),\s*(\d+),\s*(\d+)\)`).FindSubmatch(data)
	if match == nil {
		t.Fatal("bl_info has no version")
	}
	blInfo := string(match[1]) + "." + string(match[2]) + "." + string(match[3])
	if AddonVersion() == "" || AddonVersion() != blInfo {
		t.Fatalf("blender_manifest.toml version %q, bl_info version %q", AddonVersion(), blInfo)
	}
}
