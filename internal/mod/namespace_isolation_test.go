package mod

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/mod/namespace"
	"nahida.live/desktop/internal/xxmi"
)

const namespaceMainINI = `namespace = Creator\Dress
[Constants]
global persist $dress = 0
global $active = 0
[Present]
run = CommandList\Creator\Dress\Update
[ResourceDiffuse]
filename = Textures\Dress.dds
`

const namespaceGUIINI = `namespace = Creator\Dress
[Constants]
global persist $hair = 1
[CommandListUpdate]
$\Creator\Dress\dress = 1
`

type namespaceTestSettings struct {
	testSettings
	enabled atomic.Bool
}

func (s *namespaceTestSettings) GetPersistToggles(context.Context) (bool, error) {
	return s.enabled.Load(), nil
}

type namespaceTestImporterSource struct{ folder string }

func (s namespaceTestImporterSource) GetEnabledImporters(context.Context) ([]xxmi.EnabledImporter, error) {
	return []xxmi.EnabledImporter{{Key: "GIMI", ImporterFolder: s.folder}}, nil
}

func newNamespaceTestMod(t *testing.T) (*Mod, string, *namespaceTestSettings) {
	t.Helper()
	m, root := newTestMod(t, testSettings{})
	folder := filepath.Join(root, "GIMI")
	mods := filepath.Join(folder, "Mods")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	key := "GIMI"
	if err := m.AddGame(t.Context(), "Game", mods, &key, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	settings := &namespaceTestSettings{}
	settings.enabled.Store(true)
	m.settings = settings
	m.xxmi = namespaceTestImporterSource{folder: folder}
	m.namespaceIsolation.settle = 5 * time.Millisecond
	m.UseNamespaceIsolationHooks(NamespaceIsolationHooks{
		CheckStopped:   func(context.Context, string) error { return nil },
		WithMutation:   func(_ context.Context, _ string, change func() error) error { return change() },
		SuspendPersist: func(context.Context) (func() error, error) { return func() error { return nil }, nil },
	})
	t.Cleanup(func() {
		if err := m.ServiceShutdown(); err != nil {
			t.Error(err)
		}
	})
	return m, mods, settings
}

func namespaceFixture(t *testing.T, mods, name string) string {
	t.Helper()
	path := filepath.Join(mods, "Character", name)
	namespaceWrite(t, filepath.Join(path, "main.ini"), namespaceMainINI)
	namespaceWrite(t, filepath.Join(path, "GUI", "gui.ini"), namespaceGUIINI)
	return path
}

func namespaceWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func namespaceDocument(t *testing.T, path string) namespace.Document {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document, err := namespace.Parse(content)
	if err != nil || document.Unsupported {
		t.Fatalf("Parse %s: %+v, %v", path, document, err)
	}
	return document
}

func TestNamespaceIsolationCopiesAllParticipantsAndGUI(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	paths := []string{
		namespaceFixture(t, mods, "Original"),
		namespaceFixture(t, mods, "Copy"),
		namespaceFixture(t, mods, "DISABLED Offline clone"),
	}
	namespaceWrite(t, filepath.Join(paths[0], "nhd.json"), `{"source":"custom","unknown":{"keep":true}}`)
	// Formatting, comments, and persisted defaults do not make copies different.
	namespaceWrite(
		t,
		filepath.Join(paths[1], "main.ini"),
		strings.ReplaceAll(
			strings.ReplaceAll(namespaceMainINI, "$dress = 0", "$dress=9 ; changed default"),
			"\n",
			"\r\n",
		),
	)
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("rescan: %+v %v", state, err)
	}
	names := map[string]bool{}
	ids := map[string]bool{}
	for _, path := range paths {
		doc := namespaceDocument(t, filepath.Join(path, "main.ini"))
		gui := namespaceDocument(t, filepath.Join(path, "GUI", "gui.ini"))
		if doc.Namespace == `Creator\Dress` || names[doc.Namespace] || gui.Namespace != doc.Namespace {
			t.Fatalf("not isolated: %+v %+v", doc, gui)
		}
		names[doc.Namespace] = true
		_, manifest, _, err := readNamespaceManifest(path)
		if err != nil || manifest == nil || ids[manifest.InstanceID] || len(manifest.Files) != 2 {
			t.Fatalf("manifest: %+v %v", manifest, err)
		}
		ids[manifest.InstanceID] = true
	}
	document, _, _, err := readNamespaceManifest(paths[0])
	if err != nil || !strings.Contains(string(document["unknown"]), `"keep": true`) {
		t.Fatalf("unknown metadata lost: %v %v", document, err)
	}
	if state.Checking || state.Revision == 0 {
		t.Fatalf("invalid state: %+v", state)
	}
}

