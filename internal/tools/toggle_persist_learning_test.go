package tools

import (
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"
)

const togglePersistTestINI = `C:\Mods\Example\example.ini`

func TestTogglePersistLearnerEmitsIsolatedChangeAfterQuietWindow(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	learner.Observe(togglePersistTestINI, "Toggle", "1", 1, 0)
	if len(learner.TakeReady(togglePersistTestINI, 2_999).Updates) != 0 {
		t.Fatal("too early")
	}
	got := learner.TakeReady(togglePersistTestINI, 3_000).Updates
	want := [][2]string{{"toggle", "1"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %#v, want %#v", got, want)
	}
}

func TestTogglePersistLearnerCoalescesBurstToFinalValue(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	for index, value := range []string{"0.1", "0.4", "0.8"} {
		learner.Observe(togglePersistTestINI, "Amount", value, index+1, int64(index*1_000))
	}
	if len(learner.TakeReady(togglePersistTestINI, 4_999).Updates) != 0 {
		t.Fatal("too early")
	}
	got := learner.TakeReady(togglePersistTestINI, 5_000).Updates
	want := [][2]string{{"amount", "0.8"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %#v, want %#v", got, want)
	}
}

func TestTogglePersistLearnerSuppressesContinuousNumericStream(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	suppressed := map[string]struct{}{}
	for index := range 10 {
		result := learner.Observe(togglePersistTestINI, "Phase", formatRatio(index, 10), index+1, int64(index*2_000))
		for _, name := range result.NewlySuppressed {
			suppressed[name] = struct{}{}
		}
	}
	if !reflect.DeepEqual(keys(suppressed), []string{"Phase"}) {
		t.Fatalf("suppressed = %#v", keys(suppressed))
	}
	if len(learner.TakeReady(togglePersistTestINI, 60_000).Updates) != 0 {
		t.Fatal("suppressed stream should discard the final value")
	}
}

func TestTogglePersistLearnerKeepsLearningAcrossQuietFlushes(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	suppressed := map[string]struct{}{}
	learned := map[string]struct{}{}
	var updates [][2]string
	for index := range 12 {
		at := int64(index * 15_000)
		result := learner.Observe(togglePersistTestINI, "Phase", formatRatio(index, 12), index+1, at)
		for _, name := range result.NewlySuppressed {
			suppressed[name] = struct{}{}
		}
		for _, variable := range result.NewlyLearned {
			learned[variable.Name] = struct{}{}
		}
		updates = append(updates, learner.TakeReady(togglePersistTestINI, at+10_000).Updates...)
	}
	if !reflect.DeepEqual(keys(suppressed), []string{"Phase"}) {
		t.Fatalf("suppressed = %#v", keys(suppressed))
	}
	if !reflect.DeepEqual(keys(learned), []string{"Phase"}) {
		t.Fatalf("learned = %#v", keys(learned))
	}
	if len(updates) != 7 {
		t.Fatalf("updates length = %d, %#v", len(updates), updates)
	}
	if updates[len(updates)-1] != [2]string{"phase", "0.5"} {
		t.Fatalf("last update = %#v", updates[len(updates)-1])
	}
}

func TestTogglePersistLearnerDetectsRegularDiscreteCycle(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	suppressed := map[string]struct{}{}
	for index := range 10 {
		value := "1"
		if index%2 != 0 {
			value = "-1"
		}
		result := learner.Observe(togglePersistTestINI, "Direction", value, index+1, int64(index*4_000))
		for _, name := range result.NewlySuppressed {
			suppressed[name] = struct{}{}
		}
	}
	if !reflect.DeepEqual(keys(suppressed), []string{"Direction"}) {
		t.Fatalf("suppressed = %#v", keys(suppressed))
	}
}

func TestTogglePersistLearnerLearnsSparseCyclePair(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	suppressed := map[string]struct{}{}
	learned := map[string]struct{}{}
	for index := range 8 {
		result := learner.Observe(togglePersistTestINI, "Phase", formatRatio(index, 8), index+1, int64(index*3_000))
		for _, name := range result.NewlySuppressed {
			suppressed[name] = struct{}{}
		}
	}
	observations := []struct {
		revision int
		at       int64
		value    string
	}{
		{1, 0, "1"}, {4, 9_000, "-1"}, {9, 30_000, "1"}, {10, 45_000, "-1"}, {11, 60_000, "1"},
	}
	for _, observation := range observations {
		for _, varName := range []string{"autoDirToy", "autoDirBeads"} {
			result := learner.Observe(togglePersistTestINI, varName, observation.value, observation.revision, observation.at)
			for _, name := range result.NewlySuppressed {
				suppressed[name] = struct{}{}
			}
			for _, variable := range result.NewlyLearned {
				learned[variable.Name] = struct{}{}
			}
		}
	}
	if !sameSet(keys(suppressed), []string{"Phase", "autoDirBeads", "autoDirToy"}) {
		t.Fatalf("suppressed = %#v", keys(suppressed))
	}
	if !sameSet(keys(learned), []string{"autoDirBeads", "autoDirToy"}) {
		t.Fatalf("learned = %#v", keys(learned))
	}
}

func TestTogglePersistLearnerLearnsRuntimeCohort(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	suppressed := map[string]struct{}{}
	learned := map[string]struct{}{}
	for index := range 21 {
		revision := index + 1
		at := int64(index * 3_000)
		phase := float64(index) / 10
		if index > 10 {
			phase = float64(20-index) / 10
		}
		for _, item := range []struct {
			name  string
			value float64
		}{
			{"autoPhaseToy", phase}, {"Freq_shape_7", phase}, {"autoPhaseBeads", phase}, {"Freq_shape_6", phase},
		} {
			result := learner.Observe(togglePersistTestINI, item.name, formatFloat(item.value), revision, at)
			for _, name := range result.NewlySuppressed {
				suppressed[name] = struct{}{}
			}
			for _, variable := range result.NewlyLearned {
				learned[variable.Name] = struct{}{}
			}
		}
		if index%5 == 0 {
			value := "1"
			if index%10 != 0 {
				value = "-1"
			}
			for _, varName := range []string{"autoDirToy", "autoDirBeads"} {
				result := learner.Observe(togglePersistTestINI, varName, value, revision, at)
				for _, name := range result.NewlySuppressed {
					suppressed[name] = struct{}{}
				}
				for _, variable := range result.NewlyLearned {
					learned[variable.Name] = struct{}{}
				}
			}
		}
	}
	want := []string{"Freq_shape_6", "Freq_shape_7", "autoDirBeads", "autoDirToy", "autoPhaseBeads", "autoPhaseToy"}
	if !sameSet(keys(suppressed), want) {
		t.Fatalf("suppressed = %#v", keys(suppressed))
	}
	if !sameSet(keys(learned), want) {
		t.Fatalf("learned = %#v", keys(learned))
	}
}

func TestTogglePersistLearnerSavesIrregularSparseSequence(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	ats := []int64{0, 20_000, 55_000, 90_000}
	revisions := []int{1, 3, 10, 15}
	for index, at := range ats {
		result := learner.Observe(togglePersistTestINI, "Sparse", formatInt(index), revisions[index], at)
		if len(result.NewlySuppressed) != 0 {
			t.Fatalf("newlySuppressed = %#v", result.NewlySuppressed)
		}
	}
	got := learner.TakeReady(togglePersistTestINI, 100_000).Updates
	want := [][2]string{{"sparse", "3"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %#v, want %#v", got, want)
	}
}

func TestTogglePersistLearnerReconsidersAfterCooldown(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	for index := range 10 {
		learner.Observe(togglePersistTestINI, "Phase", formatInt(index), index+1, int64(index*2_000))
	}
	learner.Observe(togglePersistTestINI, "Phase", "manual", 20, 50_000)
	got := learner.TakeReady(togglePersistTestINI, 53_000).Updates
	want := [][2]string{{"phase", "manual"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %#v, want %#v", got, want)
	}
}

func TestTogglePersistLearnerUsesLearnedProfileAsPrior(t *testing.T) {
	t.Parallel()
	learner := newTogglePersistLearner()
	learner.RegisterLearnedVariables(togglePersistTestINI, map[string]TogglePersistLearnedVariable{
		"phase": {Name: "Phase", MedianIntervalMs: 1_000, LearnedAt: time.Unix(0, 0).UTC().Format("2006-01-02T15:04:05.000Z")},
	})
	learner.Observe(togglePersistTestINI, "Phase", "0.5", 1, 0)
	if got := learner.TakeReady(togglePersistTestINI, 3_000).Updates; !reflect.DeepEqual(got, [][2]string{{"phase", "0.5"}}) {
		t.Fatalf("isolated = %#v", got)
	}

	repeated := newTogglePersistLearner()
	repeated.RegisterLearnedVariables(togglePersistTestINI, map[string]TogglePersistLearnedVariable{
		"phase": {Name: "Phase", MedianIntervalMs: 1_000, LearnedAt: time.Unix(0, 0).UTC().Format("2006-01-02T15:04:05.000Z")},
	})
	var suppressed []string
	for index, at := range []int64{0, 1_000, 2_000} {
		suppressed = append(suppressed, repeated.Observe(togglePersistTestINI, "Phase", formatInt(index), index+1, at).NewlySuppressed...)
	}
	if !reflect.DeepEqual(suppressed, []string{"Phase"}) {
		t.Fatalf("suppressed = %#v", suppressed)
	}
	if len(repeated.TakeReady(togglePersistTestINI, 20_000).Updates) != 0 {
		t.Fatal("learned cadence should remain suppressed")
	}

	different := newTogglePersistLearner()
	different.RegisterLearnedVariables(togglePersistTestINI, map[string]TogglePersistLearnedVariable{
		"phase": {Name: "Phase", MedianIntervalMs: 1_000, LearnedAt: time.Unix(0, 0).UTC().Format("2006-01-02T15:04:05.000Z")},
	})
	for index, at := range []int64{0, 3_000, 6_000} {
		if got := different.Observe(togglePersistTestINI, "Phase", formatInt(index), index+1, at).NewlySuppressed; len(got) != 0 {
			t.Fatalf("different cadence suppressed = %#v", got)
		}
	}
	if got := different.TakeReady(togglePersistTestINI, 15_000).Updates; !reflect.DeepEqual(got, [][2]string{{"phase", "2"}}) {
		t.Fatalf("different cadence updates = %#v", got)
	}
}

func TestFingerprintTogglePersistININormalizesPersistValues(t *testing.T) {
	t.Parallel()
	first := "[Constants]\r\nglobal persist $Toggle = 0\r\n[Present]\r\npost $Toggle = 1"
	second := "[Constants]\nglobal persist $Toggle = 9\n[Present]\npost $Toggle = 1"
	structural := "[Constants]\nglobal persist $Toggle = 9\n[Present]\npost $Toggle = 2"
	if fingerprintTogglePersistINI(first) != fingerprintTogglePersistINI(second) {
		t.Fatal("persist value should be normalized")
	}
	if fingerprintTogglePersistINI(second) == fingerprintTogglePersistINI(structural) {
		t.Fatal("structural change should change fingerprint")
	}
}

func TestParseTogglePersistProfileNormalizesVariableKeys(t *testing.T) {
	t.Parallel()
	profile, err := parseTogglePersistProfile(map[string]any{
		"version": float64(1),
		"files": map[string]any{
			"example.ini": map[string]any{
				"fingerprint": "hash",
				"variables": map[string]any{
					"Phase": map[string]any{
						"name": "Phase", "medianIntervalMs": float64(1_000),
						"learnedAt": time.Unix(0, 0).UTC().Format("2006-01-02T15:04:05.000Z"),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Files["example.ini"].Variables["phase"].Name != "Phase" {
		t.Fatalf("normalized variable = %#v", profile.Files["example.ini"].Variables)
	}
	if _, err := parseTogglePersistProfile(map[string]any{"version": float64(2), "files": map[string]any{}}); err == nil {
		t.Fatal("expected version error")
	}
}

func formatRatio(index, denom int) string {
	return jsNumberString(float64(index) / float64(denom))
}

func formatFloat(value float64) string {
	return jsNumberString(value)
}

func formatInt(value int) string {
	return strconv.Itoa(value)
}

func jsNumberString(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func keys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func sameSet(got, want []string) bool {
	gotCopy := append([]string{}, got...)
	wantCopy := append([]string{}, want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	return reflect.DeepEqual(gotCopy, wantCopy)
}
