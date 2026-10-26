//go:build windows

package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/platform"
)

// newChainedFolder returns a folder holding a mod and a junction that reaches it, plus the folder's physical path.
func newChainedFolder(t *testing.T) (middle, physical string) {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "Library")
	writeModFolderFile(t, filepath.Join(folder, "Hero", "mod.ini"))
	physical, err := platform.FinalPath(folder)
	if err != nil {
		t.Fatal(err)
	}
	middle = filepath.Join(t.TempDir(), "LauncherMods")
	if err := writeJunction(middle, folder); err != nil {
		t.Fatal(err)
	}
	return middle, physical
}

func assertLinksTo(t *testing.T, link, want string) {
	t.Helper()
	target, err := os.Readlink(link)
	if err != nil || !platform.SamePathFold(target, want) {
		t.Fatalf("%s links to %q (%v), want %q", link, target, err, want)
	}
}

func TestCreateJunctionSkipsLinkedTarget(t *testing.T) {
	middle, physical := newChainedFolder(t)
	link := filepath.Join(t.TempDir(), "Mods")

	if err := createJunction(link, middle); err != nil {
		t.Fatal(err)
	}
	assertLinksTo(t, link, physical)
}

func TestRepairImporterLinks(t *testing.T) {
	ctx := context.Background()
	service, folder := newModFolderTestService(t)
	mods := filepath.Join(folder, "Mods")
	middle, physical := newChainedFolder(t)
	if err := os.Remove(mods); err != nil {
		t.Fatal(err)
	}
	if err := writeJunction(mods, middle); err != nil {
		t.Fatal(err)
	}
	direct, err := platform.FinalPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	extras := filepath.Join(folder, "Extras")
	if err := writeJunction(extras, direct); err != nil {
		t.Fatal(err)
	}
	launcherShaders := filepath.Join(t.TempDir(), "ShaderFixes")
	writeModFolderFile(t, filepath.Join(launcherShaders, "help.ini"))
	shaders := filepath.Join(folder, "ShaderFixes")
	if err := writeJunction(shaders, launcherShaders); err != nil {
		t.Fatal(err)
	}

	// An unreadable importer listed first is reported without holding back the repair of the next one.
	if err := service.client.XXMIImporters.Upsert(ctx, "AAA", "{"); err != nil {
		t.Fatal(err)
	}

	if err := service.RepairImporterLinks(ctx); err == nil {
		t.Fatal("unreadable importer config was not reported")
	}
	assertLinksTo(t, mods, physical)
	if !fileExists(filepath.Join(mods, "Hero", "mod.ini")) {
		t.Fatal("mods are unreachable through the flattened link")
	}
	assertLinksTo(t, extras, direct)
	// The intermediate link belongs to whoever made it, such as an external launcher, and stays as it was.
	if !fileExists(filepath.Join(middle, "Hero", "mod.ini")) {
		t.Fatal("flattening changed the intermediate link")
	}

	info, err := os.Lstat(shaders)
	if err != nil || isInstallReparsePoint(info) || !info.IsDir() {
		t.Fatalf("ShaderFixes is not a real folder: %v, %v", info, err)
	}
	if !fileExists(filepath.Join(shaders, "help.ini")) {
		t.Fatal("ShaderFixes content was not copied")
	}
	if !fileExists(filepath.Join(launcherShaders, "help.ini")) {
		t.Fatal("the external launcher's ShaderFixes was changed")
	}
}

func TestReplaceFolderLinkKeepsLinkOverNestedLink(t *testing.T) {
	shared := t.TempDir()
	writeModFolderFile(t, filepath.Join(shared, "fix.hlsl"))
	launcherShaders := filepath.Join(t.TempDir(), "ShaderFixes")
	if err := os.MkdirAll(launcherShaders, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJunction(filepath.Join(launcherShaders, "Shared"), shared); err != nil {
		t.Fatal(err)
	}
	importer := t.TempDir()
	shaders := filepath.Join(importer, "ShaderFixes")
	if err := writeJunction(shaders, launcherShaders); err != nil {
		t.Fatal(err)
	}
	link, ok := folderLink(shaders)
	if !ok {
		t.Fatal("ShaderFixes junction was not recognized")
	}

	if err := replaceFolderLink(context.Background(), link); err == nil {
		t.Fatal("a target holding a nested link was replaced")
	}
	assertLinksTo(t, shaders, launcherShaders)
	if !fileExists(filepath.Join(shaders, "Shared", "fix.hlsl")) {
		t.Fatal("shaders behind the nested link are unreachable")
	}
	entries, err := os.ReadDir(importer)
	if err != nil || len(entries) != 1 {
		t.Fatalf("importer folder holds %v (%v), want only ShaderFixes", entries, err)
	}
}