func TestNamespaceIsolationSameModSharingIsNormal(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	path := namespaceFixture(t, mods, "Only")
	namespaceWrite(t, filepath.Join(path, "second.ini"), namespaceMainINI)
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("same-mod sharing: %+v %v", state, err)
	}
	if namespaceDocument(t, filepath.Join(path, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatal("noncolliding namespace changed")
	}
}

func TestNamespaceIsolationBlocksUnsafeCopies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, reason string
		change       func(*testing.T, *Mod, string, string)
	}{
		{name: "content", reason: "different_content", change: func(t *testing.T, _ *Mod, _ string, path string) {
			t.Helper()
			namespaceWrite(t, filepath.Join(path, "main.ini"), strings.ReplaceAll(namespaceMainINI, "$active = 0", "$active = 7"))
		}},
		{name: "topology", reason: "different_content", change: func(t *testing.T, _ *Mod, _ string, path string) {
			t.Helper()
			namespaceWrite(t, filepath.Join(path, "extra.ini"), "[Constants]\nglobal $extra = 1\n")
		}},
		{name: "outside reference", reason: "external_reference", change: func(t *testing.T, m *Mod, _ string, _ string) {
			t.Helper()
			source := m.xxmi.(namespaceTestImporterSource)
			namespaceWrite(t, filepath.Join(source.folder, "Core", "external.ini"), "[Present]\nrun = CommandList\\Creator\\Dress\\Update\n")
		}},
		{name: "unknown boundary", reason: "unrecognized_mod_boundary", change: func(t *testing.T, _ *Mod, mods string, _ string) {
			t.Helper()
			namespaceWrite(t, filepath.Join(mods, "unrecognized.ini"), namespaceMainINI)
		}},
		{name: "invalid encoding", reason: "incomplete_inventory", change: func(t *testing.T, m *Mod, _ string, _ string) {
			t.Helper()
			source := m.xxmi.(namespaceTestImporterSource)
			namespaceWrite(t, filepath.Join(source.folder, "Core", "unknown.ini"), string([]byte{0xff, 0x01}))
		}},
		{name: "missing root", reason: "incomplete_inventory", change: func(t *testing.T, m *Mod, _ string, _ string) {
			t.Helper()
			source := m.xxmi.(namespaceTestImporterSource)
			missing := filepath.Join(source.folder, "missing")
			key := "GIMI"
			if err := m.AddGame(t.Context(), "Missing", missing, &key, nil, nil, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "corrupt metadata", reason: "different_content", change: func(t *testing.T, _ *Mod, _ string, path string) {
			t.Helper()
			namespaceWrite(t, filepath.Join(path, "nhd.json"), "{")
		}},
		{name: "unsupported metadata", reason: "different_content", change: func(t *testing.T, _ *Mod, _ string, path string) {
			t.Helper()
			namespaceWrite(t, filepath.Join(path, "nhd.json"), `{"namespaceIsolation":{"version":99}}`)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m, mods, _ := newNamespaceTestMod(t)
			original := namespaceFixture(t, mods, "A")
			copyPath := namespaceFixture(t, mods, "B")
			test.change(t, m, mods, copyPath)
			state, err := m.RescanNamespaceIsolation(t.Context(), "")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(
				state.Conflicts,
				func(c NamespaceIsolationConflict) bool { return c.Reason == test.reason },
			) {
				t.Fatalf("want %s: %+v", test.reason, state)
			}
			if namespaceDocument(t, filepath.Join(original, "main.ini")).Namespace != `Creator\Dress` {
				t.Fatal("unsafe mod was rewritten")
			}
		})
	}
}

func TestNamespaceIsolationReCopyAndManualRename(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	b := namespaceFixture(t, mods, "B")
	if _, err := m.RescanNamespaceIsolation(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	old := namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace
	content, err := os.ReadFile(filepath.Join(a, "main.ini"))
	if err != nil {
		t.Fatal(err)
	}
	namespaceWrite(
		t,
		filepath.Join(a, "main.ini"),
		strings.ReplaceAll(string(content), "$dress = 0", "$dress = 17 ; harmless"),
	)
	c := filepath.Join(mods, "Character", "C")
	if err := copyDirectory(a, c); err != nil {
		t.Fatal(err)
	}
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("re-copy: %+v %v", state, err)
	}
	for _, path := range []string{a, c} {
		name := namespaceDocument(t, filepath.Join(path, "main.ini")).Namespace
		if name == old || strings.Count(name, "__nhd_") != 1 {
			t.Fatalf("suffix accumulated: %q", name)
		}
	}
	before := namespaceDocument(t, filepath.Join(b, "main.ini")).Namespace
	for _, relative := range []string{"main.ini", filepath.Join("GUI", "gui.ini")} {
		content, err := os.ReadFile(filepath.Join(b, relative))
		if err != nil {
			t.Fatal(err)
		}
		namespaceWrite(t, filepath.Join(b, relative), strings.ReplaceAll(string(content), before, `User\Renamed`))
	}
	if _, err := m.RescanNamespaceIsolation(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if name := namespaceDocument(t, filepath.Join(b, "main.ini")).Namespace; name != `User\Renamed` {
		t.Fatalf("manual name changed: %q", name)
	}
	_, manifest, _, err := readNamespaceManifest(b)
	if err != nil || manifest.Mappings[`User\Renamed`] != `User\Renamed` {
		t.Fatalf("stale metadata: %+v %v", manifest, err)
	}
}

func TestNamespaceIsolationWaitSettingRestartAndGetter(t *testing.T) {
	t.Parallel()
	m, mods, settings := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	var running atomic.Bool
	running.Store(true)
	c := m.namespaceIsolation
	c.poll = 10 * time.Millisecond
	c.fallback = time.Hour
	hooks := c.hooks
	hooks.CheckStopped = func(context.Context, string) error {
		if running.Load() {
			return errors.New("XXMI_GAME_RUNNING")
		}
		return nil
	}
	m.UseNamespaceIsolationHooks(hooks)
	if err := m.StartNamespaceIsolation(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := m.GetNamespaceIsolationState()
	if len(state.Conflicts) != 1 || state.Conflicts[0].Status != "waiting_for_game_exit" {
		t.Fatalf("not waiting: %+v", state)
	}
	if c.waitingGameStopped(t.Context()) {
		t.Fatal("exit poll would rescan while the game is still running")
	}
	state.Conflicts[0].ModPaths[0] = "changed"
	if m.GetNamespaceIsolationState().Conflicts[0].ModPaths[0] == "changed" {
		t.Fatal("getter leaks mutable slices")
	}
	if namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatal("getter or running scan mutated mod")
	}
	if err := m.StopNamespaceIsolation(); err != nil {
		t.Fatal(err)
	}
	running.Store(false)
	if !c.waitingGameStopped(t.Context()) {
		t.Fatal("exit poll missed the stopped game")
	}
	settings.enabled.Store(false)
	if err := m.StartNamespaceIsolation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.GetNamespaceIsolationState().Conflicts[0].Reason != "automatic_isolation_disabled" {
		t.Fatal("setting ignored")
	}
	if err := m.StopNamespaceIsolation(); err != nil {
		t.Fatal(err)
	}
	settings.enabled.Store(true)
	if err := m.StartNamespaceIsolation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(m.GetNamespaceIsolationState().Conflicts) != 0 {
		t.Fatalf("restart did not isolate: %+v", m.GetNamespaceIsolationState())
	}
}

func TestNamespaceIsolationUserFileStabilityAndOperationLock(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	source := m.xxmi.(namespaceTestImporterSource)
	userPath := filepath.Join(source.folder, "d3dx_user.ini")
	namespaceWrite(t, userPath, "[Constants]\n$\\Other\\key = 1\n")
	c := m.namespaceIsolation
	c.settle = 40 * time.Millisecond
	hooks := c.hooks
	var once sync.Once
	hooks.SuspendPersist = func(context.Context) (func() error, error) {
		once.Do(func() {
			go func() {
				time.Sleep(10 * time.Millisecond)
				_ = os.WriteFile(userPath, []byte("[Constants]\n$\\Other\\key = 2\n"), 0o644)
			}()
		})
		return func() error { return nil }, nil
	}
	m.UseNamespaceIsolationHooks(hooks)
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 1 || state.Conflicts[0].Status != "failed" {
		t.Fatalf("unstable userfile not deferred: %+v %v", state, err)
	}
	if namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatal("unstable file allowed apply")
	}
	finish := m.beginModOperation()
	done := make(chan error, 1)
	go func() { _, err := m.RescanNamespaceIsolation(t.Context(), ""); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("apply bypassed app operation: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("operation lock deadlocked")
	}
}

func TestNamespaceIsolationRecoveryStatusAndLaunch(t *testing.T) {
	t.Parallel()
	m, mods, settings := newNamespaceTestMod(t)
	path := namespaceFixture(t, mods, "Only")
	settings.enabled.Store(false)
	namespaceWrite(t, filepath.Join(path, ".nhd-namespace", "transactions", "broken", "journal.json"), "{")
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 1 || state.Conflicts[0].Status != "recovery_required" ||
		state.Conflicts[0].Reason != "unresolved_transaction" {
		t.Fatalf("recovery not blocked: %+v %v", state, err)
	}
	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err == nil {
		t.Fatal("unresolved launch allowed")
	}
	if err := m.StartNamespaceIsolation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if m.namespaceIsolation.watcher == nil || m.namespaceIsolation.cancel == nil {
		t.Fatal("domain recovery error stopped worker")
	}
	if err := os.RemoveAll(filepath.Join(path, ".nhd-namespace")); err != nil {
		t.Fatal(err)
	}
	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err != nil {
		t.Fatal(err)
	}
}

func TestNamespaceIsolationLaunchSkipsGateAndReviewDoesNotBlock(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	namespaceFixture(t, mods, "A")
	path := namespaceFixture(t, mods, "B")
	namespaceWrite(
		t,
		filepath.Join(path, "main.ini"),
		strings.ReplaceAll(namespaceMainINI, "$active = 0", "$active = 7"),
	)
	hooks := m.namespaceIsolation.hooks
	hooks.WithMutation = func(context.Context, string, func() error) error {
		t.Error("launch reacquired importer gate")
		return errors.New("gate reacquired")
	}
	m.UseNamespaceIsolationHooks(hooks)
	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err != nil {
		t.Fatalf("ordinary review blocked launch: %v", err)
	}
}

func TestNamespaceIsolationSnapshotHashIsImmutable(t *testing.T) {
	t.Parallel()
	m, _, _ := newNamespaceTestMod(t)
	source := m.xxmi.(namespaceTestImporterSource)
	path := filepath.Join(source.folder, "d3dx_user.ini")
	namespaceWrite(t, path, "[Constants]\n$\\Other\\key = 1\n")
	before, err := m.namespaceIsolation.inventories(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	hash := namespaceInventoryHash(before, "GIMI")
	namespaceWrite(t, path, "[Constants]\n$\\Other\\key = 2\n")
	after, err := m.namespaceIsolation.inventories(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if namespaceInventoryHash(before, "GIMI") != hash || namespaceInventoryHash(after, "GIMI") == hash {
		t.Fatal("snapshot was read lazily")
	}
	encoded, err := json.Marshal(m.GetNamespaceIsolationState())
	if err != nil || !strings.Contains(string(encoded), `"conflicts":[]`) {
		t.Fatalf("DTO JSON: %s %v", encoded, err)
	}
}

func TestNamespaceIsolationCaseOnlyNamespaceDifferences(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	namespaceFixture(t, mods, "A")
	path := namespaceFixture(t, mods, "B")
	for _, relative := range []string{"main.ini", filepath.Join("GUI", "gui.ini")} {
		content, err := os.ReadFile(filepath.Join(path, relative))
		if err != nil {
			t.Fatal(err)
		}
		namespaceWrite(
			t,
			filepath.Join(path, relative),
			strings.ReplaceAll(string(content), `Creator\Dress`, `creator\dress`),
		)
	}
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("case-only namespace: %+v %v", state, err)
	}
}

func TestNamespaceIsolationPendingRecoveryPollsWhileSettingOff(t *testing.T) {
	t.Parallel()
	m, mods, settings := newNamespaceTestMod(t)
	path := namespaceFixture(t, mods, "Only")
	settings.enabled.Store(false)
	content, info, err := readNamespaceFile(filepath.Join(path, "main.ini"))
	if err != nil {
		t.Fatal(err)
	}
	change := namespace.Change{
		ModPath:      path,
		RelativePath: "main.ini",
		Before:       content,
		After:        append(slices.Clone(content), []byte("; pending\n")...),
		Exists:       true,
		Info:         info,
	}
	if err := namespace.Apply(t.Context(), []namespace.Change{change}, nil); err != nil {
		t.Fatal(err)
	}
	journals, err := filepath.Glob(filepath.Join(path, ".nhd-namespace", "transactions", "*", "journal.json"))
	if err != nil || len(journals) != 1 {
		t.Fatalf("journals %v %v", journals, err)
	}
	content, err = os.ReadFile(journals[0])
	if err != nil {
		t.Fatal(err)
	}
	var journal map[string]json.RawMessage
	if err := json.Unmarshal(content, &journal); err != nil {
		t.Fatal(err)
	}
	journal["status"] = json.RawMessage(`"pending"`)
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	namespaceWrite(t, journals[0], string(encoded))
	var running atomic.Bool
	running.Store(true)
	hooks := m.namespaceIsolation.hooks
	hooks.CheckStopped = func(context.Context, string) error {
		if running.Load() {
			return errors.New("XXMI_GAME_RUNNING")
		}
		return nil
	}
	m.UseNamespaceIsolationHooks(hooks)
	m.namespaceIsolation.poll = 10 * time.Millisecond
	m.namespaceIsolation.fallback = time.Hour
	if err := m.StartNamespaceIsolation(t.Context()); err != nil {
		t.Fatal(err)
	}
	state := m.GetNamespaceIsolationState()
	if m.namespaceIsolation.cancel == nil || len(state.Conflicts) != 1 ||
		state.Conflicts[0].Status != "waiting_for_game_exit" {
		t.Fatalf("pending startup did not wait with live worker: %+v", state)
	}
	running.Store(false)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pending, err := namespace.HasIncompleteTransactions(
			path,
		); err == nil && !pending &&
			len(m.GetNamespaceIsolationState().Conflicts) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pending transaction not recovered by exit poll: %+v", m.GetNamespaceIsolationState())
}

func TestNamespaceIsolationRecursiveWatchCoalescesOfflineClone(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	m.namespaceIsolation.fallback = time.Hour
	if err := m.StartNamespaceIsolation(t.Context()); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(mods, "Character", "Offline")
	if err := copyDirectory(a, b); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		m.RefreshNamespaceIsolationImporters(t.Context())
	}
	if len(m.namespaceIsolation.notify) > 1 {
		t.Fatal("notifications were not coalesced")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(filepath.Join(b, "main.ini"))
		if err == nil && strings.Contains(string(content), "__nhd_") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("external clone not isolated: %+v", m.GetNamespaceIsolationState())
}

func TestNamespaceIsolationPermanentFailuresCacheAndBusyRetries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		failure   error
		wantCalls int
	}{
		{name: "permission", failure: os.ErrPermission, wantCalls: 1},
		{name: "busy", failure: errors.New("XXMI_BUSY"), wantCalls: 2},
		{name: "unstable", failure: errors.New("namespace inventory is still changing"), wantCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m, mods, _ := newNamespaceTestMod(t)
			namespaceFixture(t, mods, "A")
			namespaceFixture(t, mods, "B")
			var calls atomic.Int32
			hooks := m.namespaceIsolation.hooks
			hooks.WithMutation = func(context.Context, string, func() error) error { calls.Add(1); return test.failure }
			m.UseNamespaceIsolationHooks(hooks)
			for range 2 {
				if err := m.namespaceIsolation.reconcile(t.Context(), "", false); err != nil {
					t.Fatal(err)
				}
			}
			if int(calls.Load()) != test.wantCalls {
				t.Fatalf("mutation attempts=%d want=%d", calls.Load(), test.wantCalls)
			}
			if _, err := m.RescanNamespaceIsolation(t.Context(), ""); err != nil {
				t.Fatal(err)
			}
			if int(calls.Load()) != test.wantCalls+1 {
				t.Fatal("public rescan did not invalidate failure cache")
			}
		})
	}
}

