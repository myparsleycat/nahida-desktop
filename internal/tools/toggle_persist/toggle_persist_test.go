package togglepersist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPersistModelViewerToggleStateOnlyUpdatesConstantsPersistVariables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mod.ini")
	input := "[Constants]\r\nglobal persist $Toggle = 0\r\nglobal $Ignored = 1\r\n\r\n[Other]\r\nglobal persist $Toggle = 5\r\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	result, err := service.PersistModelViewerToggleState(path, map[string]any{"toggle": float64(2), "missing": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.UpdatedVariables, []string{"Toggle"}) {
		t.Fatalf("updated variables = %#v", result.UpdatedVariables)
	}
	want := "[Constants]\r\nglobal persist $Toggle = 2\r\nglobal $Ignored = 1\r\n\r\n[Other]\r\nglobal persist $Toggle = 5\r\n"
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("updated INI = %q, %v; want %q", raw, err, want)
	}
}

func TestPersistModelViewerToggleStateHandlesDeclarationVariants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mod.ini")
	input := "[Constants]\nglobal persis $Toggle = 0\nglobal persis $Unlisted = 1\npersist global $Reversed = 0\n" +
		"global persist $Dup = 0\nglobal persis $Dup = 1\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	result, err := service.PersistModelViewerToggleState(
		path,
		map[string]any{"toggle": float64(0), "reversed": float64(2)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.UpdatedVariables, []string{"Reversed"}) ||
		!reflect.DeepEqual(result.RepairedVariables, []string{"Toggle", "Unlisted"}) {
		t.Fatalf("result = %#v", result)
	}
	want := "[Constants]\nglobal persist $Toggle = 0\nglobal persist $Unlisted = 1\nglobal persist $Reversed = 2\n" +
		"global persist $Dup = 0\nglobal persis $Dup = 1\n"
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("updated INI = %q, %v; want %q", raw, err, want)
	}
}

func TestPersistModelViewerToggleStateRepairsWithoutViewerState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mod.ini")
	if err := os.WriteFile(path, []byte("[Constants]\nglobal persis $Toggle = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := New().PersistModelViewerToggleState(path, map[string]any{})
	if err != nil || !reflect.DeepEqual(result.RepairedVariables, []string{"Toggle"}) {
		t.Fatalf("result = %#v, %v", result, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "[Constants]\nglobal persist $Toggle = 1\n" {
		t.Fatalf("updated INI = %q, %v", raw, err)
	}
}

func TestParsePersistTargetsReadsDeclarationVariants(t *testing.T) {
	content := "[Constants]\npersist global $Reversed = 1\nglobal persist $Plain = 0\nglobal $Local = 0\n" +
		"global persis $Typo = 0\n"
	targets, _ := parsePersistTargets(content, "")
	got := map[string]bool{}
	for _, target := range targets {
		got[target.varName] = target.persistent
	}
	want := map[string]bool{"Reversed": true, "Plain": true, "Local": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v; want %#v", got, want)
	}
}

func TestFingerprintIgnoresWhatASaveRewrites(t *testing.T) {
	want := fingerprintTogglePersistINI("[Constants]\nglobal persist $Toggle = 2\n")
	for _, input := range []string{
		"[Constants]\npersist global $Toggle = 0\n",
		"[Constants]\nglobal persis $Toggle = 1\n",
	} {
		if got := fingerprintTogglePersistINI(input); got != want {
			t.Fatalf("fingerprint of %q differs from the rewritten declaration", input)
		}
	}
}

func TestPersistModelViewerToggleStateDoesNotRewriteUnchangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mod.ini")
	input := "[Constants]\nglobal persist $Toggle = 2\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	result, err := service.PersistModelViewerToggleState(path, map[string]any{"Toggle": " 2 "})
	if err != nil || len(result.UpdatedVariables) != 0 {
		t.Fatalf("result = %#v, %v", result, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != input {
		t.Fatalf("unchanged file was rewritten: %q", raw)
	}
}

func TestTogglePersistBatchesWatcherChangesAndWritesThemAfterTheQuietWindow(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}, {"Amount", "0"}})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	defer harness.engine.Stop()

	harness.trigger([][2]string{{"Toggle", "1"}, {"Amount", "0.75"}})
	if got := harness.engine.cachedValue("test", `$\Mods\Example\mod.ini\Toggle`); got != "1" {
		t.Fatalf("cached Toggle = %q", got)
	}
	if due := harness.engine.nextDueAt(harness.targetINIPath); due == nil || *due != 3000 {
		t.Fatalf("nextDueAt = %v", due)
	}
	harness.engine.Advance(2999)
	raw, _ := os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Toggle = 0") {
		t.Fatalf("INI changed too early: %s", raw)
	}

	harness.engine.Advance(1)
	raw, _ = os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Toggle = 1") {
		t.Fatalf("info=%v error=%v INI=%s", harness.info, harness.errors, raw)
	}
	if !strings.Contains(string(raw), "$Amount = 0.75") {
		t.Fatalf("Amount not updated: %s", raw)
	}
	if due := harness.engine.nextDueAt(harness.targetINIPath); due != nil {
		t.Fatalf("nextDueAt after flush = %v", *due)
	}
	if countPrefix(harness.info, "Updated persist variables") != 1 {
		t.Fatalf("updated log count = %d (%v)", countPrefix(harness.info, "Updated persist variables"), harness.info)
	}
}

