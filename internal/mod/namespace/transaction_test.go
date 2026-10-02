package namespace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/mod/metadata"
)

func transactionChange(t *testing.T, root, relative string, before, after []byte, exists bool) Change {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	change := Change{ModPath: root, RelativePath: relative, Before: before, After: after, Exists: exists}
	if exists {
		if err := os.WriteFile(path, before, 0o644); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		info, err := file.Stat()
		closeErr := file.Close()
		if err := errors.Join(err, closeErr); err != nil {
			t.Fatal(err)
		}
		change.Info = info
	}
	return change
}

func transactionPlan(t *testing.T) []Change {
	t.Helper()
	a, b := t.TempDir(), t.TempDir()
	return []Change{
		transactionChange(t, a, "nested/a.ini", []byte("old-a\r\n"), []byte("new-a\r\n"), true),
		transactionChange(
			t,
			a,
			"nhd.json",
			[]byte(` {"unknown":{"n":1e3},"name":"old"} `),
			[]byte(` {"unknown":{"n":1e3},"name":"new"} `),
			true,
		),
		transactionChange(t, b, "b.ini", nil, []byte("new-b"), false),
	}
}

func assertTransactionBytes(t *testing.T, changes []Change, applied bool) {
	t.Helper()
	for _, change := range changes {
		data, err := os.ReadFile(filepath.Join(change.ModPath, change.RelativePath))
		if !applied && !change.Exists {
			if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("new file remains: %s: %v", change.RelativePath, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := change.Before
		if applied {
			want = change.After
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("%s = %q, want %q", change.RelativePath, data, want)
		}
	}
}

func setTransactionFault(t *testing.T, hook func(string, string) error) {
	t.Helper()
	previous := transactionFault
	transactionFault = hook
	t.Cleanup(func() { transactionFault = previous })
}

func TestTransactionCommitAndDiscovery(t *testing.T) {
	changes := transactionPlan(t)
	validateCalled, verifyCalled := false, false
	err := ApplyVerified(t.Context(), changes, func() error {
		validateCalled = true
		assertTransactionBytes(t, changes, false)
		return nil
	}, func() error {
		verifyCalled = true
		assertTransactionBytes(t, changes, true)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !validateCalled || !verifyCalled {
		t.Fatal("callbacks not called")
	}
	assertTransactionBytes(t, changes, true)
	for _, root := range []string{changes[0].ModPath, changes[2].ModPath} {
		pending, err := HasIncompleteTransactions(root)
		if pending || err != nil {
			t.Fatalf("completed journal discovery: %v %v", pending, err)
		}
		entries, err := os.ReadDir(filepath.Join(root, transactionDirectory))
		if err != nil || len(entries) != 1 {
			t.Fatalf("durable backup missing: %v %v", entries, err)
		}
		local, err := loadJournal(root, entries[0].Name())
		if err != nil {
			t.Fatal(err)
		}
		if len(local.journal.Participants) != 2 || local.journal.Status != "committed" {
			t.Fatalf("journal: %#v", local.journal)
		}
		for i, file := range local.journal.Files {
			if _, err := readBackup(local, i, "before", file.BeforeHash); err != nil {
				t.Fatal(err)
			}
			if _, err := readBackup(local, i, "after", file.AfterHash); err != nil {
				t.Fatal(err)
			}
		}
	}
	results, err := Recover(t.Context(), []string{changes[0].ModPath, changes[2].ModPath})
	if err != nil || len(results) != 0 {
		t.Fatalf("completed transactions recovered: %v %v", results, err)
	}
	for _, path := range []string{filepath.Join(changes[0].ModPath, "nhd.json"), filepath.Join(changes[0].ModPath, ".nhd-namespace")} {
		ptr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		attrs, err := windows.GetFileAttributes(ptr)
		if err != nil || attrs&windows.FILE_ATTRIBUTE_HIDDEN == 0 {
			t.Fatalf("not hidden: %s %v", path, err)
		}
	}
}

func TestTransactionEveryDurableBoundary(t *testing.T) {
	// Discover actual IO boundaries in a successful multi-mod commit, then fail
	// each one exactly once. The rollback must preserve raw metadata and missing files.
	stages := make([]string, 0)
	setTransactionFault(t, func(stage, path string) error { stages = append(stages, stage); return nil })
	if err := Apply(t.Context(), transactionPlan(t), nil); err != nil {
		t.Fatal(err)
	}
	transactionFault = func(stage, path string) error { return nil }
	failure := errors.New("injected IO failure")
	for index, stage := range stages {
		t.Run(fmt.Sprintf("%03d_%s", index, stage), func(t *testing.T) {
			changes := transactionPlan(t)
			calls := 0
			setTransactionFault(t, func(stage, path string) error {
				current := calls
				calls++
				if current == index {
					return failure
				}
				return nil
			})
			err := Apply(t.Context(), changes, nil)
			if !errors.Is(err, failure) {
				t.Fatalf("fault did not propagate: %v", err)
			}
			assertTransactionBytes(t, changes, false)
		})
	}
}

func TestTransactionCancellationAndValidation(t *testing.T) {
	for _, at := range []string{"before", "validate", "second-file", "verify", "commit"} {
		t.Run(at, func(t *testing.T) {
			changes := transactionPlan(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if at == "before" {
				cancel()
			}
			setTransactionFault(t, func(stage, path string) error {
				if at == "commit" && stage == "journal-committed" {
					cancel()
				}
				if at == "second-file" && stage == "apply" && filepath.Base(path) == "nhd.json" {
					cancel()
				}
				return nil
			})
			err := ApplyVerified(ctx, changes, func() error {
				if at == "validate" {
					cancel()
				}
				return nil
			}, func() error {
				if at == "verify" {
					cancel()
				}
				return nil
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			assertTransactionBytes(t, changes, false)
		})
	}
	for _, post := range []bool{false, true} {
		changes := transactionPlan(t)
		failure := errors.New("invalid graph")
		validate := func() error {
			if !post {
				return failure
			}
			return nil
		}
		verify := func() error {
			if post {
				return failure
			}
			return nil
		}
		if err := ApplyVerified(t.Context(), changes, validate, verify); !errors.Is(err, failure) {
			t.Fatal(err)
		}
		assertTransactionBytes(t, changes, false)
	}
}

func TestTransactionSnapshotConflicts(t *testing.T) {
	for _, kind := range []string{"contents", "identity", "missing", "callback-edit", "duplicate", "mode"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			change := transactionChange(t, root, "a.ini", []byte("before"), []byte("after"), kind != "missing")
			path := filepath.Join(root, "a.ini")
			changes := []Change{change}
			switch kind {
			case "contents", "missing":
				if err := os.WriteFile(path, []byte("external"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "identity":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, change.Before, 0o644); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				changes = append(changes, change)
			case "mode":
				if err := os.Chmod(path, 0o444); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
			}
			err := Apply(t.Context(), changes, func() error {
				if kind == "callback-edit" {
					return os.WriteFile(path, []byte("external"), 0o644)
				}
				return nil
			})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("conflict not detected: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(data, change.After) {
				t.Fatal("conflicting file overwritten")
			}
		})
	}
}

func TestTransactionReadonlyPermission(t *testing.T) {
	root := t.TempDir()
	change := transactionChange(t, root, "a.ini", []byte("before"), []byte("after"), true)
	path := filepath.Join(root, "a.ini")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o666) })
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	change.Info = info
	if err := Apply(t.Context(), []Change{change}, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("read-only file accepted: %v", err)
	}
	assertTransactionBytes(t, []Change{change}, false)
	info, err = os.Stat(path)
	if err != nil || info.Mode().Perm() != change.Info.Mode().Perm() {
		t.Fatalf("mode: %v %v", info, err)
	}
}

func crashTransaction(t *testing.T, changes []Change, afterWrites int) []*localTransaction {
	t.Helper()
	locals, _, err := groupChanges(changes)
	if err != nil {
		t.Fatal(err)
	}
	for _, local := range locals {
		if err := prepareTransaction(local); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for _, local := range locals {
		for i := range local.journal.Files {
			if count == afterWrites {
				return locals
			}
			if err := applyFile(t.Context(), local, i); err != nil {
				t.Fatal(err)
			}
			count++
		}
	}
	return locals
}

func TestTransactionCrashRecoveryEachApplication(t *testing.T) {
	for count := range 4 {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			changes := transactionPlan(t)
			crashTransaction(t, changes, count)
			for _, root := range []string{changes[0].ModPath, changes[2].ModPath} {
				pending, err := HasIncompleteTransactions(root)
				if !pending || err != nil {
					t.Fatalf("pending discovery: %v %v", pending, err)
				}
			}
			results, err := Recover(t.Context(), []string{changes[0].ModPath, changes[2].ModPath})
			if err != nil || len(results) != 2 {
				t.Fatalf("recovery: %v %v", results, err)
			}
			assertTransactionBytes(t, changes, false)
			results, err = Recover(t.Context(), []string{changes[0].ModPath, changes[2].ModPath})
			if err != nil || len(results) != 0 {
				t.Fatalf("repeat recovery: %v %v", results, err)
			}
		})
	}
}

func TestTransactionRollbackFailureRetainsJournal(t *testing.T) {
	changes := transactionPlan(t)
	failure := errors.New("rollback fault")
	setTransactionFault(t, func(stage, path string) error {
		if stage == "rollback" {
			return failure
		}
		return nil
	})
	err := ApplyVerified(t.Context(), changes, nil, func() error { return errors.New("verify failed") })
	if !errors.Is(err, ErrRecovery) || !errors.Is(err, failure) {
		t.Fatalf("rollback error: %v", err)
	}
	for _, root := range []string{changes[0].ModPath, changes[2].ModPath} {
		pending, err := HasIncompleteTransactions(root)
		if !pending || err != nil {
			t.Fatalf("lost journal: %v %v", pending, err)
		}
	}
	transactionFault = func(stage, path string) error { return nil }
	if _, err := Recover(t.Context(), []string{changes[0].ModPath, changes[2].ModPath}); err != nil {
		t.Fatal(err)
	}
	assertTransactionBytes(t, changes, false)
}

func TestTransactionRecoveryProtectsExternalEdits(t *testing.T) {
	for _, kind := range []string{"bytes", "identity", "removed", "rollback-race", "new-file-edit"} {
		t.Run(kind, func(t *testing.T) {
			changes := transactionPlan(t)
			crashTransaction(t, changes, 3)
			change := changes[0]
			if kind == "new-file-edit" {
				change = changes[2]
			}
			path := filepath.Join(change.ModPath, change.RelativePath)
			switch kind {
			case "bytes", "new-file-edit":
				if err := os.WriteFile(path, []byte("external"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "identity":
				if err := os.Rename(path, path+".external"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, change.After, 0o644); err != nil {
					t.Fatal(err)
				}
			case "removed":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "rollback-race":
				setTransactionFault(t, func(stage, target string) error {
					if stage == "rollback" && target == path {
						return os.WriteFile(path, []byte("external"), 0o644)
					}
					return nil
				})
			}
			_, err := Recover(t.Context(), []string{change.ModPath})
			if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrConflict) {
				t.Fatalf("unsafe recovery: %v", err)
			}
			data, readErr := os.ReadFile(path)
			if kind != "removed" && (readErr != nil || bytes.Equal(data, change.Before)) {
				t.Fatalf("external edit overwritten: %q %v", data, readErr)
			}
			pending, _ := HasIncompleteTransactions(change.ModPath)
			if !pending {
				t.Fatal("failed recovery journal discarded")
			}
		})
	}
}

func TestTransactionMovedAndCopiedLocalRecovery(t *testing.T) {
	for _, copied := range []bool{false, true} {
		t.Run(fmt.Sprintf("copy=%v", copied), func(t *testing.T) {
			changes := transactionPlan(t)
			crashTransaction(t, changes, 3)
			original := changes[0].ModPath
			moved := filepath.Join(t.TempDir(), "relocated")
			if copied {
				err := filepath.WalkDir(original, func(path string, entry os.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					relative, err := filepath.Rel(original, path)
					if err != nil {
						return err
					}
					target := filepath.Join(moved, relative)
					if entry.IsDir() {
						return os.MkdirAll(target, 0o755)
					}
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					return os.WriteFile(target, data, 0o644)
				})
				if err != nil {
					t.Fatal(err)
				}
			} else if err := os.Rename(original, moved); err != nil {
				t.Fatal(err)
			}
			// Neither the original path nor the other participant is consulted.
			results, err := Recover(t.Context(), []string{moved})
			if err != nil || len(results) != 1 {
				t.Fatalf("local recovery: %v %v", results, err)
			}
			for _, change := range changes[:2] {
				change.ModPath = moved
				assertTransactionBytes(t, []Change{change}, false)
			}
			data, err := os.ReadFile(filepath.Join(changes[2].ModPath, changes[2].RelativePath))
			if err != nil || !bytes.Equal(data, changes[2].After) {
				t.Fatalf("other participant touched: %q %v", data, err)
			}
		})
	}
}

func TestTransactionInvalidJournalsAndPaths(t *testing.T) {
	for _, kind := range []string{"json", "version", "status", "traversal", "absolute", "ads", "hash", "missing-backup", "duplicate", "reserved"} {
		t.Run(kind, func(t *testing.T) {
			changes := transactionPlan(t)
			local := crashTransaction(t, changes, 1)[0]
			journalPath := filepath.Join(local.dir, "journal.json")
			switch kind {
			case "json":
				if err := os.WriteFile(journalPath, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "version":
				local.journal.Version = 99
			case "status":
				local.journal.Status = "unknown"
			case "traversal":
				local.journal.Files[0].Path = "../victim.ini"
			case "absolute":
				local.journal.Files[0].Path = "C:/victim.ini"
			case "ads":
				local.journal.Files[0].Path = "a.ini:secret"
			case "hash":
				if err := os.WriteFile(local.backup(0, "before"), []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing-backup":
				if err := os.Remove(local.backup(0, "before")); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				local.journal.Files = append(local.journal.Files, local.journal.Files[0])
			case "reserved":
				local.journal.Files[0].Path = ".nhd-namespace/journal.json"
			}
			if kind != "json" {
				data, err := json.Marshal(local.journal)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(journalPath, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Recover(t.Context(), []string{local.root})
			if !errors.Is(err, ErrRecovery) || !errors.Is(err, ErrJournal) {
				t.Fatalf("invalid journal accepted: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(changes[0].ModPath, changes[0].RelativePath))
			if err != nil || !bytes.Equal(data, changes[0].After) {
				t.Fatalf("corrupt record restored: %q %v", data, err)
			}
			pending, discoveryErr := HasIncompleteTransactions(local.root)
			if !pending {
				t.Fatal("corrupt transaction treated as finished")
			}
			if kind != "hash" && kind != "missing-backup" && discoveryErr == nil {
				t.Fatal("discovery hid invalid journal")
			}
		})
	}
	for _, relative := range []string{"../a.ini", "C:/a.ini", "a.ini:stream", "x/../a.ini", "a./file", ".nhd-namespace/a.ini", "NUL", "a?b.ini"} {
		if err := Apply(
			t.Context(),
			[]Change{{ModPath: t.TempDir(), RelativePath: relative, After: []byte("new")}},
			nil,
		); err == nil {
			t.Fatalf("unsafe path allowed: %q", relative)
		}
	}
}

func TestTransactionWindowsLinks(t *testing.T) {
	for _, kind := range []string{"file", "directory", "backup", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			change := transactionChange(t, root, "nested/a.ini", []byte("before"), []byte("after"), true)
			path := filepath.Join(root, change.RelativePath)
			victim := filepath.Join(outside, "victim.ini")
			if err := os.WriteFile(victim, []byte("before"), 0o644); err != nil {
				t.Fatal(err)
			}
			var linkErr error
			switch kind {
			case "file", "hardlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if kind == "file" {
					linkErr = os.Symlink(victim, path)
				} else {
					linkErr = os.Link(victim, path)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				linkErr = os.Symlink(outside, filepath.Dir(path))
			case "backup":
				local := crashTransaction(t, []Change{change}, 1)[0]
				backup := local.backup(0, "before")
				if err := os.Remove(backup); err != nil {
					t.Fatal(err)
				}
				linkErr = os.Symlink(victim, backup)
			}
			if linkErr != nil {
				if strings.Contains(linkErr.Error(), "privilege") ||
					errors.Is(linkErr, windows.ERROR_PRIVILEGE_NOT_HELD) {
					t.Skip("Windows symlink privilege unavailable")
				}
				t.Fatal(linkErr)
			}
			var err error
			if kind == "backup" {
				_, err = Recover(t.Context(), []string{root})
			} else {
				err = Apply(t.Context(), []Change{change}, nil)
			}
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("link accepted: %v", err)
			}
			data, err := os.ReadFile(victim)
			if err != nil || string(data) != "before" {
				t.Fatalf("link target changed: %q %v", data, err)
			}
		})
	}
}

func TestTransactionMetadataReservation(t *testing.T) {
	root := t.TempDir()
	change := transactionChange(t, root, "nhd.json", []byte(`{"unknown":1}`), []byte(`{"unknown":2}`), true)
	started, done := make(chan struct{}), make(chan error, 1)
	err := ApplyVerified(t.Context(), []Change{change}, func() error {
		go func() {
			close(started)
			done <- metadata.Update(root, func(data []byte) ([]byte, error) { return append(data, '\n'), nil })
		}()
		<-started
		return nil
	}, func() error {
		select {
		case err := <-done:
			return errors.Join(errors.New("metadata queue released before commit"), err)
		default:
			return nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "nhd.json"))
	if err != nil || !bytes.Equal(data, append(bytes.Clone(change.After), '\n')) {
		t.Fatalf("metadata lost update: %q %v", data, err)
	}
}

func TestTransactionMixedCommitRecovery(t *testing.T) {
	changes := transactionPlan(t)
	locals := crashTransaction(t, changes, 3)
	locals[0].journal.Status = "committed"
	if err := saveJournal(locals[0]); err != nil {
		t.Fatal(err)
	}
	results, err := Recover(t.Context(), []string{locals[0].root, locals[1].root})
	if err != nil || len(results) != 2 {
		t.Fatalf("mixed commit recovery: %v %v", results, err)
	}
	assertTransactionBytes(t, changes, false)
}

func TestTransactionRecoveryEveryDurableBoundary(t *testing.T) {
	changes := transactionPlan(t)
	crashTransaction(t, changes, 3)
	stages := make([]string, 0)
	setTransactionFault(t, func(stage, path string) error { stages = append(stages, stage); return nil })
	if _, err := Recover(t.Context(), []string{changes[0].ModPath, changes[2].ModPath}); err != nil {
		t.Fatal(err)
	}
	transactionFault = func(stage, path string) error { return nil }
	failure := errors.New("recovery IO fault")
	for index, stage := range stages {
		t.Run(fmt.Sprintf("%03d_%s", index, stage), func(t *testing.T) {
			changes := transactionPlan(t)
			crashTransaction(t, changes, 3)
			calls := 0
			setTransactionFault(t, func(stage, path string) error {
				current := calls
				calls++
				if current == index {
					return failure
				}
				return nil
			})
			_, err := Recover(t.Context(), []string{changes[0].ModPath, changes[2].ModPath})
			if !errors.Is(err, ErrRecovery) || !errors.Is(err, failure) {
				t.Fatalf("recovery failure: %v", err)
			}
			transactionFault = func(stage, path string) error { return nil }
			if _, err := Recover(t.Context(), []string{changes[0].ModPath, changes[2].ModPath}); err != nil {
				t.Fatal(err)
			}
			assertTransactionBytes(t, changes, false)
		})
	}
}

func TestTransactionReplaceRaceAndAbortedPreparation(t *testing.T) {
	changes := transactionPlan(t)
	path := filepath.Join(changes[0].ModPath, changes[0].RelativePath)
	setTransactionFault(t, func(stage, target string) error {
		if stage == "replace" && target == path {
			return os.WriteFile(path, []byte("external"), 0o644)
		}
		return nil
	})
	err := Apply(t.Context(), changes, nil)
	if !errors.Is(err, ErrConflict) || !errors.Is(err, ErrRecovery) {
		t.Fatalf("replace race: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "external" {
		t.Fatalf("external edit overwritten: %q %v", data, err)
	}
	transactionFault = func(stage, path string) error { return nil }
	changes = transactionPlan(t)
	if err := Apply(t.Context(), changes, func() error { return errors.New("validation failed") }); err == nil {
		t.Fatal("missing validation error")
	}
	for _, root := range []string{changes[0].ModPath, changes[2].ModPath} {
		pending, err := HasIncompleteTransactions(root)
		if pending || err != nil {
			t.Fatalf("aborted preparation remains: %v %v", pending, err)
		}
	}
}

func TestTransactionInterruptedPreparationIsNotPending(t *testing.T) {
	changes := transactionPlan(t)
	root := changes[0].ModPath
	// A process killed between creating the directory and saving the first journal
	// leaves backups without journal.json; no mod file was replaced yet.
	dir := filepath.Join(root, transactionDirectory, uuid.NewString())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "0.before.bin"), changes[0].Before, 0o600); err != nil {
		t.Fatal(err)
	}

	if pending, err := HasIncompleteTransactions(root); pending || err != nil {
		t.Fatalf("interrupted preparation blocks the mod: %v %v", pending, err)
	}
	if _, err := Recover(t.Context(), []string{root}); err != nil {
		t.Fatalf("interrupted preparation failed recovery: %v", err)
	}
	if err := Apply(t.Context(), changes, nil); err != nil {
		t.Fatalf("interrupted preparation blocks later transactions: %v", err)
	}
	assertTransactionBytes(t, changes, true)
}

func TestTransactionRecoveryGuardAfterMetadataWait(t *testing.T) {
	changes := transactionPlan(t)
	crashTransaction(t, changes, 3)
	root := changes[0].ModPath
	release, err := metadata.ReserveTransaction(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release() })
	var running atomic.Bool
	var checks atomic.Int32
	gameRunning := errors.New("game started while recovery waited")
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := RecoverVerified(t.Context(), []string{root, changes[2].ModPath}, func() error {
			checks.Add(1)
			if running.Load() {
				return gameRunning
			}
			return nil
		})
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("recovery bypassed reservation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if checks.Load() != 0 {
		t.Fatal("guard ran before acquiring reservations")
	}
	running.Store(true)
	release()
	// The cleanup must not release the queue twice.
	release = func() {}
	select {
	case err := <-done:
		if !errors.Is(err, gameRunning) {
			t.Fatalf("guard failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not finish after release")
	}
	assertTransactionBytes(t, changes, true)
	for _, path := range []string{root, changes[2].ModPath} {
		pending, err := HasIncompleteTransactions(path)
		if !pending || err != nil {
			t.Fatalf("blocked recovery changed journal: %v %v", pending, err)
		}
	}
}

func TestTransactionRecoveryGuardBeforeEachMod(t *testing.T) {
	changes := transactionPlan(t)
	crashTransaction(t, changes, 3)
	checks := 0
	gameRunning := errors.New("game started before second mod recovery")
	results, err := RecoverVerified(t.Context(), []string{changes[0].ModPath, changes[2].ModPath}, func() error {
		checks++
		if checks == 3 {
			return gameRunning
		}
		return nil
	})
	if !errors.Is(err, gameRunning) || len(results) != 1 || results[0].Err != nil {
		t.Fatalf("per-mod guard: results=%v error=%v", results, err)
	}
	assertTransactionBytes(t, changes[:2], false)
	assertTransactionBytes(t, changes[2:], true)
	pending, err := HasIncompleteTransactions(changes[2].ModPath)
	if !pending || err != nil {
		t.Fatalf("second mod journal changed: %v %v", pending, err)
	}
	if _, err := RecoverVerified(
		t.Context(),
		[]string{changes[0].ModPath, changes[2].ModPath},
		func() error { return nil },
	); err != nil {
		t.Fatal(err)
	}
	assertTransactionBytes(t, changes, false)
}