type namespaceMultiImporterSource struct{ importers []xxmi.EnabledImporter }

func (s namespaceMultiImporterSource) GetEnabledImporters(context.Context) ([]xxmi.EnabledImporter, error) {
	return s.importers, nil
}

func TestNamespaceIsolationSharedImporterModsAreReviewOnly(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	folder := m.xxmi.(namespaceTestImporterSource).folder
	m.xxmi = namespaceMultiImporterSource{
		importers: []xxmi.EnabledImporter{{Key: "GIMI", ImporterFolder: folder}, {Key: "ZZMI", ImporterFolder: folder}},
	}
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil ||
		!slices.ContainsFunc(
			state.Conflicts,
			func(c NamespaceIsolationConflict) bool { return c.Reason == "shared_importer_mods" },
		) {
		t.Fatalf("shared root: %+v %v", state, err)
	}
	if namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatal("shared importer mod was mutated")
	}
}

func TestNamespaceIsolationImplicitNamespaceAndDisabledINI(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	b := namespaceFixture(t, mods, "B")
	namespaceWrite(t, filepath.Join(a, "DISABLED old.ini"), "not valid INI \xff")
	source := m.xxmi.(namespaceTestImporterSource)
	namespaceWrite(t, filepath.Join(source.folder, "d3dx_user.ini"), "[Constants]\n$\\Creator\\Dress\\dress = 2\n")
	if state, err := m.RescanNamespaceIsolation(t.Context(), ""); err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("backup/userfile blocked isolation: %+v %v", state, err)
	}
	namespaceWrite(t, filepath.Join(a, "implicit.ini"), "[Constants]\nglobal persist $shared = 1\n")
	namespaceWrite(
		t,
		filepath.Join(b, "explicit.ini"),
		"namespace = Mods\\Character\\A\\implicit.ini\n[Constants]\nglobal persist $shared = 1\n",
	)
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) == 0 {
		t.Fatalf("implicit collision missed: %+v %v", state, err)
	}
	for _, conflict := range state.Conflicts {
		if conflict.Status != "needs_review" {
			t.Fatalf("implicit collision automatically changed: %+v", state)
		}
	}
}

