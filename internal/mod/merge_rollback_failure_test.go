package mod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestRollbackMergeContinuesInReverseOrderAndCollectsFailures(t *testing.T) {
	realRemove := rollbackRemovePath
	t.Cleanup(func() { rollbackRemovePath = realRemove })

	var calls []string
	rollbackRemovePath = func(path string) error {
		calls = append(calls, path)
		if path == "second" {
			return errors.New("simulated rollback failure")
		}
		return nil
	}
	actions := []mergeRollback{
		{kind: "remove", path: "first"},
		{kind: "remove", path: "second"},
		{kind: "remove", path: "third"},
	}

	failures := rollbackMerge(actions)
	if strings.Join(calls, ",") != "third,second,first" {
		t.Fatalf("rollback calls = %v", calls)
	}
	if len(failures) != 1 || failures[0].action.path != "second" || failures[0].err.Error() != "simulated rollback failure" {
		t.Fatalf("rollback failures = %#v", failures)
	}
}

func TestRollbackMergeMoveOverwritesRecreatedDestination(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "disabled.ini")
	to := filepath.Join(root, "mod.ini")
	if err := os.WriteFile(from, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("recreated"), 0o600); err != nil {
		t.Fatal(err)
	}

	if failures := rollbackMerge([]mergeRollback{{kind: "move", from: from, to: to}}); len(failures) != 0 {
		t.Fatalf("rollback failures = %#v", failures)
	}
	raw, err := os.ReadFile(to)
	if err != nil || string(raw) != "original" {
		t.Fatalf("restored destination = %q, %v", raw, err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("rollback source should be moved, stat = %v", err)
	}
}

func TestRollbackMergeRestorePassesOriginalContentAndMode(t *testing.T) {
	realWrite := rollbackWriteFile
	t.Cleanup(func() { rollbackWriteFile = realWrite })
	var gotPath string
	var gotContent []byte
	var gotMode os.FileMode
	rollbackWriteFile = func(path string, content []byte, mode os.FileMode) error {
		gotPath = path
		gotContent = append([]byte(nil), content...)
		gotMode = mode
		return nil
	}
	action := mergeRollback{
		kind: "restore", path: "original.ini", content: []byte("original"), mode: 0o640,
	}

	if failures := rollbackMerge([]mergeRollback{action}); len(failures) != 0 {
		t.Fatalf("rollback failures = %#v", failures)
	}
	if gotPath != action.path || string(gotContent) != string(action.content) || gotMode != action.mode {
		t.Fatalf("restore = path %q, content %q, mode %v", gotPath, gotContent, gotMode)
	}
}

func TestEnablePackFoldersStagesCollisionsAndRollbackRestoresNames(t *testing.T) {
	root := t.TempDir()
	disabled := filepath.Join(root, "DISABLED PackA")
	enabled := filepath.Join(root, "PackA")
	for _, path := range []string{disabled, enabled} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	disabledINI := filepath.Join(disabled, "CharA.ini")
	enabledINI := filepath.Join(enabled, "CharA.ini")
	if err := os.WriteFile(disabledINI, []byte("disabled"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(enabledINI, []byte("enabled"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := []mergeRollback{}
	packs, err := enablePackFolders([]MergePackClassification{
		{Path: disabled, PrimaryIniPath: &disabledINI},
		{Path: enabled, PrimaryIniPath: &enabledINI},
	}, &created)
	if err != nil {
		t.Fatal(err)
	}
	if packs[0].Path != enabled || packs[1].Path != filepath.Join(root, "PackA (2)") {
		t.Fatalf("enabled paths = %q, %q", packs[0].Path, packs[1].Path)
	}
	if packs[0].PrimaryIniPath == nil || *packs[0].PrimaryIniPath != filepath.Join(enabled, "CharA.ini") ||
		packs[1].PrimaryIniPath == nil || *packs[1].PrimaryIniPath != filepath.Join(root, "PackA (2)", "CharA.ini") {
		t.Fatalf("remapped INIs = %v, %v", packs[0].PrimaryIniPath, packs[1].PrimaryIniPath)
	}
	if failures := rollbackMerge(created); len(failures) != 0 {
		t.Fatalf("rollback failures = %#v", failures)
	}
	disabledText, disabledErr := os.ReadFile(disabledINI)
	enabledText, enabledErr := os.ReadFile(enabledINI)
	if disabledErr != nil || string(disabledText) != "disabled" || enabledErr != nil || string(enabledText) != "enabled" {
		t.Fatalf("restored files = disabled %q/%v, enabled %q/%v", disabledText, disabledErr, enabledText, enabledErr)
	}
	if _, err := os.Stat(filepath.Join(root, "PackA (2)")); !os.IsNotExist(err) {
		t.Fatalf("temporary enabled destination remains: %v", err)
	}
}

func TestMergeModsLogsOriginalErrorAndRollbackFailures(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "Empty1")
	second := filepath.Join(root, "Empty2")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	service := setupMergeGame(t, root)
	var logOutput bytes.Buffer
	service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &logOutput, DisableFile: true})

	realRemove := rollbackRemovePath
	t.Cleanup(func() { rollbackRemovePath = realRemove })
	failedPath := filepath.Join(root, "CharA")
	rollbackRemovePath = func(path string) error {
		if path == failedPath {
			return errors.New("simulated output cleanup failure")
		}
		return os.RemoveAll(path)
	}

	request := namespaceMergeRequest(root, false, first, second)
	request.Placement = "new_folder"
	_, err := service.MergeMods(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "NAMESPACE_MERGE_NEEDS_CHILD") {
		t.Fatalf("merge error = %v", err)
	}
	if _, statErr := os.Stat(first); statErr != nil {
		t.Fatalf("first source was not restored: %v", statErr)
	}
	if _, statErr := os.Stat(second); statErr != nil {
		t.Fatalf("second source was not restored: %v", statErr)
	}

	line := string(bytes.TrimSpace(logOutput.Bytes()))
	_, rest, ok := strings.Cut(line, "] ")
	if !ok {
		t.Fatalf("log message has no context prefix: %s", line)
	}
	rawPayload := rest
	var payload mergeFailureLog
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		t.Fatalf("merge payload = %q: %v", rawPayload, err)
	}
	if payload.Operation != "merge-mods" || payload.Stage != "execute" || payload.Error != "NAMESPACE_MERGE_NEEDS_CHILD" {
		t.Fatalf("merge payload = %#v", payload)
	}
	wantAction := failedPath
	if home, homeErr := os.UserHomeDir(); homeErr == nil && home != "" {
		wantAction = strings.ReplaceAll(failedPath, home, "%USERPROFILE%")
	}
	if len(payload.RollbackFailures) != 1 ||
		payload.RollbackFailures[0].Action != wantAction ||
		payload.RollbackFailures[0].Error != "simulated output cleanup failure" {
		t.Fatalf("rollback failure payload = %#v", payload.RollbackFailures)
	}
}
