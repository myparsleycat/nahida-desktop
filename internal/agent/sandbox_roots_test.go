package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestPromotedSandboxRoots(t *testing.T) {
	t.Parallel()

	workspace := filepath.Join(t.TempDir(), "Workspace With Spaces")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(workspace, "config.ini")
	if err := os.WriteFile(file, []byte("value=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "Other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "quoted directory with spaces",
			text: "`" + workspace + "` 폴더를 확인해줘",
			want: []string{workspace},
		},
		{
			name: "existing file exposes its parent",
			text: "edit \"" + file + "\"",
			want: []string{workspace},
		},
		{
			name: "unquoted path before prose",
			text: other + " 폴더에 파일을 만들어줘",
			want: []string{other},
		},
		{
			name: "path with an attached Korean particle",
			text: other + "를 확인해줘",
			want: []string{other},
		},
		{
			name: "missing file exposes one existing parent",
			text: "write `" + filepath.Join(workspace, "new.ini") + "`",
			want: []string{workspace},
		},
		{
			name: "multiple paths",
			text: "compare `" + workspace + "` and `" + other + "`",
			want: []string{workspace, other},
		},
		{
			name: "relative path ignored",
			text: `edit .\config.ini`,
		},
		{
			name: "URL ignored",
			text: `open https://example.com/c:/files`,
		},
		{
			name: "device namespace ignored",
			text: `inspect \\?\C:\Windows`,
		},
		{
			name: "UNC path ignored",
			text: `inspect \\server\share\folder`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			roots := promotedSandboxRoots(test.text)
			if len(roots) != len(test.want) {
				t.Fatalf("promotedSandboxRoots(%q) = %#v, want paths %#v", test.text, roots, test.want)
			}
			for index, want := range test.want {
				canonical, err := canonicalExistingDir(want)
				if err != nil {
					t.Fatal(err)
				}
				if roots[index].Path != canonical {
					t.Errorf("root %d path = %q, want %q", index, roots[index].Path, canonical)
				}
				if roots[index].ID == "" || roots[index].Name == "" {
					t.Errorf("root %d is incomplete: %#v", index, roots[index])
				}
			}
		})
	}
}

func TestPromotedSandboxRootSupportsReadWrite(t *testing.T) {
	t.Parallel()

	rootPath := filepath.Join(t.TempDir(), "External")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatal(err)
	}
	roots := promotedSandboxRoots("use `" + rootPath + "`")
	if len(roots) != 1 {
		t.Fatalf("roots = %#v", roots)
	}
	sandbox, err := NewSandbox(roots)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	if _, err := sandbox.ApplyPatch(roots[0].ID, []PatchOperation{{
		Type: "write", Path: "created.txt", Content: "hello",
	}}); err != nil {
		t.Fatal(err)
	}
	read, err := sandbox.ReadFile(roots[0].ID, "created.txt", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if read.Text != "hello" {
		t.Fatalf("read text = %q, want hello", read.Text)
	}
}

func TestSandboxRootsFromEvents(t *testing.T) {
	t.Parallel()

	first := filepath.Join(t.TempDir(), "First")
	second := filepath.Join(t.TempDir(), "Second")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(t.TempDir(), "Missing")
	payload, err := json.Marshal(map[string]any{"sandboxRoots": []SandboxRoot{
		{ID: "untrusted", Name: "First", Path: first},
		{ID: "missing", Name: "Missing", Path: missing},
		{ID: "second", Name: "", Path: second},
		{ID: "unc", Name: "Remote", Path: `\\server\share`},
		{ID: "relative", Name: "Relative", Path: `relative\path`},
	}})
	if err != nil {
		t.Fatal(err)
	}

	roots := sandboxRootsFromEvents([]db.AgentEventRow{
		{EventType: "message/assistant", Payload: string(payload)},
		{EventType: "turn/start", Payload: string(payload)},
	})
	if len(roots) != 2 {
		t.Fatalf("roots = %#v, want two available turn roots", roots)
	}
	canonicalFirst, err := canonicalExistingDir(first)
	if err != nil {
		t.Fatal(err)
	}
	canonicalSecond, err := canonicalExistingDir(second)
	if err != nil {
		t.Fatal(err)
	}
	if roots[0].ID == "untrusted" || roots[0].Path != canonicalFirst {
		t.Errorf("first root was not revalidated: %#v", roots[0])
	}
	if roots[1].Name != filepath.Base(canonicalSecond) || roots[1].Path != canonicalSecond {
		t.Errorf("second root = %#v", roots[1])
	}
}