func TestNamespaceIsolationImporterIncludeScope(t *testing.T) {
	t.Parallel()
	for _, outside := range []bool{false, true} {
		name := "Core and Mods"
		if outside {
			name = "outside"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m, mods, _ := newNamespaceTestMod(t)
			a := namespaceFixture(t, mods, "A")
			b := namespaceFixture(t, mods, "B")
			folder := m.xxmi.(namespaceTestImporterSource).folder
			for _, path := range []string{a, b} {
				content, err := os.ReadFile(filepath.Join(path, "main.ini"))
				if err != nil {
					t.Fatal(err)
				}
				namespaceWrite(
					t,
					filepath.Join(path, "main.ini"),
					string(content)+"\n[CommandListCore]\nrun = CommandList\\WWMIv1\\Update\n",
				)
			}
			core := filepath.Join(folder, "Core", "WWMI", "WuWa-Model-Importer.ini")
			namespaceWrite(t, core, "namespace = WWMI\ninclude = WWMI-Utilities.ini\n[Constants]\nglobal $active = 0\n")
			namespaceWrite(
				t,
				filepath.Join(filepath.Dir(core), "WWMI-Utilities.ini"),
				"namespace = WWMIUtilities\n[Constants]\nglobal $active = 0\n",
			)
			include := `Core\WWMI\WuWa-Model-Importer.ini`
			if outside {
				include = filepath.Join(t.TempDir(), "external.ini")
				namespaceWrite(t, include, "[Present]\nrun = CommandList\\Creator\\Dress\\Update\n")
			}
			namespaceWrite(
				t,
				filepath.Join(folder, "d3dx.ini"),
				"[Include]\ninclude = "+include+"\ninclude_recursive = Mods\n",
			)
			state, err := m.RescanNamespaceIsolation(t.Context(), "")
			if err != nil {
				t.Fatal(err)
			}
			if outside {
				if !slices.ContainsFunc(
					state.Conflicts,
					func(c NamespaceIsolationConflict) bool { return c.Reason == "include_outside_inventory" },
				) {
					t.Fatalf("outside include not blocked: %+v", state)
				}
				if namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace != `Creator\Dress` {
					t.Fatal("outside include allowed apply")
				}
			} else if len(state.Conflicts) != 0 {
				t.Fatalf("standard includes blocked: %+v", state)
			}
		})
	}
}

