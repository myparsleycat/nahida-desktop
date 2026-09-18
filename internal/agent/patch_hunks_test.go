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

func TestApplyPatchHunksRejectsAmbiguousMatch(t *testing.T) {
	t.Parallel()
	text := "[A]\r\nkey=1\r\n[B]\r\nkey=1\r\n"
	_, _, err := applyPatchHunks(text, []PatchHunk{
		{OldLines: []string{"key=1"}, NewLines: []string{"key=2"}},
	}, "mod.ini", "\r\n")
	if err == nil {
		t.Fatal("ambiguous hunk unexpectedly applied")
	}
	if !strings.Contains(err.Error(), "ambiguous match") || !strings.Contains(err.Error(), "lines 2, 4") {
		t.Fatalf("err = %v", err)
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

func TestApplyPatchHunksRequiresFileOrder(t *testing.T) {
	t.Parallel()
	_, _, err := applyPatchHunks("first=1\r\nsecond=2\r\n", []PatchHunk{
		{OldLines: []string{"second=2"}, NewLines: []string{"second=22"}},
		{OldLines: []string{"first=1"}, NewLines: []string{"first=11"}},
	}, "mod.ini", "\r\n")
	if err == nil || !strings.Contains(err.Error(), "hunks must be ordered by file position") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyPatchHunksRequiresFileOrderAfterContext(t *testing.T) {
	t.Parallel()
	_, _, err := applyPatchHunks("first=1\r\nsecond=2\r\n", []PatchHunk{{
		Context:  "second=2",
		OldLines: []string{"first=1"},
		NewLines: []string{"first=11"},
	}}, "mod.ini", "\r\n")
	if err == nil || !strings.Contains(err.Error(), "hunks must be ordered by file position") {
		t.Fatalf("err = %v", err)
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
		{
			name:   "eof with oldLines",
			hunks:  []PatchHunk{{EOF: true, OldLines: []string{"key=1"}, NewLines: []string{"key=2"}}},
			errMsg: "eof: true requires empty oldLines",
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
