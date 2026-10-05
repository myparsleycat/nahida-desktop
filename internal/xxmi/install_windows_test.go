//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

func TestInstallImporterPackageLogsValidationFailure(t *testing.T) {
	var output bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &output, DisableFile: true})
	service := NewWithOptions(Options{Log: log})

	err := service.InstallImporterPackage(
		context.Background(),
		InstallImporterPackageInput{Importer: "GIMI", Version: ""},
	)
	if err == nil {
		t.Fatal("expected invalid version error")
	}
	logged := output.String()
	for _, expected := range []string{
		`"operation":"install-importer-package"`,
		`"stage":"validate-input"`,
		`"importer":"GIMI"`,
		`"rollback":"not-started"`,
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("log does not contain %q: %s", expected, logged)
		}
	}
}

func TestExecuteXcmdDeletesSkipsJunctionInDeletionPath(t *testing.T) {
	root := t.TempDir()
	commandRoot := filepath.Join(root, "command")
	importerRoot := filepath.Join(root, "importer")
	outside := filepath.Join(root, "outside")
	for _, directory := range []string{
		filepath.Join(commandRoot, "Core"), filepath.Join(importerRoot, "Core"), outside,
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	createXXMIJunction(t, outside, filepath.Join(importerRoot, "Core", "linked"))
	if err := os.WriteFile(
		filepath.Join(commandRoot, "Core", "auto_update.xcmd"),
		[]byte("[PreInstall]\ndelete = Core/linked/victim.txt\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	skipped, err := executeXcmdDeletes(commandRoot, importerRoot, "PreInstall")
	if err != nil {
		t.Fatalf("executeXcmdDeletes() error = %v, want the junction target skipped", err)
	}
	if want := filepath.Join("Core", "linked", "victim.txt"); len(skipped) != 1 || skipped[0] != want {
		t.Fatalf("skipped = %v, want [%s]", skipped, want)
	}
	assertFile(t, victim, "keep")
}

func TestExecuteXcmdDeletesIgnoresMissingParentDirectory(t *testing.T) {
	importerRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(importerRoot, "Core"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(importerRoot, "Core", "auto_update.xcmd"),
		[]byte("[PreLaunch]\ndelete = Core\\GIMI\\Libraries\\Offset\\main.ini\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := executeXcmdDeletes(importerRoot, importerRoot, "PreLaunch"); err != nil {
		t.Fatalf("executeXcmdDeletes() error = %v, want nil for a missing parent directory", err)
	}
}

func TestCopyTreeFilterRejectsDestinationJunction(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	outside := filepath.Join(root, "outside")
	for _, directory := range []string{filepath.Join(source, "Core"), destination, outside} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "Core", "new.ini"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	createXXMIJunction(t, outside, filepath.Join(destination, "Core"))

	err := copyTreeFilter(source, destination, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse point") {
		t.Fatalf("err = %v, want reparse-point rejection", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.ini")); !os.IsNotExist(err) {
		t.Fatalf("outside destination was modified: %v", err)
	}
}

func TestImporterInstallTransactionRecoversInterruptedConfigFailure(t *testing.T) {
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	configPath := filepath.Join(root, xxmiConfigName)
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	importerRoot := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(importerRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importerRoot, "marker.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	transaction, err := beginImporterInstallTransaction(context.Background(), importerRoot, configPath, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	stage, err := transaction.prepare(context.Background())
	if err != nil {
		_ = transaction.Close()
		t.Fatal(err)
	}
	if err := stage.writeFileAtomic(
		context.Background(), "marker.txt", strings.NewReader("new"), 0o644, nil,
	); err != nil {
		_ = stage.Close()
		_ = transaction.Close()
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		_ = transaction.Close()
		t.Fatal(err)
	}
	if _, _, err := transaction.commit(context.Background(), []byte("{\n")); err == nil {
		_ = transaction.Close()
		t.Fatal("expected committed configuration validation to fail")
	}
	if err := transaction.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := beginImporterInstallTransaction(context.Background(), importerRoot, configPath, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(importerRoot, "marker.txt"), "old")
	restoredConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredConfig, originalConfig) {
		t.Fatal("configuration was not restored after interrupted installation")
	}
}

func TestImporterInstallTransactionWithoutExternalConfig(t *testing.T) {
	root := t.TempDir()
	importerRoot := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(importerRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importerRoot, "marker.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	transaction, err := beginImporterInstallTransaction(context.Background(), importerRoot, "", "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	stage, err := transaction.prepare(context.Background())
	if err != nil {
		_ = transaction.Close()
		t.Fatal(err)
	}
	if err := stage.writeFileAtomic(
		context.Background(),
		"marker.txt",
		strings.NewReader("new"),
		0o644,
		nil,
	); err != nil {
		_ = stage.Close()
		_ = transaction.Close()
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		_ = transaction.Close()
		t.Fatal(err)
	}
	if _, _, err := transaction.commit(context.Background(), nil); err != nil {
		_ = transaction.Close()
		t.Fatal(err)
	}
	if err := transaction.finish(); err != nil {
		_ = transaction.Close()
		t.Fatal(err)
	}
	if err := transaction.Close(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(importerRoot, "marker.txt"), "new")
}

func TestImporterInstallTransactionPreservesLinkedMods(t *testing.T) {
	root := t.TempDir()
	mods := filepath.Join(root, "real-mods")
	importerRoot := filepath.Join(root, "GIMI")
	for _, directory := range []string{filepath.Join(mods, "SomeMod"), importerRoot} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(mods, "SomeMod", "mod.ini"), []byte("mod"), 0o644); err != nil {
		t.Fatal(err)
	}
	createXXMIJunction(t, mods, filepath.Join(importerRoot, "Mods"))

	commitTestImporterInstall(t, importerRoot, func(stage *installRoot) {
		// The builtin installer creates an empty Mods folder when the stage has none.
		if err := stage.root.Mkdir("Mods", 0o755); err != nil {
			t.Fatal(err)
		}
	})

	info, err := os.Lstat(filepath.Join(importerRoot, "Mods"))
	if err != nil || !isInstallReparsePoint(info) {
		t.Fatalf("Mods = %v, err = %v, want the junction kept", info, err)
	}
	assertFile(t, filepath.Join(importerRoot, "Mods", "SomeMod", "mod.ini"), "mod")
}

func TestImporterInstallTransactionMovesModsInsteadOfCopying(t *testing.T) {
	importerRoot := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(filepath.Join(importerRoot, "Mods", "SomeMod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(importerRoot, "Mods", "SomeMod", "mod.ini"),
		[]byte("mod"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(importerRoot, "Mods"))
	if err != nil {
		t.Fatal(err)
	}

	commitTestImporterInstall(t, importerRoot, func(stage *installRoot) {
		if _, err := stage.root.Lstat("Mods"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("staged Mods err = %v, want Mods left out of the copy", err)
		}
	})

	after, err := os.Stat(filepath.Join(importerRoot, "Mods"))
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("Mods identity changed (err = %v), want the original folder moved", err)
	}
	assertFile(t, filepath.Join(importerRoot, "Mods", "SomeMod", "mod.ini"), "mod")
}

func TestImporterInstallTransactionRollbackRestoresMods(t *testing.T) {
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	importerRoot := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(filepath.Join(importerRoot, "Mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(importerRoot, "Mods", "mod.ini"), []byte("mod"), 0o644); err != nil {
		t.Fatal(err)
	}

	transaction, err := beginImporterInstallTransaction(
		context.Background(), importerRoot, filepath.Join(root, xxmiConfigName), "GIMI",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Close() }()
	stage, err := transaction.prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	// Invalid configuration fails the commit after Mods has moved into the new tree.
	if _, _, err := transaction.commit(context.Background(), []byte("{\n")); err == nil {
		t.Fatal("expected commit to fail")
	}
	if err := transaction.rollback(); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(importerRoot, "Mods", "mod.ini"), "mod")
}

func TestExecuteXcmdDeletesFollowsLinkedImporterRoot(t *testing.T) {
	root := t.TempDir()
	realImporter := filepath.Join(root, "real-importer")
	if err := os.MkdirAll(filepath.Join(realImporter, "Core"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(realImporter, "Core", "old.ini")
	if err := os.WriteFile(victim, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(realImporter, "Core", "auto_update.xcmd"), []byte("[PreLaunch]\ndelete = Core\\old.ini\n"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "GIMI")
	createXXMIJunction(t, realImporter, linked)

	if _, err := executeXcmdDeletes(linked, linked, "PreLaunch"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(victim); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("victim err = %v, want deleted through the linked importer root", err)
	}
}

func TestResolveUserFileFollowsLinkedFile(t *testing.T) {
	root := t.TempDir()
	importerRoot := filepath.Join(root, "GIMI")
	shared := filepath.Join(root, "shared")
	for _, directory := range []string{importerRoot, shared} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(shared, "real.ini"), []byte("[Loader]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(shared, "real.ini"), filepath.Join(importerRoot, "d3dx.ini")); err != nil {
		t.Skipf("file symlink creation unavailable: %v", err)
	}
	importer, err := openInstallRoot(importerRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = importer.Close() }()

	target, name, err := importer.resolveUserFile("d3dx.ini")
	if err != nil || target == nil {
		t.Fatalf("resolveUserFile() = %v, %q, %v", target, name, err)
	}
	defer func() { _ = target.Close() }()
	_, info, err := target.readFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.writeFileAtomic(
		context.Background(),
		name,
		strings.NewReader("[Loader]\nx=1\n"),
		0o644,
		info,
	); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(shared, "real.ini"), "[Loader]\nx=1\n")
	if linkInfo, err := os.Lstat(
		filepath.Join(importerRoot, "d3dx.ini"),
	); err != nil ||
		!isInstallReparsePoint(linkInfo) {
		t.Fatalf("d3dx.ini link = %v, err = %v, want the link kept", linkInfo, err)
	}
}

// commitTestImporterInstall runs a full install transaction without an external config.
func commitTestImporterInstall(t *testing.T, importerRoot string, mutate func(*installRoot)) {
	t.Helper()
	transaction, err := beginImporterInstallTransaction(context.Background(), importerRoot, "", "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Close() }()
	stage, err := transaction.prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mutate(stage)
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := transaction.commit(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if err := transaction.finish(); err != nil {
		t.Fatal(err)
	}
}

func createXXMIJunction(t *testing.T, target, link string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
}

func TestImporterInstallTransactionReportsFolderInUse(t *testing.T) {
	root := t.TempDir()
	importerRoot := filepath.Join(root, "GIMI")
	heldPath := filepath.Join(importerRoot, "Core", "held.txt")
	if err := os.MkdirAll(filepath.Dir(heldPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(heldPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An open file below the importer folder is what a running game or launcher leaves behind.
	held, err := os.Open(heldPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	transaction, err := beginImporterInstallTransaction(context.Background(), importerRoot, "", "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Close() }()
	stage, err := transaction.prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := transaction.commit(context.Background(), nil); err == nil ||
		!strings.Contains(err.Error(), "XXMI_IMPORTER_FOLDER_IN_USE") {
		t.Fatalf("commit over an open importer folder = %v", err)
	}
	if err := transaction.rollback(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, heldPath, "old")
}

func TestSaveImporterConfigRejectsLauncherRoot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	launcherRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(launcherRoot, xxmiConfigName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := service.EnableImporter(ctx, "GIMI", launcherRoot); err == nil ||
		!strings.Contains(err.Error(), "XXMI_IMPORTER_FOLDER_IS_LAUNCHER") {
		t.Fatalf("launcher root accepted as importer folder: %v", err)
	}
	if err := service.EnableImporter(ctx, "GIMI", filepath.Join(launcherRoot, "GIMI")); err != nil {
		t.Fatalf("importer folder below the launcher root rejected: %v", err)
	}
}