func TestNamespaceIsolationConnectedNamespacesAndManualGroup(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	for _, path := range []string{a, filepath.Join(mods, "Character", "B")} {
		namespaceWrite(
			t,
			filepath.Join(path, "second.ini"),
			"namespace = Second\n[Constants]\nglobal persist $other = 0\n",
		)
	}
	group := filepath.Join(mods, "Character", "Manual")
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.SetManualSubGroup(t.Context(), group, true); err != nil {
		t.Fatal(err)
	}
	c := filepath.Join(group, "C")
	if err := copyDirectory(a, c); err != nil {
		t.Fatal(err)
	}
	inventories, err := m.namespaceIsolation.inventories(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	plans := analyzeNamespaceIsolation(inventories[0])
	if len(plans) != 1 || len(plans[0].mods) != 3 {
		t.Fatalf("connected namespaces or manual group not grouped: %+v", plans)
	}
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 {
		t.Fatalf("connected transaction: %+v %v", state, err)
	}
	for _, path := range plans[0].mods {
		_, manifest, _, err := readNamespaceManifest(path)
		if err != nil || len(manifest.Mappings) != 2 {
			t.Fatalf("one instance per participant: %+v %v", manifest, err)
		}
	}
}

func TestNamespaceIsolationStopCancelsSynchronousRescan(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	m.namespaceIsolation.settle = time.Hour
	started := make(chan struct{})
	hooks := m.namespaceIsolation.hooks
	hooks.SuspendPersist = func(context.Context) (func() error, error) { close(started); return func() error { return nil }, nil }
	m.UseNamespaceIsolationHooks(hooks)
	done := make(chan error, 1)
	go func() { _, err := m.RescanNamespaceIsolation(t.Context(), ""); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("rescan did not start")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- m.StopNamespaceIsolation() }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel synchronous scan")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rescan did not drain")
	}
	if m.GetNamespaceIsolationState().Checking {
		t.Fatal("cancelled state still checking")
	}
}

