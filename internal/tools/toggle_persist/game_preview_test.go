package togglepersist

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"nahida.live/desktop/internal/gamelink"
)

// A preview must leave the game as it found it, including values the user set in the game after the viewer
// loaded, which the viewer's own state does not know.
func TestGamePreviewSetsBackTheGameValues(t *testing.T) {
	importer, path := writePreviewINI(t)
	const outfit, hair = `$\mods\foo\foo.ini\outfit`, `$\mods\foo\foo.ini\hair`
	link := &fakeGameLink{
		states: []gamelink.State{linkedGame(20, "GIMI", importer)},
		values: map[string]float32{outfit: 3, hair: 5},
		sent:   map[uint32][]gamelink.Assign{},
	}
	preview := &GamePreview{service: NewWithOptions(Options{Link: link}), games: map[uint32]*previewGame{}}

	preview.show([]GamePreviewINI{{Path: path, State: map[string]any{"outfit": float64(1)}}})
	preview.show([]GamePreviewINI{{Path: path, State: map[string]any{"outfit": float64(2), "hair": float64(1)}}})
	preview.show([]GamePreviewINI{{Path: path, State: map[string]any{"outfit": float64(3)}}})
	preview.show(nil)

	want := []gamelink.Assign{
		{Name: outfit, Value: 1},
		{Name: hair, Value: 1}, {Name: outfit, Value: 2},
		{Name: hair, Value: 5}, {Name: outfit, Value: 3},
	}
	if !reflect.DeepEqual(link.sent[20], want) {
		t.Fatalf("sent %+v, want %+v", link.sent[20], want)
	}
	if got := (map[string]float32{outfit: 3, hair: 5}); !reflect.DeepEqual(link.values, got) {
		t.Fatalf("game values after the preview %+v, want %+v", link.values, got)
	}
	if len(preview.games) != 0 {
		t.Fatalf("preview kept %d games after it ended", len(preview.games))
	}

	// The next preview reads the game again, as its values may have changed in between.
	link.values[outfit] = 4
	preview.show([]GamePreviewINI{{Path: path, State: map[string]any{"outfit": float64(1)}}})
	preview.show(nil)
	if got := link.values[outfit]; got != 4 {
		t.Fatalf("outfit after the second preview = %v, want 4", got)
	}
	if len(link.reads) != 3 {
		t.Fatalf(
			"game read %d times, want once per newly previewed variable and preview: %v",
			len(link.reads),
			link.reads,
		)
	}
}

// Moving the pointer from one row to the next ends the preview before the next begins. The end must wait for
// restoreDelay, so the game goes from the first previewed value straight to the second.
func TestGamePreviewHoldsBackTheEndOfAPreview(t *testing.T) {
	importer, path := writePreviewINI(t)
	link := &fakeGameLink{
		states: []gamelink.State{linkedGame(20, "GIMI", importer)},
		values: map[string]float32{`$\mods\foo\foo.ini\outfit`: 3},
		sent:   map[uint32][]gamelink.Assign{},
	}
	preview := link.newPreview()
	row := func(value float64) []GamePreviewINI {
		return []GamePreviewINI{{Path: path, State: map[string]any{"outfit": value}}}
	}
	preview.show(row(1))
	taken := func(next func()) []GamePreviewINI {
		t.Helper()
		result := make(chan []GamePreviewINI, 1)
		go func() {
			inis, _ := preview.next()
			result <- inis
		}()
		next()
		select {
		case inis := <-result:
			return inis
		case <-time.After(5 * time.Second):
			t.Fatal("the preview took no state")
			return nil
		}
	}

	// The wake of the end itself does not end the delay.
	preview.restoreDelay = 50 * time.Millisecond
	preview.Show(nil)
	start := time.Now()
	if inis := taken(func() {}); inis != nil {
		t.Fatalf("took %+v, want the end", inis)
	}
	if elapsed := time.Since(start); elapsed < preview.restoreDelay {
		t.Fatalf("the end was taken after %v, before the %v delay", elapsed, preview.restoreDelay)
	}

	preview.restoreDelay = time.Hour
	preview.Show(nil)
	if inis := taken(func() { preview.Show(row(2)) }); !reflect.DeepEqual(inis, row(2)) {
		t.Fatalf("took %+v, want the next row in place of the end", inis)
	}
	preview.Show(nil)
	if inis := taken(preview.Close); inis != nil {
		t.Fatalf("took %+v after closing, want the end at once", inis)
	}
}

