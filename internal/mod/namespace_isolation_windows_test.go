//go:build windows

package mod

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestNamespaceIsolationShortImporterPath(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
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
}