type namespaceFailSettings struct{ *namespaceTestSettings }

func (s namespaceFailSettings) GetPersistToggles(context.Context) (bool, error) {
	return false, errors.New("setting read failed")
}

func TestNamespaceIsolationStartupInfrastructureFailureClosesWatcher(t *testing.T) {
	t.Parallel()
	m, _, settings := newNamespaceTestMod(t)
	m.settings = namespaceFailSettings{namespaceTestSettings: settings}
	if err := m.StartNamespaceIsolation(t.Context()); err == nil {
		t.Fatal("startup infrastructure error was ignored")
	}
	if m.namespaceIsolation.watcher != nil || m.namespaceIsolation.cancel != nil ||
		m.GetNamespaceIsolationState().Checking {
		t.Fatal("failed startup leaked lifecycle resources")
	}
	m.settings = settings
	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err != nil {
		t.Fatalf("failed startup left launch admission closed: %v", err)
	}
}

func TestNamespaceIsolationAbortedReconcileKeepsPublishedConflicts(t *testing.T) {
	t.Parallel()
	m, mods, settings := newNamespaceTestMod(t)
	namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	settings.enabled.Store(false)
	if state, err := m.RescanNamespaceIsolation(t.Context(), ""); err != nil || len(state.Conflicts) != 1 {
		t.Fatalf("collision not reported: %+v %v", state, err)
	}
	m.settings = namespaceFailSettings{namespaceTestSettings: settings}
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err == nil || len(state.Conflicts) != 1 || state.Checking {
		t.Fatalf("aborted reconcile cleared conflicts: %+v %v", state, err)
	}
}

