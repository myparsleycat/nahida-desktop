package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
)

func TestSwapperReplacesExecutableOrRestoresIt(t *testing.T) {
	t.Parallel()
	errFixture := errors.New("fixture failure")
	for _, test := range []struct {
		name         string
		parentAlive  bool
		failInstall  bool
		failRestore  bool
		failLaunches int
		wantCode     int
		wantTarget   string
		wantLaunches int
		wantStaged   bool
	}{
		{name: "updated", wantCode: exitUpdated, wantTarget: "new build", wantLaunches: 1},
		{
			name: "application keeps running", parentAlive: true,
			wantCode: exitParentAlive, wantTarget: "old build", wantLaunches: 1, wantStaged: true,
		},
		{
			name: "move into place fails", failInstall: true,
			wantCode: exitSwapFailed, wantTarget: "old build", wantLaunches: 1, wantStaged: true,
		},
		{
			name: "update does not start", failLaunches: 1,
			wantCode: exitLaunchFailed, wantTarget: "old build", wantLaunches: 2,
		},
		{name: "restore fails", failInstall: true, failRestore: true, wantCode: exitBroken, wantStaged: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			target := filepath.Join(root, "nahida-desktop.exe")
			dir := filepath.Join(root, stagingPrefix+"test")
			staged := filepath.Join(dir, assetName)
			writeFixture(t, target, "old build")
			writeFixture(t, staged, "new build")
			// An original an earlier update could not delete while its image was mapped.
			writeFixture(t, target+".old-1", "older build")

			var launches []string
			code := swapper{
				waitParent: func(time.Duration) error {
					if test.parentAlive {
						return errFixture
					}
					return nil
				},
				move: func(source, destination string) error {
					if test.failInstall && source == staged ||
						test.failRestore && destination == target && strings.Contains(source, ".old-") {
						return errFixture
					}
					return platform.MoveNoReplace(source, destination)
				},
				launch: func(path string) error {
					launches = append(launches, path)
					if len(launches) <= test.failLaunches {
						return errFixture
					}
					return nil
				},
				logf: t.Logf,
			}.run(target, staged)

			if code != test.wantCode {
				t.Fatalf("exit code = %d, want %d", code, test.wantCode)
			}
			if test.wantTarget != "" {
				if content, err := os.ReadFile(target); err != nil || string(content) != test.wantTarget {
					t.Fatalf("target = %q, %v, want %q", content, err, test.wantTarget)
				}
			}
			if len(launches) != test.wantLaunches || slices.ContainsFunc(launches, func(path string) bool {
				return path != target
			}) {
				t.Fatalf("launches = %v, want %d of %s", launches, test.wantLaunches, target)
			}
			if _, err := os.Stat(staged); (err == nil) != test.wantStaged {
				t.Fatalf("staged update present = %t, want %t", err == nil, test.wantStaged)
			}
			if test.wantCode == exitUpdated {
				if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("staging directory survived the update: %v", err)
				}
			}
			if test.wantCode != exitParentAlive && test.wantCode != exitBroken {
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if strings.Contains(entry.Name(), ".old-") {
						t.Fatalf("set-aside original left behind: %s", entry.Name())
					}
				}
			}
		})
	}
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
