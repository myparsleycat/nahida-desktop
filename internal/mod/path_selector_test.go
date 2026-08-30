package mod

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type failingPathSelectionDialog struct {
	err error
}

type scriptedPathSelectionDialog struct {
	results []platform.DialogResult
}

func (d *scriptedPathSelectionDialog) SaveFile(platform.SaveFileOptions) (platform.DialogResult, error) {
	return d.next()
}

func (d *scriptedPathSelectionDialog) SelectDirectory() (platform.DialogResult, error) {
	return d.next()
}

func (d *scriptedPathSelectionDialog) next() (platform.DialogResult, error) {
	if len(d.results) == 0 {
		return platform.DialogResult{}, errors.New("unexpected dialog call")
	}
	result := d.results[0]
	d.results = d.results[1:]
	return result, nil
}

type asyncPathSelectionResult struct {
	result PathSelectorResult
	err    error
}

func (d failingPathSelectionDialog) SaveFile(platform.SaveFileOptions) (platform.DialogResult, error) {
	return platform.DialogResult{}, d.err
}

func (d failingPathSelectionDialog) SelectDirectory() (platform.DialogResult, error) {
	return platform.DialogResult{}, d.err
}

func TestSelectFolderPathRejectsPendingSelectionOnDialogError(t *testing.T) {
	for _, selectFile := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "file"}[selectFile], func(t *testing.T) {
			dialogErr := errors.New("dialog failed")
			selectionIDs := make(chan string, 1)
			selector := newPathSelector(
				failingPathSelectionDialog{err: dialogErr},
				nil,
				func(_ string, args ...any) {
					data, ok := args[0].(map[string]any)
					if !ok {
						t.Errorf("event data type = %T, want map[string]any", args[0])
						return
					}
					selectionID, ok := data["selectionId"].(string)
					if !ok {
						t.Errorf("selectionId type = %T, want string", data["selectionId"])
						return
					}
					selectionIDs <- selectionID
				},
				nil,
			)

			type selectionResult struct {
				result PathSelectorResult
				err    error
			}
			results := make(chan selectionResult, 1)
			go func() {
				result, err := selector.getSelectedPathWithModeModal(
					context.Background(), "preview.zip", nil, nil, "drive", nil, selectFile,
				)
				results <- selectionResult{result: result, err: err}
			}()

			var selectionID string
			select {
			case selectionID = <-selectionIDs:
			case <-time.After(time.Second):
				t.Fatal("path selection event was not emitted")
			}

			selected, err := selector.selectFolderPath(context.Background(), selectionID)
			if selected {
				t.Fatal("failed dialog unexpectedly selected a path")
			}
			if !errors.Is(err, dialogErr) {
				t.Fatalf("selectFolderPath error = %v, want %v", err, dialogErr)
			}

			select {
			case got := <-results:
				if !errors.Is(got.err, dialogErr) {
					t.Fatalf("pending selection error = %v, want %v", got.err, dialogErr)
				}
				if got.result != (PathSelectorResult{}) {
					t.Fatalf("pending selection result = %#v, want zero value", got.result)
				}
			case <-time.After(time.Second):
				t.Fatal("pending selection remained blocked after dialog error")
			}

			selector.mu.Lock()
			_, stillPending := selector.pending[selectionID]
			selector.mu.Unlock()
			if stillPending {
				t.Fatal("failed selection was not removed from the pending map")
			}
		})
	}
}