// After a failed request the preview keeps a game's original values only while the game can still take them
// back, and sends a state again only when the game did not queue it.
func TestGamePreviewKeepsTheRecordWhileTheGameCanTakeItBack(t *testing.T) {
	for _, tc := range []struct {
		name                string
		fail                func(*fakeGameLink)
		wantRetry, wantKept bool
	}{
		{name: "busy", fail: func(f *fakeGameLink) { f.busy = 1 }, wantRetry: true, wantKept: true},
		{name: "reconnecting", fail: func(f *fakeGameLink) {
			f.states[0].Status, f.disconnected = gamelink.StatusConnecting, true
		}, wantRetry: true, wantKept: true},
		{name: "timeout", fail: func(f *fakeGameLink) { f.timeouts = 1 }},
		{name: "game closed", fail: func(f *fakeGameLink) { f.states, f.disconnected = nil, true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			importer, path := writePreviewINI(t)
			link := &fakeGameLink{
				states: []gamelink.State{linkedGame(20, "GIMI", importer)},
				values: map[string]float32{`$\mods\foo\foo.ini\outfit`: 3},
				sent:   map[uint32][]gamelink.Assign{},
			}
			preview := link.newPreview()
			preview.show([]GamePreviewINI{{Path: path, State: map[string]any{"outfit": float64(1)}}})

			tc.fail(link)
			failed := preview.show(nil)
			if retry := len(failed) > 0; retry != tc.wantRetry {
				t.Errorf("retry = %v (%v), want %v", retry, failed, tc.wantRetry)
			}
			if kept := len(preview.games) > 0; kept != tc.wantKept {
				t.Errorf("record kept = %v, want %v", kept, tc.wantKept)
			}
		})
	}
}

// A game that rejects the request to set its value back still shows the preview, so the preview must keep that
// value and send it again, even once the viewer closed.
func TestGamePreviewRetriesSettingBackARejectedValue(t *testing.T) {
	importer, path := writePreviewINI(t)
	const outfit = `$\mods\foo\foo.ini\outfit`
	link := &fakeGameLink{
		states: []gamelink.State{linkedGame(20, "GIMI", importer)},
		values: map[string]float32{outfit: 3},
		sent:   map[uint32][]gamelink.Assign{},
		set:    make(chan []gamelink.Assign, 4),
	}
	preview := link.newPreview()
	go preview.run()

	preview.Show([]GamePreviewINI{{Path: path, State: map[string]any{"outfit": float64(1)}}})
	if got := waitSetVars(t, link); !reflect.DeepEqual(got, []gamelink.Assign{{Name: outfit, Value: 1}}) {
		t.Fatalf("preview sent %+v", got)
	}
	link.mu.Lock()
	link.busy = 2
	link.mu.Unlock()
	preview.Close()
	if got := waitSetVars(t, link); !reflect.DeepEqual(got, []gamelink.Assign{{Name: outfit, Value: 3}}) {
		t.Fatalf("closing sent %+v, want the game value back", got)
	}
}

func (f *fakeGameLink) newPreview() *GamePreview {
	return &GamePreview{
		service: NewWithOptions(Options{Link: f}), wake: make(chan struct{}, 1), restoreDelay: time.Hour,
		games: map[uint32]*previewGame{},
	}
}

func waitSetVars(t *testing.T, link *fakeGameLink) []gamelink.Assign {
	t.Helper()
	select {
	case vars := <-link.set:
		return vars
	case <-time.After(5 * time.Second):
		t.Fatal("the game got no SetVars")
		return nil
	}
}

func writePreviewINI(t *testing.T) (importer, path string) {
	t.Helper()
	importer = t.TempDir()
	path = filepath.Join(importer, "Mods", "Foo", "foo.ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		path,
		[]byte("[Constants]\nglobal persist $outfit = 0\nglobal $hair = 0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	return importer, path
}