func TestSandboxRootsFromEventsDoesNotChargeUnavailableRootsToActiveLimit(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	available := filepath.Join(base, "Available")
	if err := os.MkdirAll(available, 0o755); err != nil {
		t.Fatal(err)
	}
	stored := make([]SandboxRoot, 0, maxPromotedRootsPerSession+1)
	for index := range maxPromotedRootsPerSession {
		stored = append(stored, SandboxRoot{
			ID: fmt.Sprintf("missing-%d", index), Name: "Missing",
			Path: filepath.Join(base, fmt.Sprintf("Missing-%d", index)),
		})
	}
	stored = append(stored, SandboxRoot{ID: "available", Name: "Available", Path: available})
	payload, err := json.Marshal(map[string]any{"sandboxRoots": stored})
	if err != nil {
		t.Fatal(err)
	}
	canonicalAvailable, err := canonicalExistingDir(available)
	if err != nil {
		t.Fatal(err)
	}

	roots := sandboxRootsFromEvents([]db.AgentEventRow{{EventType: "turn/start", Payload: string(payload)}})
	if len(roots) != 1 || roots[0].Path != canonicalAvailable {
		t.Fatalf("roots = %#v, want only %q", roots, canonicalAvailable)
	}
}

func TestSandboxRootsRejectVolumeRoots(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	volume := filepath.VolumeName(base) + string(filepath.Separator)
	if !isWindowsVolumeRoot(volume) || !isWindowsVolumeRoot(strings.ToLower(volume)) || isWindowsVolumeRoot(base) {
		t.Fatalf("isWindowsVolumeRoot(%q) = %v", volume, isWindowsVolumeRoot(volume))
	}

	if roots := promotedSandboxRoots("inspect `" + volume + "`"); len(roots) != 0 {
		t.Fatalf("promoted volume root: %#v", roots)
	}
	if _, ok := existingSandboxRoot(volume); ok {
		t.Fatalf("existingSandboxRoot accepted %q", volume)
	}
	if roots := sandboxRootsFromEvents([]db.AgentEventRow{{
		EventType: "turn/start",
		Payload:   mustSandboxRootsPayload(t, SandboxRoot{ID: "volume", Name: "Volume", Path: volume}),
	}}); len(roots) != 0 {
		t.Fatalf("restored volume root: %#v", roots)
	}

	missing := volume + "nahida-missing-sandbox-root.txt"
	if roots := promotedSandboxRoots("write `" + missing + "`"); len(roots) != 0 {
		t.Fatalf("missing file at volume root promoted: %#v", roots)
	}

	child := base
	for parent := filepath.Dir(child); !isWindowsVolumeRoot(parent); parent = filepath.Dir(child) {
		if parent == child {
			t.Fatal("temp dir is not under a volume root")
		}
		child = parent
	}
	roots := promotedSandboxRoots("inspect `" + child + "`")
	if len(roots) != 1 || isWindowsVolumeRoot(roots[0].Path) {
		t.Fatalf("directory under volume root = %#v", roots)
	}

	entries, err := os.ReadDir(volume)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := filepath.Join(volume, entry.Name())
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			continue
		}
		if roots := promotedSandboxRoots("edit `" + path + "`"); len(roots) != 0 {
			t.Fatalf("file at volume root promoted: %#v", roots)
		}
		if _, ok := existingSandboxRoot(path); ok {
			t.Fatalf("existingSandboxRoot accepted volume-root file %q", path)
		}
		if restored := sandboxRootsFromEvents([]db.AgentEventRow{{
			EventType: "turn/start",
			Payload:   mustSandboxRootsPayload(t, SandboxRoot{ID: "file", Name: "File", Path: path}),
		}}); len(restored) != 0 {
			t.Fatalf("restored file at volume root: %#v", restored)
		}
		break
	}

	volumeLink := filepath.Join(base, "volume-link")
	if err := os.Symlink(volume, volumeLink); err != nil {
		t.Fatal(err)
	}
	if roots := promotedSandboxRoots("inspect `" + volumeLink + "`"); len(roots) != 0 {
		t.Fatalf("promoted volume symlink: %#v", roots)
	}
	if _, ok := existingSandboxRoot(volumeLink); ok {
		t.Fatalf("existingSandboxRoot accepted volume symlink %q", volumeLink)
	}
	if restored := sandboxRootsFromEvents([]db.AgentEventRow{{
		EventType: "turn/start",
		Payload:   mustSandboxRootsPayload(t, SandboxRoot{ID: "link", Name: "Link", Path: volumeLink}),
	}}); len(restored) != 0 {
		t.Fatalf("restored volume symlink: %#v", restored)
	}

	target := filepath.Join(base, "nested")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	normalLink := filepath.Join(base, "normal-link")
	if err := os.Symlink(target, normalLink); err != nil {
		t.Fatal(err)
	}
	roots = promotedSandboxRoots("inspect `" + normalLink + "`")
	canonical, err := canonicalExistingDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || !strings.EqualFold(roots[0].Path, canonical) {
		t.Fatalf("normal symlink roots = %#v, want %q", roots, canonical)
	}
}