func TestNamespaceIsolationKeyedInventoryScansOneImporter(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	namespaceFixture(t, mods, "A")
	folder := m.xxmi.(namespaceTestImporterSource).folder
	m.xxmi = namespaceMultiImporterSource{
		importers: []xxmi.EnabledImporter{{Key: "GIMI", ImporterFolder: folder}, {Key: "ZZMI", ImporterFolder: folder}},
	}
	inventories, err := m.namespaceIsolation.inventories(t.Context(), "gimi")
	if err != nil || len(inventories) != 2 {
		t.Fatalf("inventories: %+v %v", inventories, err)
	}
	scanned, other := inventories[0], inventories[1]
	if len(scanned.files) == 0 || len(other.files) != 0 || len(other.mods) != 0 || len(other.roots) == 0 {
		t.Fatalf("keyed inventory scope: scanned %+v other %+v", scanned, other)
	}
	// The unscanned importer still has to expose shared ownership of the mods.
	if !slices.ContainsFunc(
		scanned.issues,
		func(c NamespaceIsolationConflict) bool { return c.Reason == "shared_importer_mods" },
	) {
		t.Fatalf("shared ownership lost in keyed inventory: %+v", scanned.issues)
	}
}

func TestNamespaceIsolationNTEImporterIsExcluded(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	folder := m.xxmi.(namespaceTestImporterSource).folder
	m.xxmi = namespaceMultiImporterSource{importers: []xxmi.EnabledImporter{{Key: "NTE", ImporterFolder: folder}}}
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 0 ||
		namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatalf("NTE analyzed or mutated: %+v %v", state, err)
	}
}

