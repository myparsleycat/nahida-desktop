//go:build windows

package mod

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func namespaceJunction(t *testing.T, link, target string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction: %v (%s)", err, output)
	}
}

func namespaceSymlink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
}

// The built-in importer folder links Mods and ShaderFixes to an external XXMI Launcher.
func TestNamespaceIsolationFollowsLinkedImporterFolders(t *testing.T) {
	t.Parallel()
	links := map[string]func(*testing.T, string, string){"junction": namespaceJunction, "symlink": namespaceSymlink}
	for name, link := range links {
		for _, layout := range []string{"game folder is the link", "game folder is the target"} {
			t.Run(name+"/"+layout, func(t *testing.T) {
				t.Parallel()
				m, mods, _ := newNamespaceTestMod(t)
				importer := filepath.Dir(mods)
				collection := mods
				if layout == "game folder is the link" {
					collection = filepath.Join(t.TempDir(), "External Mods")
					if err := os.Rename(mods, collection); err != nil {
						t.Fatal(err)
					}
				} else {
					importer = filepath.Join(t.TempDir(), "Linked GIMI")
					if err := os.MkdirAll(importer, 0o755); err != nil {
						t.Fatal(err)
					}
					m.xxmi = namespaceTestImporterSource{folder: importer}
				}
				link(t, filepath.Join(importer, "Mods"), collection)
				paths := []string{namespaceFixture(t, collection, "A"), namespaceFixture(t, collection, "B")}

				shaders := filepath.Join(t.TempDir(), "External ShaderFixes")
				namespaceWrite(t, filepath.Join(shaders, "help.ini"), "[Constants]\nglobal persist $fix = 1\n")
				link(t, filepath.Join(importer, "ShaderFixes"), shaders)
				// A link back to an ancestor must end the walk instead of looping.
				link(t, filepath.Join(shaders, "Loop"), importer)

				// File links need a privilege that junctions do not, so they are covered when available.
				want := map[string]string{"help.ini": `ShaderFixes\help.ini`}
				external := filepath.Join(t.TempDir(), "external.ini")
				namespaceWrite(t, external, "[Constants]\nglobal persist $core = 1\n")
				namespaceWrite(t, filepath.Join(importer, "Core", "plain.ini"), "[Constants]\nglobal $plain = 1\n")
				if os.Symlink(external, filepath.Join(importer, "Core", "linked.ini")) == nil {
					want["linked.ini"] = `Core\linked.ini`
				}
				want["plain.ini"] = `Core\plain.ini`

				inventories, err := m.namespaceIsolation.inventories(t.Context(), "")
				if err != nil {
					t.Fatal(err)
				}
				if len(inventories) != 1 || len(inventories[0].issues) != 0 ||
					len(inventories[0].files) != 4+len(want) {
					t.Fatalf("incomplete inventory: %+v", inventories)
				}
				for _, file := range inventories[0].files {
					if name, ok := want[filepath.Base(file.path)]; ok {
						if file.document.Namespace != name || file.modPath != "" {
							t.Fatalf("linked reference lost its importer-relative namespace: %+v", file)
						}
						continue
					}
					if file.modPath == "" {
						t.Fatalf("linked mods root lost mod ownership: %s", file.path)
					}
				}

				state, err := m.RescanNamespaceIsolation(t.Context(), "")
				if err != nil || len(state.Conflicts) != 0 {
					t.Fatalf("isolation through linked folders: %+v %v", state, err)
				}
				names := map[string]bool{}
				for _, path := range paths {
					name := namespaceDocument(t, filepath.Join(path, "main.ini")).Namespace
					if !strings.Contains(name, "__nhd_") || names[name] {
						t.Fatalf("copies behind a linked mods root were not isolated: %q", name)
					}
					names[name] = true
				}
			})
		}
	}
}

func TestNamespaceIsolationRejectsLinksInsideMods(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	outside := t.TempDir()
	namespaceWrite(t, filepath.Join(outside, "extra.ini"), namespaceGUIINI)
	namespaceJunction(t, filepath.Join(a, "Linked"), outside)

	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	unsafe := slices.ContainsFunc(state.Conflicts, func(c NamespaceIsolationConflict) bool {
		return c.Reason == "unsafe_path" && len(c.INIPaths) == 1 && filepath.Base(c.INIPaths[0]) == "Linked"
	})
	if !unsafe {
		t.Fatalf("link inside a mod was not reported: %+v", state)
	}
	if namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatal("mod with a linked folder was mutated")
	}
}

