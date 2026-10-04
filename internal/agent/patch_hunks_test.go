package agent

import (
	"strings"
	"testing"
)

func TestApplyPatchHunksReplacesLines(t *testing.T) {
	t.Parallel()
	text := "; comment\r\n[A]\r\nvalue=old\r\nother=1\r\n"
	updated, warnings, err := applyPatchHunks(text, []PatchHunk{{
		OldLines: []string{"value=old"},
		NewLines: []string{"value=new"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "; comment\r\n[A]\r\nvalue=new\r\nother=1\r\n" {
		t.Fatalf("updated = %q", updated)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestApplyPatchHunksReplacesMultipleLinesInOrder(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nfirst=1\r\nkeep=0\r\nsecond=2\r\n"
	updated, _, err := applyPatchHunks(text, []PatchHunk{
		{OldLines: []string{"first=1"}, NewLines: []string{"first=11", "added=1"}},
		{OldLines: []string{"second=2"}, NewLines: []string{"second=22"}},
	}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "[A]\r\nfirst=11\r\nadded=1\r\nkeep=0\r\nsecond=22\r\n"
	if updated != want {
		t.Fatalf("updated = %q, want %q", updated, want)
	}
}

func TestApplyPatchHunksReusesPreviousBoundaryAsContext(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nfirst=1\r\n[B]\r\nsecond=2\r\n[C]\r\n"
	updated, _, err := applyPatchHunks(text, []PatchHunk{
		{
			Context:  "[A]",
			OldLines: []string{"first=1", "[B]"},
			NewLines: []string{"first=11", "[B]"},
		},
		{
			Context:  "[B]",
			OldLines: []string{"second=2", "[C]"},
			NewLines: []string{"second=22", "[C]"},
		},
	}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	want := "[A]\r\nfirst=11\r\n[B]\r\nsecond=22\r\n[C]\r\n"
	if updated != want {
		t.Fatalf("updated = %q, want %q", updated, want)
	}
}

func TestApplyPatchHunksRejectsChangedPreviousBoundaryContext(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		newLines []string
	}{
		{name: "renamed", newLines: []string{"first=11", "[Renamed]"}},
		{name: "deleted", newLines: []string{"first=11"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := applyPatchHunks("[A]\r\nfirst=1\r\n[B]\r\nsecond=2\r\n", []PatchHunk{
				{
					Context:  "[A]",
					OldLines: []string{"first=1", "[B]"},
					NewLines: testCase.newLines,
				},
				{
					Context:  "[B]",
					OldLines: []string{"second=2"},
					NewLines: []string{"second=22"},
				},
			}, "mod.ini", "\r\n")
			if err == nil || !strings.Contains(err.Error(), `context "[B]" not found`) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestApplyPatchHunksDeletesBlock(t *testing.T) {
	t.Parallel()
	updated, _, err := applyPatchHunks("a\r\nremove=1\r\nkeep=2\r\n", []PatchHunk{{
		OldLines: []string{"remove=1"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "a\r\nkeep=2\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksMatcherRelaxesInOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		file  string
		old   []string
		new   []string
		want  string
		label string
	}{
		{
			name:  "trailing whitespace",
			file:  "value=old   \r\nnext=1\r\n",
			old:   []string{"value=old"},
			new:   []string{"value=new"},
			want:  "value=new\r\nnext=1\r\n",
			label: "trailing whitespace ignored",
		},
		{
			name:  "indentation",
			file:  "    if $pause == 0\r\nendif=1\r\n",
			old:   []string{"if $pause == 0"},
			new:   []string{"if $pause == 0"},
			want:  "if $pause == 0\r\nendif=1\r\n",
			label: "indentation ignored",
		},
		{
			name:  "typographic punctuation",
			file:  "speed \u2014 30\r\n",
			old:   []string{"speed - 30"},
			new:   []string{"speed - 25"},
			want:  "speed - 25\r\n",
			label: "typographic punctuation normalized",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			updated, warnings, err := applyPatchHunks(testCase.file, []PatchHunk{{
				OldLines: testCase.old,
				NewLines: testCase.new,
			}}, "mod.ini", "\r\n")
			if err != nil {
				t.Fatal(err)
			}
			if updated != testCase.want {
				t.Fatalf("updated = %q, want %q", updated, testCase.want)
			}
			if len(warnings) != 1 || !strings.Contains(warnings[0], testCase.label) {
				t.Fatalf("warnings = %v, want one warning containing %q", warnings, testCase.label)
			}
		})
	}
}

// Hunks listed in file order walk through repeated blocks one after another, as the codex patch
// format does.
func TestApplyPatchHunksTakesRepeatedBlocksInFileOrder(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n[C]\r\nkey=1\r\n"
	updated, _, err := applyPatchHunks(text, []PatchHunk{
		{OldLines: []string{"key=1"}, NewLines: []string{"key=2"}},
		{OldLines: []string{"key=1"}, NewLines: []string{"key=3"}},
	}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nkey=2\r\n[B]\r\nkey=3\r\n[C]\r\nkey=1\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksAnchorsOldLinesToEndOfFile(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n"
	updated, _, err := applyPatchHunks(text, []PatchHunk{
		{EOF: true, OldLines: []string{"key=1"}, NewLines: []string{"key=2"}},
	}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nkey=1\r\n[B]\r\nkey=2\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksReportsEveryFailedHunk(t *testing.T) {
	t.Parallel()
	_, _, err := applyPatchHunks("[A]\r\nkey=1\r\n", []PatchHunk{
		{OldLines: []string{"missing=1"}, NewLines: []string{"missing=2"}},
		{OldLines: []string{"key=1"}, NewLines: []string{"key=2"}},
		{Context: "[Missing]", OldLines: []string{"key=1"}, NewLines: []string{"key=3"}},
	}, "mod.ini", "\r\n")
	if err == nil || !strings.Contains(err.Error(), "hunk 1: expected lines not found") ||
		!strings.Contains(err.Error(), `hunk 3: context "[Missing]" not found`) ||
		strings.Contains(err.Error(), "hunk 2") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyPatchHunksDropsTrailingBlankContext(t *testing.T) {
	t.Parallel()
	updated, _, err := applyPatchHunks("[A]\r\nkey=1\r\n[B]\r\n", []PatchHunk{
		{OldLines: []string{"key=1", ""}, NewLines: []string{"key=2", ""}},
	}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nkey=2\r\n[B]\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksContextDisambiguates(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n"
	updated, _, err := applyPatchHunks(text, []PatchHunk{{
		Context:  "[B]",
		OldLines: []string{"key=1"},
		NewLines: []string{"key=2"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nkey=1\r\n[B]\r\nkey=2\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksRejectsUnknownContext(t *testing.T) {
	t.Parallel()
	_, _, err := applyPatchHunks("key=1\r\n", []PatchHunk{{
		Context:  "[Missing]",
		OldLines: []string{"key=1"},
		NewLines: []string{"key=2"},
	}}, "mod.ini", "\r\n")
	if err == nil || !strings.Contains(err.Error(), `context "[Missing]" not found in "mod.ini"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyPatchHunksRejectsUnmatchedLines(t *testing.T) {
	t.Parallel()
	_, _, err := applyPatchHunks("key=1\r\n", []PatchHunk{{
		OldLines: []string{"missing=1", "missing=2"},
		NewLines: []string{"key=2"},
	}}, "mod.ini", "\r\n")
	if err == nil ||
		!strings.Contains(err.Error(), `expected lines not found in "mod.ini": "missing=1" and 1 more lines`) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyPatchHunksAcceptsAnyOrder(t *testing.T) {
	t.Parallel()
	text := "[A]\r\na=1\r\n[B]\r\nb=1\r\n[C]\r\nc=1\r\n[D]\r\nd=1\r\n[E]\r\ne=1\r\n"
	updated, warnings, err := applyPatchHunks(text, []PatchHunk{
		{OldLines: []string{"[E]", "e=1"}, NewLines: []string{"[E]", "e=2"}},
		{OldLines: []string{"a=1"}, NewLines: []string{"a=2"}},
		{OldLines: []string{"d=1"}, NewLines: []string{"d=2", "d2=1"}},
		{OldLines: []string{"b=1"}, NewLines: nil},
		{Context: "[C]", OldLines: []string{"c=1"}, NewLines: []string{"c=2"}},
	}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	want := "[A]\r\na=2\r\n[B]\r\n[C]\r\nc=2\r\n[D]\r\nd=2\r\nd2=1\r\n[E]\r\ne=2\r\n"
	if updated != want {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksMatchesBeforeContextWhenUnique(t *testing.T) {
	t.Parallel()
	updated, _, err := applyPatchHunks("first=1\r\nsecond=2\r\n", []PatchHunk{{
		Context:  "second=2",
		OldLines: []string{"first=1"},
		NewLines: []string{"first=11"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "first=11\r\nsecond=2\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksRejectsOverlap(t *testing.T) {
	t.Parallel()
	_, _, err := applyPatchHunks("[A]\r\na=1\r\nb=1\r\nc=1\r\n", []PatchHunk{
		{OldLines: []string{"b=1", "c=1"}, NewLines: []string{"b=2", "c=2"}},
		{OldLines: []string{"a=1", "b=1"}, NewLines: []string{"a=2", "b=3"}},
	}, "mod.ini", "\r\n")
	if err == nil || !strings.Contains(err.Error(), `hunk 1 overlaps hunk 2 at line 3 of "mod.ini"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyPatchHunksRejectsAmbiguousEarlierBlock(t *testing.T) {
	t.Parallel()
	_, _, err := applyPatchHunks("[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n[C]\r\nlast=1\r\n", []PatchHunk{
		{OldLines: []string{"last=1"}, NewLines: []string{"last=2"}},
		{OldLines: []string{"key=1"}, NewLines: []string{"key=2"}},
	}, "mod.ini", "\r\n")
	if err == nil || !strings.Contains(err.Error(), `hunk 2: ambiguous match in "mod.ini" at lines 2, 4`) {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyPatchHunksKeepsInputOrderForInsertionsAtOneLine(t *testing.T) {
	t.Parallel()
	updated, _, err := applyPatchHunks("[A]\r\nkey=1\r\n", []PatchHunk{
		{Context: "[A]", NewLines: []string{"first=1"}},
		{Context: "[A]", NewLines: []string{"second=1"}},
		{OldLines: []string{"key=1"}, NewLines: []string{"key=2"}},
	}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nfirst=1\r\nsecond=1\r\nkey=2\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksInsertsAfterContextAndAtEndOfFile(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nkey=1\r\n"

	afterContext, _, err := applyPatchHunks(text, []PatchHunk{{
		Context:  "key=1",
		NewLines: []string{"key=2"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if afterContext != "[A]\r\nkey=1\r\nkey=2\r\n" {
		t.Fatalf("afterContext = %q", afterContext)
	}

	atEnd, _, err := applyPatchHunks(text, []PatchHunk{{
		EOF:      true,
		NewLines: []string{"[B]", "key=3"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if atEnd != "[A]\r\nkey=1\r\n[B]\r\nkey=3\r\n" {
		t.Fatalf("atEnd = %q", atEnd)
	}
}

func TestApplyPatchHunksInsertsAtEndOfUnterminatedFile(t *testing.T) {
	t.Parallel()
	updated, _, err := applyPatchHunks("[A]\r\nkey=1", []PatchHunk{{
		EOF:      true,
		NewLines: []string{"[B]"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nkey=1\r\n[B]" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksPreservesUntouchedLineEndings(t *testing.T) {
	t.Parallel()
	updated, _, err := applyPatchHunks("a\r\nb\nc\r\n", []PatchHunk{{
		OldLines: []string{"b"},
		NewLines: []string{"B"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "a\r\nB\nc\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksKeepsMissingFinalNewline(t *testing.T) {
	t.Parallel()
	updated, _, err := applyPatchHunks("[A]\r\nkey=1", []PatchHunk{{
		OldLines: []string{"key=1"},
		NewLines: []string{"key=2"},
	}}, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nkey=2" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestApplyPatchHunksRejectsInvalidHunks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		hunks  []PatchHunk
		errMsg string
	}{
		{name: "no hunks", hunks: nil, errMsg: "update requires at least one hunk"},
		{
			name:   "empty oldLines",
			hunks:  []PatchHunk{{NewLines: []string{"key=2"}}},
			errMsg: "empty oldLines requires a context line or eof: true",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := applyPatchHunks("key=1\r\n", testCase.hunks, "mod.ini", "\r\n")
			if err == nil || !strings.Contains(err.Error(), testCase.errMsg) {
				t.Fatalf("err = %v, want message containing %q", err, testCase.errMsg)
			}
		})
	}
}

func TestApplySearchReplace(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nhandling = skip\r\n[B]\r\nhandling = skip\r\n"

	updated, _, err := applySearchReplace(
		text, "[A]\nhandling = skip", "[A]\nhandling = skip\n; kept", "mod.ini", "\r\n", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated != "[A]\r\nhandling = skip\r\n; kept\r\n[B]\r\nhandling = skip\r\n" {
		t.Fatalf("updated = %q", updated)
	}

	replacedAll, _, err := applySearchReplace(
		text,
		"handling = skip",
		"handling = skip\n; note",
		"mod.ini",
		"\r\n",
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if replacedAll != "[A]\r\nhandling = skip\r\n; note\r\n[B]\r\nhandling = skip\r\n; note\r\n" {
		t.Fatalf("replaceAll = %q", replacedAll)
	}
}

// A model often gets the indentation of a copied block wrong; a block that is unique once
// whitespace is relaxed still applies, and the relaxation is reported.
func TestApplySearchReplaceFallsBackToRelaxedLines(t *testing.T) {
	t.Parallel()
	text := "[A]\r\n\tif $x\r\n\t\tdraw = 1\r\n\tendif\r\n\r\n    run = Cleanup\r\n"

	updated, warning, err := applySearchReplace(
		text,
		"\t\t\tdraw = 1\n\t\tendif\n\n    run = Cleanup",
		"\t\tdraw = 1\n\tendif\n\n    run = Restore\n    run = Cleanup",
		"mod.ini", "\r\n", false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warning, "matched at line 3 with indentation ignored") {
		t.Fatalf("warning = %q", warning)
	}
	want := "[A]\r\n\tif $x\r\n\t\tdraw = 1\r\n\tendif\r\n\r\n    run = Restore\r\n    run = Cleanup\r\n"
	if updated != want {
		t.Fatalf("updated = %q", updated)
	}

	withTerminator, _, err := applySearchReplace(text, "  if $x\n", "\tif $y\n", "mod.ini", "\r\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if withTerminator != "[A]\r\n\tif $y\r\n\t\tdraw = 1\r\n\tendif\r\n\r\n    run = Cleanup\r\n" {
		t.Fatalf("withTerminator = %q", withTerminator)
	}

	_, _, err = applySearchReplace("\tkey=1\r\n[B]\r\n\tkey=1\r\n", "  key=1", "key=2", "mod.ini", "\r\n", false)
	if err == nil || !strings.Contains(err.Error(), "resembles lines 1, 3") {
		t.Fatalf("ambiguous relaxed err = %v", err)
	}
}

func TestApplySearchReplaceRejects(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n"
	cases := []struct {
		name       string
		old, next  string
		replaceAll bool
		errMsg     string
	}{
		{name: "empty oldString", next: "key=2", errMsg: "oldString must not be empty"},
		{name: "identical", old: "key=1", next: "key=1", errMsg: "oldString and newString are identical"},
		{name: "missing", old: "key=missing", next: "key=2", errMsg: "oldString not found"},
		{name: "ambiguous", old: "key=1", next: "key=2", errMsg: "oldString matches 2 times"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := applySearchReplace(text, testCase.old, testCase.next, "mod.ini", "\r\n", testCase.replaceAll)
			if err == nil || !strings.Contains(err.Error(), testCase.errMsg) {
				t.Fatalf("err = %v, want message containing %q", err, testCase.errMsg)
			}
		})
	}
}
