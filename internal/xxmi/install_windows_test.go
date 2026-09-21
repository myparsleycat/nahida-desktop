//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

func TestExecuteXcmdDeletesRejectsJunctionInDeletionPath(t *testing.T) {
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

	err := executeXcmdDeletes(commandRoot, importerRoot, "PreInstall")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "reparse point") {
		t.Fatalf("err = %v, want reparse-point rejection", err)
	}
	assertFile(t, victim, "keep")
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

func createXXMIJunction(t *testing.T, target, link string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
}