func TestNamespaceIsolationShortImporterPath(t *testing.T) {
	t.Parallel()
	m, mods, settings := newNamespaceTestMod(t)
	original := namespaceFixture(t, mods, "Original")
	namespaceWrite(t, filepath.Join(original, "second.ini"), namespaceMainINI)
	folder, err := filepath.EvalSymlinks(m.xxmi.(namespaceTestImporterSource).folder)
	if err != nil {
		t.Fatal(err)
	}
	namespaceWrite(t, filepath.Join(folder, "Core", "implicit.ini"), "[Constants]\nglobal persist $core = 1\n")
	longPath, err := windows.UTF16FromString(folder)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, len(longPath))
	length, err := windows.GetShortPathName(&longPath[0], &buffer[0], uint32(len(buffer)))
	if err != nil || length == 0 || length >= uint32(len(buffer)) {
		t.Fatalf("short importer path length = %d, error = %v", length, err)
	}
	shortPath := windows.UTF16ToString(buffer)
	if strings.EqualFold(shortPath, folder) {
		t.Skip("importer has no distinct 8.3 path on this filesystem")
	}
	t.Logf("importer aliases: long=%q short=%q", folder, shortPath)
	m.xxmi = namespaceTestImporterSource{folder: shortPath}

	inventories, err := m.namespaceIsolation.inventories(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(inventories) != 1 || len(inventories[0].issues) != 0 || len(inventories[0].files) != 4 {
		t.Fatalf("incomplete inventory: %+v", inventories)
	}
	for _, file := range inventories[0].files {
		if filepath.Base(file.path) == "implicit.ini" {
			if file.document.Namespace != `Core\implicit.ini` {
				t.Fatalf("implicit namespace includes a path alias: %q", file.document.Namespace)
			}
			continue
		}
		if file.modPath == "" {
			t.Fatalf("short importer path lost mod ownership: %s", file.path)
		}
	}
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("same-mod sharing through short path: %+v %v", state, err)
	}
	if namespaceDocument(t, filepath.Join(original, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatal("noncolliding namespace changed")
	}

	clone := namespaceFixture(t, mods, "Copy")
	namespaceWrite(t, filepath.Join(clone, "second.ini"), namespaceMainINI)
	state, err = m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("copy isolation through short path: %+v %v", state, err)
	}
	names := map[string]bool{}
	for _, path := range []string{original, clone} {
		main := namespaceDocument(t, filepath.Join(path, "main.ini"))
		gui := namespaceDocument(t, filepath.Join(path, "GUI", "gui.ini"))
		second := namespaceDocument(t, filepath.Join(path, "second.ini"))
		if main.Namespace == `Creator\Dress` || names[main.Namespace] ||
			gui.Namespace != main.Namespace || second.Namespace != main.Namespace {
			t.Fatalf("copy namespaces were not isolated together: %+v %+v %+v", main, gui, second)
		}
		names[main.Namespace] = true
	}

	settings.isolation.Store(false)
	namespaceWrite(t, filepath.Join(original, ".nhd-namespace", "transactions", "broken", "journal.json"), "{")
	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err == nil {
		t.Fatal("disabled launch missed the journal through the short importer alias")
	}
}

func TestNamespaceIsolationDisabledLaunchChecksJournalsWithoutReadingINIs(t *testing.T) {
	t.Parallel()
	m, mods, settings := newNamespaceTestMod(t)
	collection := filepath.Join(t.TempDir(), "External Mods")
	if err := os.Rename(mods, collection); err != nil {
		t.Fatal(err)
	}
	namespaceJunction(t, mods, collection)
	path := namespaceFixture(t, collection, "Only")
	d3dxPath := filepath.Join(filepath.Dir(mods), "d3dx_user.ini")
	namespaceWrite(t, d3dxPath, "[Constants]\n")
	settings.isolation.Store(false)
	for _, lockedPath := range []string{filepath.Join(path, "main.ini"), d3dxPath} {
		name, err := windows.UTF16FromString(lockedPath)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := windows.CreateFile(&name[0], windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = windows.CloseHandle(handle) })
	}

	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err != nil {
		t.Fatalf("launch read a locked INI: %v", err)
	}
	if state := m.GetNamespaceIsolationState(); len(state.Conflicts) != 0 {
		t.Fatalf("locked INIs were scanned: %+v", state)
	}
	namespaceWrite(t, filepath.Join(path, ".nhd-namespace", "transactions", "broken", "journal.json"), "{")
	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err == nil ||
		!strings.Contains(err.Error(), "NAMESPACE_ISOLATION_TRANSACTION_UNRESOLVED") {
		t.Fatalf("journal through linked Mods root = %v", err)
	}
}