func TestModSelectFolderPathLogsTerminalError(t *testing.T) {
	dialogErr := errors.New("dialog failed")
	selector, selectionID, results := startPathSelection(
		t,
		failingPathSelectionDialog{err: dialogErr},
		true,
	)
	var output bytes.Buffer
	m := &Mod{
		paths: selector,
		log: infra.NewLogWithOptions(infra.LogOptions{
			DisableFile: true,
			Writer:      &output,
		}),
	}

	selected, err := m.SelectFolderPath(context.Background(), selectionID)
	if selected || !errors.Is(err, dialogErr) {
		t.Fatalf("SelectFolderPath() = %v, %v, want false, %v", selected, err, dialogErr)
	}
	logged := output.String()
	for _, want := range []string{
		"[Mod:SelectFolderPath]",
		`"selectionId":"` + selectionID + `"`,
		`"stage":"select_file"`,
		`"error":"dialog failed"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log %q does not contain %q", logged, want)
		}
	}
	got := awaitPathSelection(t, results)
	if !errors.Is(got.err, dialogErr) {
		t.Fatalf("pending selection error = %v, want %v", got.err, dialogErr)
	}
}

func TestSelectFolderPathRejectsUnwritablePath(t *testing.T) {
	for _, selectFile := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "file"}[selectFile], func(t *testing.T) {
			missingPath := filepath.Join(t.TempDir(), "missing")
			selectedPath := missingPath
			if selectFile {
				selectedPath = filepath.Join(missingPath, "preview.zip")
			}
			selector, selectionID, results := startPathSelection(
				t,
				&scriptedPathSelectionDialog{results: []platform.DialogResult{{FilePath: selectedPath}}},
				selectFile,
			)
			selector.fs = platform.NewFS()

			selected, err := selector.selectFolderPath(context.Background(), selectionID)
			if selected || err == nil || err.Error() != "path is not writable" {
				t.Fatalf("selectFolderPath() = %v, %v", selected, err)
			}
			got := awaitPathSelection(t, results)
			if got.err == nil || got.err.Error() != "path is not writable" {
				t.Fatalf("pending selection error = %v", got.err)
			}
			selector.mu.Lock()
			_, pending := selector.pending[selectionID]
			selector.mu.Unlock()
			if pending {
				t.Fatal("unwritable selection remained pending")
			}
		})
	}
}

func TestSelectFolderPathCancellationKeepsPendingForModManager(t *testing.T) {
	for _, selectFile := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "file"}[selectFile], func(t *testing.T) {
			selector, selectionID, results := startPathSelection(
				t,
				&scriptedPathSelectionDialog{results: []platform.DialogResult{{Canceled: true}}},
				selectFile,
			)

			selected, err := selector.selectFolderPath(context.Background(), selectionID)
			if err != nil || selected {
				t.Fatalf("selectFolderPath() = %v, %v, want false, nil", selected, err)
			}
			assertPathSelectionPending(t, selector, selectionID, results)

			path := `D:\mods\character`
			fileName := "preview.zip"
			if err := selector.selectModManagerPath(selectionID, path, &fileName); err != nil {
				t.Fatalf("selectModManagerPath: %v", err)
			}
			got := awaitPathSelection(t, results)
			if got.err != nil || got.result.Mode != "modManager" || got.result.Path == nil || *got.result.Path != path {
				t.Fatalf("selection result = %#v, %v", got.result, got.err)
			}
			if got.result.FileName == nil || *got.result.FileName != fileName {
				t.Fatalf("file name = %#v, want %q", got.result.FileName, fileName)
			}
		})
	}
}

func TestSelectFolderPathCanRetryAfterCancellation(t *testing.T) {
	for _, selectFile := range []bool{false, true} {
		t.Run(map[bool]string{false: "directory", true: "file"}[selectFile], func(t *testing.T) {
			selectedPath := t.TempDir()
			if selectFile {
				selectedPath = filepath.Join(selectedPath, "preview.zip")
			}
			selector, selectionID, results := startPathSelection(
				t,
				&scriptedPathSelectionDialog{results: []platform.DialogResult{
					{Canceled: true},
					{FilePath: selectedPath},
				}},
				selectFile,
			)

			selected, err := selector.selectFolderPath(context.Background(), selectionID)
			if err != nil || selected {
				t.Fatalf("canceled selectFolderPath() = %v, %v", selected, err)
			}
			selected, err = selector.selectFolderPath(context.Background(), selectionID)
			if err != nil || !selected {
				t.Fatalf("retried selectFolderPath() = %v, %v, want true, nil", selected, err)
			}

			got := awaitPathSelection(t, results)
			if got.err != nil || got.result.Mode != "folder" || got.result.Path == nil {
				t.Fatalf("selection result = %#v, %v", got.result, got.err)
			}
			wantPath := selectedPath
			if selectFile {
				wantPath = filepath.Dir(selectedPath)
				if got.result.FileName == nil || *got.result.FileName != filepath.Base(selectedPath) {
					t.Fatalf("file name = %#v", got.result.FileName)
				}
			}
			if *got.result.Path != wantPath {
				t.Fatalf("path = %q, want %q", *got.result.Path, wantPath)
			}
		})
	}
}

func TestSelectFolderPathCancellationCanCancelOuterRequest(t *testing.T) {
	selector, selectionID, results := startPathSelection(
		t,
		&scriptedPathSelectionDialog{results: []platform.DialogResult{{Canceled: true}}},
		false,
	)

	selected, err := selector.selectFolderPath(context.Background(), selectionID)
	if err != nil || selected {
		t.Fatalf("selectFolderPath() = %v, %v, want false, nil", selected, err)
	}
	selector.cancel(selectionID)

	got := awaitPathSelection(t, results)
	if got.err != nil || got.result.Mode != "folder" || got.result.Path != nil {
		t.Fatalf("selection result = %#v, %v", got.result, got.err)
	}
}

func startPathSelection(
	t *testing.T,
	dialog pathSelectionDialog,
	selectFile bool,
) (*pathSelector, string, <-chan asyncPathSelectionResult) {
	t.Helper()
	selectionIDs := make(chan string, 1)
	selector := newPathSelector(dialog, nil, func(_ string, args ...any) {
		data := args[0].(map[string]any)
		selectionIDs <- data["selectionId"].(string)
	}, nil)
	results := make(chan asyncPathSelectionResult, 1)
	go func() {
		result, err := selector.getSelectedPathWithModeModal(
			context.Background(), "preview.zip", nil, nil, "drive", nil, selectFile,
		)
		results <- asyncPathSelectionResult{result: result, err: err}
	}()

	select {
	case selectionID := <-selectionIDs:
		return selector, selectionID, results
	case <-time.After(time.Second):
		t.Fatal("path selection event was not emitted")
		return nil, "", nil
	}
}

func assertPathSelectionPending(
	t *testing.T,
	selector *pathSelector,
	selectionID string,
	results <-chan asyncPathSelectionResult,
) {
	t.Helper()
	selector.mu.Lock()
	_, pending := selector.pending[selectionID]
	selector.mu.Unlock()
	if !pending {
		t.Fatal("canceled selection was removed from the pending map")
	}
	select {
	case got := <-results:
		t.Fatalf("pending request completed after native cancellation: %#v, %v", got.result, got.err)
	default:
	}
}

func awaitPathSelection(t *testing.T, results <-chan asyncPathSelectionResult) asyncPathSelectionResult {
	t.Helper()
	select {
	case got := <-results:
		return got
	case <-time.After(time.Second):
		t.Fatal("pending path selection did not complete")
		return asyncPathSelectionResult{}
	}
}

func TestPathSelectorWaitsForColdRendererBeforeEmitting(t *testing.T) {
	for _, test := range []struct {
		name         string
		alreadyReady bool
		want         []string
	}{
		{name: "cold", alreadyReady: false, want: []string{"focus", "wait", "delay", "emit"}},
		{name: "warm", alreadyReady: true, want: []string{"focus", "wait", "emit"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			var selector *pathSelector
			selector = newPathSelector(
				nil,
				nil,
				func(_ string, args ...any) {
					calls = append(calls, "emit")
					data := args[0].(map[string]any)
					selector.cancel(data["selectionId"].(string))
				},
				func() { calls = append(calls, "focus") },
			)
			selector.waitReady = func(context.Context) (bool, error) {
				calls = append(calls, "wait")
				return test.alreadyReady, nil
			}
			selector.readyDelay = func(context.Context) error {
				calls = append(calls, "delay")
				return nil
			}

			if _, err := selector.getSelectedPathWithModeModal(
				context.Background(), "preview.zip", nil, nil, "drive", nil, false,
			); err != nil {
				t.Fatal(err)
			}
			if strings.Join(calls, ",") != strings.Join(test.want, ",") {
				t.Fatalf("calls = %v, want %v", calls, test.want)
			}
		})
	}
}
