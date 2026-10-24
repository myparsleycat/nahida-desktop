package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/elevated"
	"nahida.live/desktop/internal/platform"
)

type fakeAppDataGateway struct {
	acquired int
	ops      []elevated.FileOp
	apply    func(ops []elevated.FileOp)
}

func (g *fakeAppDataGateway) Acquire(context.Context) (func(), error) {
	g.acquired++
	return func() {}, nil
}

func (g *fakeAppDataGateway) ApplyFiles(_ context.Context, ops []elevated.FileOp) error {
	g.ops = append(g.ops, ops...)
	g.apply(ops)
	return nil
}

func TestAppDataAccessRestoreAsksBeforeResettingDeniedEntries(t *testing.T) {
	t.Parallel()

	// The root is left uncreated so that, like a root that refuses the account, it cannot be resolved
	// itself. The DB names it in different case, as NAHIDA_DB_PATH may, and still has to reuse the
	// target restored for the root.
	home := t.TempDir()
	root := filepath.Join(home, appdata.RootDirName)
	db := filepath.Join(strings.ToUpper(root), appdata.DatabaseFile)
	finalHome, err := platform.FinalPath(home)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(finalHome, appdata.RootDirName)
	wantDB := filepath.Join(wantRoot, appdata.DatabaseFile)

	for _, accept := range []bool{false, true} {
		denied := map[string]bool{strings.ToLower(root): true, strings.ToLower(db): true}
		gateway := &fakeAppDataGateway{apply: func(ops []elevated.FileOp) {
			for _, op := range ops {
				delete(denied, strings.ToLower(filepath.Join(home, strings.TrimPrefix(op.Target, finalHome))))
			}
		}}
		var asked string
		access := appDataAccess{
			denied: func(path string) bool { return denied[strings.ToLower(path)] },
			confirm: func(message string) (bool, error) {
				asked = message
				return accept, nil
			},
			files:  gateway,
			locale: func() string { return "en-US" },
		}

		restored, failure := access.restore(t.Context(), []string{root, filepath.Join(root, "logs"), db})
		if !strings.Contains(asked, wantRoot) || !strings.Contains(asked, wantDB) {
			t.Fatalf("accept=%v: prompt %q does not name the entries the restore changes", accept, asked)
		}
		if !accept {
			// Declining must not cost the user a UAC prompt.
			if failure == nil || restored != nil || gateway.acquired != 0 || len(gateway.ops) != 0 {
				t.Fatalf("declined: restored=%v failure=%v acquired=%d ops=%v",
					restored, failure, gateway.acquired, gateway.ops)
			}
			if !strings.Contains(failure.Error(), db) {
				t.Fatalf("declined: message %q does not name the denied entries", failure.Error())
			}
			continue
		}

		if failure != nil {
			t.Fatalf("restore failed: %v", failure)
		}
		want := []elevated.FileOp{
			{Kind: elevated.FileOpResetAccess, Target: wantRoot},
			{Kind: elevated.FileOpResetAccess, Target: wantDB},
		}
		if len(gateway.ops) != len(want) || gateway.ops[0] != want[0] || gateway.ops[1] != want[1] {
			t.Fatalf("ops = %+v, want the parent first and resolved targets %+v", gateway.ops, want)
		}
		if len(restored) != 2 || restored[0] != root || restored[1] != db {
			t.Fatalf("restored = %v", restored)
		}
	}
}

// The helper refuses a link, so an app data root moved elsewhere behind a junction must be reset at the
// folder it points at, and entries below it must follow. A destination that refuses the account does not
// open to be resolved and has to be reached through the link itself.
func TestResetAccessOpsTargetsJunctionDestination(t *testing.T) {
	t.Parallel()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, deny := range []bool{false, true} {
		home := t.TempDir()
		moved := filepath.Join(home, "moved")
		if err := os.Mkdir(moved, 0o700); err != nil {
			t.Fatal(err)
		}
		root := filepath.Join(home, appdata.RootDirName)
		if output, err := exec.Command("cmd", "/c", "mklink", "/J", root, moved).CombinedOutput(); err != nil {
			t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
		}
		finalMoved, err := platform.FinalPath(moved)
		if err != nil {
			t.Fatal(err)
		}
		if deny {
			if output, err := exec.Command(
				"icacls", moved, "/deny", "*"+user.User.Sid.String()+":(F)",
			).CombinedOutput(); err != nil {
				t.Skipf("filesystem cannot deny access to a folder: %v (%s)", err, output)
			}
			t.Cleanup(func() { _ = exec.Command("icacls", moved, "/reset").Run() })
			if _, err := platform.FinalPath(root); err == nil {
				t.Skip("filesystem does not enforce the denial when resolving the junction")
			}
		}

		ops, err := resetAccessOps([]string{root, filepath.Join(root, appdata.DatabaseFile)})
		if err != nil {
			t.Fatalf("deny=%v: %v", deny, err)
		}
		if len(ops) != 2 || ops[0].Target != finalMoved ||
			ops[1].Target != filepath.Join(finalMoved, appdata.DatabaseFile) {
			t.Fatalf("deny=%v: ops = %+v, want targets below %q", deny, ops, finalMoved)
		}
	}
}

