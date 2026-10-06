//go:build windows

package xxmi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/elevated"
)

// denyFileCreation makes folder behave like a game folder under Program Files for a standard
// user: its files stay readable, but nothing can be created in it. The deny entry is not
// inherited, so the test can still clean the folder up.
func denyFileCreation(t *testing.T, folder string) {
	t.Helper()
	const addFile, addSubdirectory = 0x2, 0x4
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(folder, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	denied, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: addFile | addSubdirectory,
		AccessMode:        windows.DENY_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(everyone),
		},
	}}, original)
	if err != nil {
		t.Fatal(err)
	}
	setDACL := func(acl *windows.ACL) error {
		return windows.SetNamedSecurityInfo(
			folder, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil,
		)
	}
	if err := setDACL(denied); err != nil {
		t.Skipf("temporary folder does not accept an access control list: %v", err)
	}
	t.Cleanup(func() {
		if err := setDACL(original); err != nil {
			t.Errorf("restore access control list of %s: %v", folder, err)
		}
	})
	if !writeDenied(folder) {
		t.Skip("temporary folder's file system does not enforce access control lists")
	}
}

// recordingPublisher keeps what a publish call was asked to do. The staged sources are read
// during the call because the staging folder is removed right after it.
type recordingPublisher struct {
	calls   int
	ops     []elevated.FileOp
	content map[string][]byte
	fail    error
}

func (p *recordingPublisher) publish(_ context.Context, ops []elevated.FileOp) error {
	p.calls++
	if p.fail != nil {
		return p.fail
	}
	if p.content == nil {
		p.content = make(map[string][]byte)
	}
	for _, op := range ops {
		p.ops = append(p.ops, op)
		if op.Kind != elevated.FileOpCopy {
			continue
		}
		data, err := os.ReadFile(op.Source)
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != op.SHA256 {
			return errors.New("staged file does not match its digest: " + op.Target)
		}
		p.content[filepath.Base(op.Target)] = data
	}
	return nil
}

func (p *recordingPublisher) publisher() *gameFilePublisher {
	return &gameFilePublisher{apply: p.publish}
}

func (p *recordingPublisher) summary() []string {
	summary := make([]string, 0, len(p.ops))
	for _, op := range p.ops {
		summary = append(summary, string(op.Kind)+" "+op.Target)
	}
	return summary
}