func TestNamespaceIsolationSettingOffDuringStabilizationDoesNotWrite(t *testing.T) {
	t.Parallel()
	m, mods, settings := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	hooks := m.namespaceIsolation.hooks
	hooks.SuspendPersist = func(context.Context) (func() error, error) {
		settings.enabled.Store(false)
		return func() error { return nil }, nil
	}
	m.UseNamespaceIsolationHooks(hooks)
	if _, err := m.RescanNamespaceIsolation(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	if namespaceDocument(t, filepath.Join(a, "main.ini")).Namespace != `Creator\Dress` {
		t.Fatal("setting disable during apply was ignored")
	}
}

type namespaceCallbackImporterSource struct{ list func() []xxmi.EnabledImporter }

func (s namespaceCallbackImporterSource) GetEnabledImporters(context.Context) ([]xxmi.EnabledImporter, error) {
	return s.list(), nil
}

func TestNamespaceIsolationAbsentImporterAfterApplyRollsBack(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	b := namespaceFixture(t, mods, "B")
	folder := m.xxmi.(namespaceTestImporterSource).folder
	m.xxmi = namespaceCallbackImporterSource{list: func() []xxmi.EnabledImporter {
		content, err := os.ReadFile(filepath.Join(a, "main.ini"))
		if err == nil && strings.Contains(string(content), "__nhd_") {
			return []xxmi.EnabledImporter{}
		}
		return []xxmi.EnabledImporter{{Key: "GIMI", ImporterFolder: folder}}
	}}
	state, err := m.RescanNamespaceIsolation(t.Context(), "")
	if err != nil || len(state.Conflicts) != 1 || !strings.Contains(state.Conflicts[0].Detail, "importer is absent") {
		t.Fatalf("absent importer not rejected: %+v %v", state, err)
	}
	for _, path := range []string{a, b} {
		if namespaceDocument(t, filepath.Join(path, "main.ini")).Namespace != `Creator\Dress` {
			t.Fatal("post-verify absence did not roll back")
		}
	}
}

func TestNamespaceIsolationParticipantValidationFailsClosed(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	a := namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	inventories, err := m.namespaceIsolation.inventories(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	before := inventories[0]
	plan := analyzeNamespaceIsolation(before)[0]
	identities, err := namespaceParticipantIdentities(before, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNamespaceParticipants(inventories, before, plan, identities); err != nil {
		t.Fatal(err)
	}
	missing := before
	missing.mods = []string{a}
	if err := validateNamespaceParticipants(
		[]namespaceIsolationInventory{missing},
		before,
		plan,
		identities,
	); err == nil {
		t.Fatal("missing physical participant accepted")
	}
	missing = before
	missing.roots = []string{}
	if err := validateNamespaceParticipants(
		[]namespaceIsolationInventory{missing},
		before,
		plan,
		identities,
	); err == nil {
		t.Fatal("missing trusted root accepted")
	}
	if err := os.Rename(a, a+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateNamespaceParticipants(inventories, before, plan, identities); err == nil {
		t.Fatal("replaced physical participant accepted")
	}
}

func TestNamespaceIsolationStoppedAdmissionAvoidsMaintenanceDeadlock(t *testing.T) {
	t.Parallel()
	m, mods, _ := newNamespaceTestMod(t)
	namespaceFixture(t, mods, "A")
	namespaceFixture(t, mods, "B")
	if err := m.StopNamespaceIsolation(); err != nil {
		t.Fatal(err)
	}
	var suspended atomic.Int32
	hooks := m.namespaceIsolation.hooks
	hooks.SuspendPersist = func(context.Context) (func() error, error) { suspended.Add(1); return func() error { return nil }, nil }
	m.UseNamespaceIsolationHooks(hooks)
	// Represents the outer tools watcher lock; admission must return even if
	// reconcileMu is unavailable, so no second drain can form a lock cycle.
	m.namespaceIsolation.reconcileMu.Lock()
	done := make(chan error, 1)
	go func() { _, err := m.RescanNamespaceIsolation(t.Context(), ""); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "PAUSED") {
			t.Errorf("paused rescan: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("paused rescan waited for reconciliation")
	}
	resume, err := m.SuspendImporterWatchers(t.Context())
	m.namespaceIsolation.reconcileMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := resume(nil); err != nil {
		t.Fatal(err)
	}
	if err := m.PrepareNamespaceIsolationLaunch(t.Context(), "GIMI"); err == nil {
		t.Fatal("paused launch entered reconciliation")
	}
	if suspended.Load() != 0 {
		t.Fatal("paused admission entered persist suspension")
	}
	if err := m.StartNamespaceIsolation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(m.GetNamespaceIsolationState().Conflicts) != 0 {
		t.Fatalf("Start did not reopen admission: %+v", m.GetNamespaceIsolationState())
	}
}