func TestTogglePersistWatcherLeavesMisspelledDeclarationsAlone(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	input := "[Constants]\nglobal persist $Toggle = 0\nglobal persis $Typo = 0\n"
	if err := os.WriteFile(harness.targetINIPath, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	defer harness.engine.Stop()

	harness.trigger([][2]string{{"Toggle", "1"}})
	harness.engine.Advance(3000)
	want := "[Constants]\nglobal persist $Toggle = 1\nglobal persis $Typo = 0\n"
	raw, err := os.ReadFile(harness.targetINIPath)
	if err != nil || string(raw) != want {
		t.Fatalf("INI = %q, %v; want %q (info=%v errors=%v)", raw, err, want, harness.info, harness.errors)
	}
}

func TestTogglePersistIgnoresDuplicateWatcherContent(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	defer harness.engine.Stop()

	harness.trigger([][2]string{{"Toggle", "0"}})
	if revision := harness.revision(); revision != 0 {
		t.Fatalf("revision after identical content = %d; want 0", revision)
	}

	harness.trigger([][2]string{{"Toggle", "1"}})
	if revision := harness.revision(); revision != 1 {
		t.Fatalf("revision after changed content = %d; want 1", revision)
	}

	harness.trigger([][2]string{{"Toggle", "1"}})
	if revision := harness.revision(); revision != 1 {
		t.Fatalf("revision after repeated content = %d; want 1", revision)
	}
}

// 3DMigoto saves d3dx_user.ini to a temporary file and renames it over the original.
func TestWatchPersistFileObservesAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d3dx_user.ini")
	if err := os.WriteFile(path, []byte("[Constants]\n$toggle = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	modified := make(chan struct{}, 1)
	stop, err := watchPersistFile(path, func() {
		select {
		case modified <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	temp := path + ".tmp"
	if err := os.WriteFile(temp, []byte("[Constants]\n$toggle = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp, path); err != nil {
		t.Fatal(err)
	}
	select {
	case <-modified:
	case <-time.After(3 * time.Second):
		t.Fatal("atomic replacement of d3dx_user.ini was not observed")
	}
}

func TestTogglePersistRevalidatesAMatchingLearnedProfileBeforeSuppressingWrites(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Phase", "0"}})
	target, err := os.ReadFile(harness.targetINIPath)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, togglePersistProfilePath(harness.targetINIPath), map[string]any{
		"version": 1,
		"files": map[string]any{
			"mod.ini": map[string]any{
				"fingerprint": fingerprintTogglePersistINI(string(target)),
				"variables": map[string]any{
					"phase": map[string]any{
						"name": "Phase", "medianIntervalMs": 2000, "learnedAt": "1970-01-01T00:00:00.000Z",
					},
				},
			},
		},
	})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	defer harness.engine.Stop()

	for _, value := range []string{"0.1", "0.2", "0.3"} {
		harness.trigger([][2]string{{"Phase", value}})
		harness.engine.Advance(2000)
	}
	harness.engine.Advance(10000)
	raw, _ := os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Phase = 0") {
		t.Fatalf("suppressed variable was written: %s", raw)
	}
	if countPrefix(harness.info, "Suppressed continuously changing persist variables") != 1 {
		t.Fatalf(
			"suppress log count = %d (%v)",
			countPrefix(harness.info, "Suppressed continuously changing persist variables"),
			harness.info,
		)
	}
}

func TestTogglePersistIgnoresAMalformedProfileAndContinuesNormalPersistence(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	if err := os.WriteFile(togglePersistProfilePath(harness.targetINIPath), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	defer harness.engine.Stop()

	harness.trigger([][2]string{{"Toggle", "1"}})
	harness.engine.Advance(3000)
	raw, _ := os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Toggle = 1") {
		t.Fatalf("INI = %s", raw)
	}
	if countContains(harness.errors, "stage=load") != 1 {
		t.Fatalf("load errors = %v", harness.errors)
	}
}

func TestTogglePersistIgnoresLearnedVariablesWhenTheTargetINIFingerprintChanges(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	writeJSON(t, togglePersistProfilePath(harness.targetINIPath), map[string]any{
		"version": 1,
		"files": map[string]any{
			"mod.ini": map[string]any{
				"fingerprint": "stale-fingerprint",
				"variables": map[string]any{
					"toggle": map[string]any{
						"name": "Toggle", "medianIntervalMs": 1000, "learnedAt": "1970-01-01T00:00:00.000Z",
					},
				},
			},
		},
	})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	defer harness.engine.Stop()

	harness.trigger([][2]string{{"Toggle", "1"}})
	harness.engine.Advance(3000)
	raw, _ := os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Toggle = 1") {
		t.Fatalf("INI = %s", raw)
	}
}

func TestTogglePersistCancelsPendingWritesWhenTheWatcherStops(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}

	harness.trigger([][2]string{{"Toggle", "1"}})
	harness.engine.Stop()
	harness.engine.Advance(10000)
	raw, _ := os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Toggle = 0") {
		t.Fatalf("pending write was not cancelled: %s", raw)
	}
}

func TestTogglePersistKeepsExplicitSaveToINICallsOutsideAutomaticLearning(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	result, err := harness.engine.PersistStateToINI(harness.targetINIPath, map[string]any{"Toggle": 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.UpdatedVariables, []string{"Toggle"}) {
		t.Fatalf("updated = %#v", result.UpdatedVariables)
	}
	raw, _ := os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Toggle = 1") {
		t.Fatalf("INI = %s", raw)
	}
	if _, err := os.Stat(togglePersistProfilePath(harness.targetINIPath)); !os.IsNotExist(err) {
		t.Fatalf("profile should not exist, stat err = %v", err)
	}
}

func TestTogglePersistLearnsAcrossPeriodicFlushesAndSavesAReusableModProfile(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Phase", "0"}})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	defer harness.engine.Stop()

	for index := 1; index <= 12; index++ {
		harness.trigger([][2]string{{"Phase", strconv.FormatFloat(float64(index)/12, 'g', -1, 64)}})
		harness.engine.Advance(10000)
		harness.engine.Advance(5000)
	}

	rawProfile, err := os.ReadFile(togglePersistProfilePath(harness.targetINIPath))
	if err != nil {
		t.Fatal(err)
	}
	var parsed any
	if err := json.Unmarshal(rawProfile, &parsed); err != nil {
		t.Fatal(err)
	}
	profile, err := parseTogglePersistProfile(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Files["mod.ini"].Variables["phase"].Name != "Phase" {
		t.Fatalf("profile = %#v", profile.Files["mod.ini"].Variables)
	}
	raw, _ := os.ReadFile(harness.targetINIPath)
	if !strings.Contains(string(raw), "$Phase = 0.5833333333333334") {
		t.Fatalf("INI = %s", raw)
	}
	if countPrefix(harness.info, "Updated persist variable") != 7 {
		t.Fatalf("updated count = %d (%v)", countPrefix(harness.info, "Updated persist variable"), harness.info)
	}
	if countPrefix(harness.info, "Suppressed continuously changing persist variables") != 1 {
		t.Fatalf(
			"suppress count = %d (%v)",
			countPrefix(harness.info, "Suppressed continuously changing persist variables"),
			harness.info,
		)
	}
}

type persistHarness struct {
	engine        *persistEngine
	targetINIPath string
	d3dxPath      string
	onModify      func()
	info          []string
	errors        []string
}

func TestTogglePersistDropsQueuedUpdateAfterModIsRemoved(t *testing.T) {
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	harness.trigger([][2]string{{"Toggle", "1"}})
	if err := os.RemoveAll(filepath.Dir(harness.targetINIPath)); err != nil {
		t.Fatal(err)
	}

	harness.engine.Advance(3_000)
	if len(harness.errors) != 0 {
		t.Fatalf("stale queued update logged errors: %v", harness.errors)
	}
}

func createPersistHarness(t *testing.T, initial [][2]string) *persistHarness {
	t.Helper()
	importerFolder := t.TempDir()
	targetINIPath := filepath.Join(importerFolder, "Mods", "Example", "mod.ini")
	d3dxPath := filepath.Join(importerFolder, "d3dx_user.ini")
	if err := os.MkdirAll(filepath.Dir(targetINIPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetINIPath, []byte(renderPersistTargetINI(initial)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d3dxPath, []byte(renderPersistD3dxUserINI(initial)), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := newPersistEngine()
	engine.useFakeClock()
	harness := &persistHarness{engine: engine, targetINIPath: targetINIPath, d3dxPath: d3dxPath}
	engine.infoFn = func(message string) { harness.info = append(harness.info, message) }
	engine.errorFn = func(message string) { harness.errors = append(harness.errors, message) }
	return harness
}

func (h *persistHarness) start() error {
	return h.engine.Start(
		[]persistImporter{{Key: "test", Folder: filepath.Dir(h.d3dxPath)}},
		func(_ string, onModify func()) (func(), error) {
			h.onModify = onModify
			return func() {}, nil
		},
	)
}

func (h *persistHarness) trigger(state [][2]string) {
	if err := os.WriteFile(h.d3dxPath, []byte(renderPersistD3dxUserINI(state)), 0o600); err != nil {
		panic(err)
	}
	if h.onModify == nil {
		panic("watcher callback is missing")
	}
	h.onModify()
}

func (h *persistHarness) revision() int {
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	return h.engine.revisions["test"]
}

func renderPersistTargetINI(state [][2]string) string {
	var builder strings.Builder
	builder.WriteString("[Constants]\n")
	for _, pair := range state {
		builder.WriteString("global persist $" + pair[0] + " = " + pair[1] + "\n")
	}
	return builder.String()
}

func renderPersistD3dxUserINI(state [][2]string) string {
	var builder strings.Builder
	builder.WriteString("[Constants]\n")
	for _, pair := range state {
		builder.WriteString(`$\Mods\Example\mod.ini\` + pair[0] + " = " + pair[1] + "\n")
	}
	return builder.String()
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func countPrefix(messages []string, prefix string) int {
	count := 0
	for _, message := range messages {
		if strings.HasPrefix(message, prefix) {
			count++
		}
	}
	return count
}

func countContains(messages []string, needle string) int {
	count := 0
	for _, message := range messages {
		if strings.Contains(message, needle) {
			count++
		}
	}
	return count
}