func mustSandboxRootsPayload(t *testing.T, roots ...SandboxRoot) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"sandboxRoots": roots})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestMergeSandboxRootsKeepsLeastPrivilege(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "Root")
	nested := filepath.Join(root, "Nested")
	outside := filepath.Join(t.TempDir(), "Outside")
	base := []SandboxRoot{{ID: "root", Name: "Root", Path: root}}
	extra := []SandboxRoot{
		{ID: "nested", Name: "Nested", Path: nested},
		{ID: "outside", Name: "Outside", Path: outside},
	}

	merged := mergeSandboxRoots(base, extra)
	if len(merged) != 2 || merged[0].ID != "root" || merged[1].ID != "outside" {
		t.Fatalf("merged roots = %#v", merged)
	}
}

func TestWindowsPathVariantsAreBounded(t *testing.T) {
	t.Parallel()

	candidate := `C:\Root` + strings.Repeat(" suffix", maxWindowsPathVariants*2)
	variants := windowsPathVariants(candidate)
	if len(variants) != maxWindowsPathVariants {
		t.Fatalf("variant count = %d, want %d", len(variants), maxWindowsPathVariants)
	}
}

func TestServiceGetSessionRestoresAndRevertsPromotedRoots(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client, err := db.New(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(t.TempDir(), "Mods")
	external := filepath.Join(t.TempDir(), "External")
	for _, path := range []string{mods, external} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "Game", ModFolderPath: mods}); err != nil {
		t.Fatal(err)
	}
	service := New(Options{})
	if err := service.UseClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	session := db.AgentSessionRow{
		ID: "session", ScopeType: "global", Title: "Test", CreatedAt: "1", UpdatedAt: "1",
	}
	if err := client.AgentSessions.Insert(ctx, session); err != nil {
		t.Fatal(err)
	}
	promoted := promotedSandboxRoots("`" + external + "`")
	payload, err := json.Marshal(map[string]any{"text": "inspect it", "sandboxRoots": promoted})
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := client.AgentEvents.Append(ctx, db.AgentEventRow{
		SessionID: session.ID, TurnID: "turn", EventType: "turn/start", Payload: string(payload), CreatedAt: "1",
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Roots) != 2 || !containsSandboxRoot(snapshot.Roots, external) {
		t.Fatalf("snapshot roots = %#v", snapshot.Roots)
	}
	if _, err := service.RevertSession(ctx, session.ID, sequence); err != nil {
		t.Fatal(err)
	}
	reverted, err := service.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reverted.Roots) != 1 || containsSandboxRoot(reverted.Roots, external) {
		t.Fatalf("reverted roots = %#v", reverted.Roots)
	}
}

func containsSandboxRoot(roots []SandboxRoot, path string) bool {
	canonical, err := canonicalExistingDir(path)
	if err != nil {
		return false
	}
	for _, root := range roots {
		if strings.EqualFold(root.Path, canonical) {
			return true
		}
	}
	return false
}