func TestEditGameFolderPublishesStagedChanges(t *testing.T) {
	t.Parallel()

	folder := filepath.Join(t.TempDir(), "LocalStorage")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Keep.ini", "Change.ini", "Remove.ini", "unrelated.sav"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("original "+name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	denyFileCreation(t, folder)

	publisher := &recordingPublisher{}
	owns := func(name string) bool { return filepath.Ext(name) == ".ini" }
	staging := ""
	err := editGameFolder(t.Context(), folder, owns, publisher.publisher(), func(edited string) error {
		staging = edited
		if _, err := os.Stat(filepath.Join(edited, "unrelated.sav")); !os.IsNotExist(err) {
			t.Errorf("file outside the edit was staged: %v", err)
		}
		if err := os.Remove(filepath.Join(edited, "Remove.ini")); err != nil {
			return err
		}

		// Rewriting under different casing replaces the file; it must not also remove it.
		if err := os.Remove(filepath.Join(edited, "Change.ini")); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(edited, "change.ini"), []byte("changed"), 0o600); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(edited, "Create.ini"), []byte("created"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"copy " + filepath.Join(folder, "Create.ini"),
		"copy " + filepath.Join(folder, "change.ini"),
		"remove " + filepath.Join(folder, "Remove.ini"),
	}
	if got := publisher.summary(); !slices.Equal(got, want) {
		t.Fatalf("published operations = %q, want %q", got, want)
	}
	if string(publisher.content["change.ini"]) != "changed" || string(publisher.content["Create.ini"]) != "created" {
		t.Fatalf("published content = %q", publisher.content)
	}
	if staging == folder || staging == "" {
		t.Fatalf("edit ran on %q instead of a private copy", staging)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatalf("staging folder remains: %v", err)
	}
	for _, name := range []string{"Keep.ini", "Change.ini", "Remove.ini", "unrelated.sav"} {
		if data, err := os.ReadFile(filepath.Join(folder, name)); err != nil || string(data) != "original "+name {
			t.Fatalf("game file %s = %q, err = %v", name, data, err)
		}
	}
}

func TestEditGameFolderWithoutChangesNeedsNoElevation(t *testing.T) {
	t.Parallel()

	folder := filepath.Join(t.TempDir(), "LocalStorage")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "Engine.ini"), []byte("settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	denyFileCreation(t, folder)

	publisher := &recordingPublisher{}
	always := func(string) bool { return true }
	if err := editGameFolder(t.Context(), folder, always, publisher.publisher(), func(edited string) error {
		_, err := os.ReadFile(filepath.Join(edited, "Engine.ini"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if publisher.calls != 0 {
		t.Fatalf("unchanged folder was published %d times", publisher.calls)
	}

	failed := errors.New("edit failed")
	if err := editGameFolder(t.Context(), folder, always, publisher.publisher(), func(edited string) error {
		if err := os.WriteFile(filepath.Join(edited, "Engine.ini"), []byte("partial"), 0o600); err != nil {
			return err
		}
		return failed
	}); !errors.Is(err, failed) {
		t.Fatalf("failed edit error = %v", err)
	}
	if publisher.calls != 0 {
		t.Fatal("failed edit was published")
	}
}

func TestEditGameFolderSucceedsOverPrivateCopyItCannotRemove(t *testing.T) {
	t.Parallel()

	folder := filepath.Join(t.TempDir(), "Config")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	denyFileCreation(t, folder)

	// A handle without delete sharing, as a scanner holds one, keeps the staged file in place
	// after the game folder already has the result.
	var (
		staged     windows.Handle
		stagedPath string
		cleanupErr error
	)
	t.Cleanup(func() {
		if staged != 0 {
			_ = windows.CloseHandle(staged)
			_ = os.RemoveAll(filepath.Dir(stagedPath))
		}
	})
	publish := &gameFilePublisher{
		apply: func(_ context.Context, ops []elevated.FileOp) error {
			stagedPath = ops[0].Source
			name, err := windows.UTF16PtrFromString(stagedPath)
			if err != nil {
				return err
			}
			staged, err = windows.CreateFile(
				name, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING,
				windows.FILE_ATTRIBUTE_NORMAL, 0,
			)
			return err
		},
		reportCleanup: func(err error) { cleanupErr = err },
	}

	err := editGameFolder(t.Context(), folder, func(string) bool { return true }, publish, func(edited string) error {
		return os.WriteFile(filepath.Join(edited, "Engine.ini"), []byte("settings"), 0o600)
	})
	if err != nil {
		t.Fatalf("published edit failed over its private copy: %v", err)
	}
	if staged == 0 || cleanupErr == nil {
		t.Fatalf("staged handle = %v, cleanup error = %v; want the leftover copy reported", staged, cleanupErr)
	}
}

func TestEditGameFolderEditsWritableFolderInPlace(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		publish func(*recordingPublisher) *gameFilePublisher
		deny    bool
	}{
		{name: "writable", publish: func(p *recordingPublisher) *gameFilePublisher { return p.publisher() }},
		{name: "no helper", publish: func(*recordingPublisher) *gameFilePublisher { return nil }, deny: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			folder := t.TempDir()
			if test.deny {
				denyFileCreation(t, folder)
			}
			publisher := &recordingPublisher{}
			edited := ""
			if err := editGameFolder(
				t.Context(), folder, func(string) bool { return true }, test.publish(publisher),
				func(target string) error {
					edited = target
					return nil
				},
			); err != nil {
				t.Fatal(err)
			}
			if edited != folder || publisher.calls != 0 {
				t.Fatalf("edit ran on %q with %d publishes, want %q in place", edited, publisher.calls, folder)
			}
		})
	}
}

func TestConfigureZZMIGameInWriteDeniedFolder(t *testing.T) {
	t.Parallel()

	// The settings folder does not exist yet, so the denial comes from its nearest ancestor.
	game := t.TempDir()
	denyFileCreation(t, game)
	path := filepath.Join(game, "ZenlessZoneZero_Data", "Persistent", "LocalStorage", "GENERAL_DATA.bin")

	publisher := &recordingPublisher{}
	if err := configureZZMIGame(t.Context(), game, publisher.publisher()); err != nil {
		t.Fatal(err)
	}
	if got, want := publisher.summary(), []string{"copy " + path}; !slices.Equal(got, want) {
		t.Fatalf("published operations = %q, want %q", got, want)
	}
	decoded, err := decodeSleepy(publisher.content["GENERAL_DATA.bin"], zzmiSleepyMagic)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := parseSleepyJSON(decoded)
	if err != nil {
		t.Fatal(err)
	}
	system := settings.field("SystemSettingDataMap")
	for _, id := range []string{"3", "13162", "99"} {
		if system == nil || system.field(id) == nil {
			t.Fatalf("setting %s missing from the published file", id)
		}
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("game folder was written without the helper: %v", err)
	}

	denied := errors.New("XXMI_ELEVATION_DENIED: the user declined")
	err = configureZZMIGame(t.Context(), game, (&recordingPublisher{fail: denied}).publisher())
	if !errors.Is(err, denied) {
		t.Fatalf("declined elevation error = %v", err)
	}
}

func TestConfigureWWMILocalStorageInWriteDeniedFolder(t *testing.T) {
	t.Parallel()

	game := t.TempDir()
	folder := filepath.Join(game, "Client", "Saved", "LocalStorage")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, marker := range map[string]string{"LocalStorage.db": "old", "LocalStorage_2026.db": "new"} {
		database, err := sql.Open("sqlite", filepath.Join(folder, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("CREATE TABLE LocalStorage(key text primary key, value text)"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec("INSERT INTO LocalStorage(key, value) VALUES(?, ?)", "marker", marker); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// The staged copies must keep these times, or the older database would win.
	oldTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(folder, "LocalStorage.db"), oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	denyFileCreation(t, folder)

	publisher := &recordingPublisher{}
	cfg := ImporterConfig{ConfigureGame: true, WWMI: &WWMIOptions{}}
	if err := configureWWMILocalStorage(t.Context(), game, cfg, true, publisher.publisher()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"copy " + filepath.Join(folder, "LocalStorage.db"),
		"remove " + filepath.Join(folder, "LocalStorage_2026.db"),
	}
	if got := publisher.summary(); !slices.Equal(got, want) {
		t.Fatalf("published operations = %q, want %q", got, want)
	}

	published := filepath.Join(t.TempDir(), "LocalStorage.db")
	if err := os.WriteFile(published, publisher.content["LocalStorage.db"], 0o600); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", published)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	values := make(map[string]string)
	for _, key := range []string{"marker", "RayTracing"} {
		var value string
		if err := database.QueryRow("SELECT value FROM LocalStorage WHERE key = ?", key).Scan(&value); err != nil {
			t.Fatalf("read %s from the published database: %v", key, err)
		}
		values[key] = value
	}
	if values["marker"] != "new" || values["RayTracing"] != "0" {
		t.Fatalf("published database values = %v", values)
	}
}