// A junction root whose destination refuses the account cannot be created, so startup must restore it before
// it creates anything below the root.
func TestBootRuntimeRestoresDeniedJunctionRoot(t *testing.T) {
	t.Parallel()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	moved := filepath.Join(home, "moved")
	if err := os.Mkdir(moved, 0o700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, appdata.RootDirName)
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", root, moved).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	finalMoved, err := platform.FinalPath(moved)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(
		"icacls", moved, "/deny", "*"+user.User.Sid.String()+":(F)",
	).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot deny access to a folder: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = exec.Command("icacls", moved, "/reset").Run() })
	if !platform.AccessDenied(root) {
		t.Skip("filesystem does not enforce the denial through the junction")
	}

	gateway := &fakeAppDataGateway{apply: func(ops []elevated.FileOp) {
		for _, op := range ops {
			_ = exec.Command("icacls", op.Target, "/reset").Run()
		}
	}}
	rt := newRuntime()
	defer func() { _ = rt.Close() }()
	rt.appDataAccess = appDataAccess{
		denied:  platform.AccessDenied,
		confirm: func(string) (bool, error) { return true, nil },
		files:   gateway,
	}

	paths, err := bootRuntime(t.Context(), rt, runtimePathInput{HomeDir: home, Cwd: t.TempDir()}, nil)
	if err != nil {
		t.Fatalf("bootRuntime: %v", err)
	}
	if len(gateway.ops) != 1 || gateway.ops[0].Target != finalMoved {
		t.Fatalf("ops = %+v, want the junction destination %q", gateway.ops, finalMoved)
	}
	if info, err := os.Stat(paths.Logs); err != nil || !info.IsDir() {
		t.Fatalf("logs below the restored root: %v", err)
	}
}

// NAHIDA_DB_PATH may name the database by its 8.3 alias. A denied file does not open to be resolved, and the
// helper refuses a target that differs from the path Windows resolves, so the alias must become the long name,
// also when its folder refuses listing until the folder itself is restored.
func TestAppDataAccessRestoreTargetsLongNameOfDeniedShortAlias(t *testing.T) {
	t.Parallel()

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	deny := func(t *testing.T, path string) {
		t.Helper()
		if output, err := exec.Command(
			"icacls", path, "/deny", "*"+user.User.Sid.String()+":(F)",
		).CombinedOutput(); err != nil {
			t.Skipf("filesystem cannot deny access: %v (%s)", err, output)
		}
		t.Cleanup(func() { _ = exec.Command("icacls", path, "/reset").Run() })
		if !platform.AccessDenied(path) {
			t.Skipf("filesystem does not enforce the denial of %q", path)
		}
	}

	for _, denyFolder := range []bool{false, true} {
		home := t.TempDir()
		folder := filepath.Join(home, "data-folder")
		if err := os.Mkdir(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		db := filepath.Join(folder, "database-long-name.db")
		if err := os.WriteFile(db, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		long, err := windows.UTF16PtrFromString(db)
		if err != nil {
			t.Fatal(err)
		}
		buffer := make([]uint16, windows.MAX_PATH)
		length, err := windows.GetShortPathName(long, &buffer[0], uint32(len(buffer)))
		if err != nil || int(length) >= len(buffer) {
			t.Fatalf("GetShortPathName(%q) = %d, %v", db, length, err)
		}
		short := windows.UTF16ToString(buffer[:length])
		if strings.EqualFold(filepath.Base(short), filepath.Base(db)) {
			t.Skip("volume does not create 8.3 names")
		}
		finalHome, err := platform.FinalPath(home)
		if err != nil {
			t.Fatal(err)
		}
		wantFolder := filepath.Join(finalHome, filepath.Base(folder))
		wantDB := filepath.Join(wantFolder, filepath.Base(db))

		deny(t, db)
		if denyFolder {
			deny(t, folder)
		}

		// Like the helper, the gateway resets only a target spelled as the path Windows resolves.
		var refused []string
		gateway := &fakeAppDataGateway{apply: func(ops []elevated.FileOp) {
			for _, op := range ops {
				if op.Target != wantFolder && op.Target != wantDB {
					refused = append(refused, op.Target)
					continue
				}
				_ = exec.Command("icacls", op.Target, "/reset").Run()
			}
		}}
		access := appDataAccess{
			denied:  platform.AccessDenied,
			confirm: func(string) (bool, error) { return true, nil },
			files:   gateway,
		}

		// Startup lists the database folder as the database path spells it.
		paths := []string{filepath.Dir(short), short}
		if _, failure := access.restore(t.Context(), paths); failure != nil || len(refused) > 0 {
			t.Fatalf("denyFolder=%v: failure = %v, refused targets %v, want %q", denyFolder, failure, refused, wantDB)
		}
	}
}

// SQLite creates the -wal and -shm files next to the database, so a database outside the app data root
// needs its folder checked before it, while the root itself is not listed twice.
func TestStartupAppDataEntriesIncludeDatabaseFolder(t *testing.T) {
	t.Parallel()

	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "override.db")
	for _, test := range []struct {
		db     string
		folder string
	}{
		{db: filepath.Join(data.Root(), appdata.DatabaseFile), folder: data.Root()},
		{db: outside, folder: filepath.Dir(outside)},
	} {
		entries := startupAppDataEntries(data, test.db)
		folders := slices.IndexFunc(
			entries,
			func(entry string) bool { return platform.SamePathFold(entry, test.folder) },
		)
		db := slices.Index(entries, test.db)
		if folders < 0 || db < folders {
			t.Fatalf("entries = %v, want %q listed before %q", entries, test.folder, test.db)
		}
		if slices.ContainsFunc(
			entries[folders+1:],
			func(entry string) bool { return platform.SamePathFold(entry, test.folder) },
		) {
			t.Fatalf("entries = %v list %q twice", entries, test.folder)
		}
	}
}
