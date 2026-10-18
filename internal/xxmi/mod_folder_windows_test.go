//go:build windows

package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func newModFolderTestService(t *testing.T) (*XXMI, string) {
	t.Helper()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	service.processSnapshot = func(context.Context) (map[string]bool, error) { return map[string]bool{}, nil }
	useBuiltinLauncher(t, service)
	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(filepath.Join(folder, "Mods"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableImporter(ctx, "GIMI", folder); err != nil {
		t.Fatal(err)
	}
	return service, folder
}

func writeModFolderFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[TextureOverride]"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sameFolder compares physical identity, since a resolved link target and a temporary folder may spell the same
// folder differently, such as with a short 8.3 name.
func sameFolder(a, b string) bool {
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}

func TestSetImporterModFolderMovesRelinksAndUnlinks(t *testing.T) {
	ctx := context.Background()
	service, folder := newModFolderTestService(t)
	mods := filepath.Join(folder, "Mods")
	writeModFolderFile(t, filepath.Join(mods, "Hero", "mod.ini"))
	first := filepath.Join(t.TempDir(), "Library")

	result, err := service.SetImporterModFolder(ctx, "GIMI", first)
	if err != nil {
		t.Fatal(err)
	}
	if !sameFolder(result.LinkTarget, first) || result.TargetMissing {
		t.Fatalf("moved folder = %+v, want link to %q", result, first)
	}
	if !fileExists(filepath.Join(first, "Hero", "mod.ini")) || !fileExists(filepath.Join(mods, "Hero", "mod.ini")) {
		t.Fatal("mods were not moved to the new folder and reached through Mods")
	}

	// A linked folder keeps its mods when the link moves elsewhere.
	second := t.TempDir()
	writeModFolderFile(t, filepath.Join(second, "Other", "mod.ini"))
	if result, err = service.SetImporterModFolder(ctx, "GIMI", second); err != nil {
		t.Fatal(err)
	}
	if !sameFolder(result.LinkTarget, second) || !fileExists(filepath.Join(mods, "Other", "mod.ini")) {
		t.Fatalf("relinked folder = %+v", result)
	}
	if !fileExists(filepath.Join(first, "Hero", "mod.ini")) {
		t.Fatal("relinking removed the mods of the previous target")
	}

	if result, err = service.SetImporterModFolder(ctx, "GIMI", ""); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(mods)
	if result.LinkTarget != "" || err != nil || len(entries) != 0 {
		t.Fatalf("unlinked folder = %+v, entries = %v, err = %v", result, entries, err)
	}
	if !fileExists(filepath.Join(second, "Other", "mod.ini")) {
		t.Fatal("unlinking removed the mods of the link target")
	}
}

func TestSetImporterModFolderCopiesAcrossVolumesAndRemovesSource(t *testing.T) {
	ctx := context.Background()
	service, folder := newModFolderTestService(t)
	service.renameUserData = crossVolumeRename
	mods := filepath.Join(folder, "Mods")
	writeModFolderFile(t, filepath.Join(mods, "Hero", "mod.ini"))
	// A link inside Mods is carried over as a link, so removing the source must not reach its target.
	outside := t.TempDir()
	writeModFolderFile(t, filepath.Join(outside, "shared.ini"))
	if err := createJunction(filepath.Join(mods, "Shared"), outside); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "Library")

	result, err := service.SetImporterModFolder(ctx, "GIMI", target)
	if err != nil || len(result.Warnings) != 0 {
		t.Fatalf("cross-volume move = %+v, %v", result, err)
	}
	if !sameFolder(result.LinkTarget, target) || !fileExists(filepath.Join(mods, "Hero", "mod.ini")) {
		t.Fatalf("copied folder = %+v", result)
	}
	if !fileExists(filepath.Join(outside, "shared.ini")) {
		t.Fatal("removing the copied source deleted the target of a link inside it")
	}
	leftovers, err := filepath.Glob(filepath.Join(folder, "Mods.*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("source left behind = %v, %v", leftovers, err)
	}
}

func TestSetImporterModFolderLinksPhysicalPathOfAlias(t *testing.T) {
	ctx := context.Background()
	service, folder := newModFolderTestService(t)
	mods := filepath.Join(folder, "Mods")
	external := t.TempDir()
	if _, err := service.SetImporterModFolder(ctx, "GIMI", external); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "Alias")
	if err := createJunction(alias, mods); err != nil {
		t.Fatal(err)
	}

	// Alias\Sub reaches external\Sub only through Mods, which the change replaces.
	result, err := service.SetImporterModFolder(ctx, "GIMI", filepath.Join(alias, "Sub"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(mods); err != nil {
		t.Fatalf("Mods is unreadable after relinking through an alias: %v", err)
	}
	if !sameFolder(result.LinkTarget, filepath.Join(external, "Sub")) {
		t.Fatalf("link target = %q, want %q", result.LinkTarget, filepath.Join(external, "Sub"))
	}
}

func TestSetImporterModFolderRejectsUnsafeTargets(t *testing.T) {
	ctx := context.Background()
	service, folder := newModFolderTestService(t)
	mods := filepath.Join(folder, "Mods")
	writeModFolderFile(t, filepath.Join(mods, "Hero", "mod.ini"))
	occupied := t.TempDir()
	writeModFolderFile(t, filepath.Join(occupied, "Other", "mod.ini"))

	for name, test := range map[string]struct {
		target string
		want   error
	}{
		"both hold mods":      {occupied, errModFolderTargetNotEmpty},
		"inside the importer": {filepath.Join(folder, "Library"), errModFolderInvalidTarget},
		"relative":            {"Library", errModFolderInvalidTarget},
	} {
		if _, err := service.SetImporterModFolder(ctx, "GIMI", test.target); !errors.Is(err, test.want) {
			t.Fatalf("%s: error = %v, want %v", name, err, test.want)
		}
	}
	info, err := os.Lstat(mods)
	if err != nil || isInstallReparsePoint(info) || !fileExists(filepath.Join(mods, "Hero", "mod.ini")) {
		t.Fatalf("rejected change touched Mods: %v, %v", info, err)
	}
}
