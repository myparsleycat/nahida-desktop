//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func TestWriteFileAtomicPreservesUserEdits(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"unchanged", "before write", "during write"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			folder := t.TempDir()
			path := filepath.Join(folder, "d3dx.ini")
			original := "[Rendering]\ncache_shaders = 0\n"
			userEdit := "[Rendering]\ncache_shaders = 1\n"
			timestamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, timestamp, timestamp); err != nil {
				t.Fatal(err)
			}
			root, err := openInstallRoot(folder)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			_, snapshot, err := root.readFile("d3dx.ini")
			if err != nil {
				t.Fatal(err)
			}
			edit := func() {
				t.Helper()
				if err := os.WriteFile(path, []byte(userEdit), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, timestamp, timestamp); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "before write" {
				edit()
			}
			updated := original + "allow_buffer_resize = 1\n"
			var input io.Reader = strings.NewReader(updated)
			if stage == "during write" {
				input = installTestReader(func(buffer []byte) (int, error) {
					edit()
					return copy(buffer, updated), io.EOF
				})
			}
			err = root.writeFileAtomic(context.Background(), "d3dx.ini", input, 0o600, snapshot)
			want := updated
			if stage != "unchanged" {
				want = userEdit
				if err == nil || !strings.Contains(err.Error(), "content changed") {
					t.Fatalf("write error = %v; want content conflict", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			assertFile(t, path, want)
			entries, err := os.ReadDir(folder)
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary write files remain: %v, err = %v", entries, err)
			}
		})
	}
}

type installTestReader func([]byte) (int, error)

func (r installTestReader) Read(buffer []byte) (int, error) { return r(buffer) }

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

func TestImporterInstallTransactionRestoresUnreadableLiveFiles(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"restored", "declined", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			importerRoot := filepath.Join(t.TempDir(), "ZZMI")
			libraries := filepath.Join(importerRoot, "Core", "ZZMI", "Libraries")
			folder := filepath.Join(libraries, "SlotFix")
			files := []string{filepath.Join(folder, "Resources.ini"), filepath.Join(libraries, "SlotFix.ini")}
			for _, path := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("[Loader]\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			// The folder and a file inside it are denied separately, so restoring the folder only reveals the
			// file, which a later round must restore under the same consent.
			for _, path := range append([]string{folder}, files...) {
				if output, err := exec.Command("icacls", path, "/deny", "*"+current.Uid+":(RD)").
					CombinedOutput(); err != nil {
					t.Skipf("filesystem cannot deny reading: %v (%s)", err, output)
				}
				t.Cleanup(func() { _ = exec.Command("icacls", path, "/remove:d", "*"+current.Uid).Run() })
			}
			if _, err := os.ReadDir(folder); err == nil {
				t.Skip("filesystem does not enforce the read denial")
			}

			transaction, err := beginImporterInstallTransaction(context.Background(), importerRoot, "", "ZZMI")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = transaction.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var requests [][]string
			transaction.restoreAccess = func(ctx context.Context, paths []string) error {
				requests = append(requests, paths)
				switch outcome {
				case "declined":
					return errors.New("consent declined")
				case "cancelled":
					cancel()
					return ctx.Err()
				}
				for _, path := range paths {
					if output, err := exec.Command("icacls", path, "/remove:d", "*"+current.Uid).
						CombinedOutput(); err != nil {
						return fmt.Errorf("%w (%s)", err, output)
					}
				}
				return nil
			}

			stage, err := transaction.prepare(ctx)
			// In walk order, the order the copy meets them.
			want := [][]string{{folder, files[1]}, {files[0]}}
			if outcome != "restored" {
				want = want[:1]
			}
			if !slices.EqualFunc(requests, want, slices.Equal) {
				t.Fatalf("access requests = %v, want %v", requests, want)
			}
			switch outcome {
			case "declined":
				if want := "XXMI_IMPORTER_FILE_ACCESS_DENIED: " + folder; err == nil || err.Error() != want {
					t.Fatalf("prepare with access left denied = %v, want %q", err, want)
				}
			case "cancelled":
				if !infra.IsCancellationError(err) ||
					strings.Contains(err.Error(), "XXMI_IMPORTER_FILE_ACCESS_DENIED") {
					t.Fatalf("prepare cancelled during the restore = %v, want a cancellation", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
				_ = stage.Close()
				for _, path := range files {
					relative, _ := filepath.Rel(importerRoot, path)
					assertFile(
						t,
						filepath.Join(filepath.Dir(importerRoot), transaction.stageName, relative),
						"[Loader]\n",
					)
				}
			}
			if err := transaction.rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type recordingFileGateway struct {
	leases int
	ops    []elevated.FileOp
}

func (g *recordingFileGateway) Acquire(context.Context) (func(), error) {
	g.leases++
	return func() {}, nil
}

func (g *recordingFileGateway) ApplyFiles(_ context.Context, ops []elevated.FileOp) error {
	g.ops = append(g.ops, ops...)
	return nil
}

func TestImporterAccessRestorerKeepsLaunchLease(t *testing.T) {
	file := filepath.Join(t.TempDir(), "SlotFix.ini")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	gateway := &recordingLaunchHelper{}
	service := &XXMI{elevated: gateway}
	lease := elevated.NewFileLease(gateway)
	defer lease.Release()
	restore, release := service.importerAccessRestorer(lease)
	if err := restore(t.Context(), []string{file}); err != nil {
		t.Fatal(err)
	}
	release()
	if err := lease.Hold(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gateway.leases != 1 || gateway.released != 0 {
		t.Fatalf(
			"after installation: leases = %d, released = %d; want one held launch lease",
			gateway.leases,
			gateway.released,
		)
	}
}

// The helper refuses a path that Windows resolves elsewhere, so a short 8.3 alias or a junction in the
// importer path must reach it resolved.
func TestRestoreImporterAccessSendsResolvedPaths(t *testing.T) {
	folder := filepath.Join(t.TempDir(), "Libraries")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, folder).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v (%s)", err, output)
	}
	if err := os.WriteFile(filepath.Join(folder, "SlotFix.ini"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	final, err := platform.FinalPath(folder)
	if err != nil {
		t.Fatal(err)
	}

	gateway := &recordingFileGateway{}
	lease := elevated.NewFileLease(gateway)
	defer lease.Release()
	if err := (&XXMI{}).restoreImporterAccess(
		context.Background(), lease, []string{filepath.Join(link, "SlotFix.ini")},
	); err != nil {
		t.Fatal(err)
	}
	want := []elevated.FileOp{{Kind: elevated.FileOpResetAccess, Target: filepath.Join(final, "SlotFix.ini")}}
	if gateway.leases != 1 || !slices.Equal(gateway.ops, want) {
		t.Fatalf("leases = %d, ops = %v; want 1 lease for %v", gateway.leases, gateway.ops, want)
	}
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
