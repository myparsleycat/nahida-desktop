package mod

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
)

type failingPathSelectionDialog struct {
	err error
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

			if err := selector.selectFolderPath(context.Background(), selectionID); !errors.Is(err, dialogErr) {
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
