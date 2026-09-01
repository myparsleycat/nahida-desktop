package menumaker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

const strictFixture = `; preamble

[Constants]
global $swap = 0
global $black = 0

[Present]
; user logic
post $user = 1

[KeySwap]
key = no_modifiers 5
condition = $active == 1
type = cycle
back = no_modifiers 4
wrap = false
$swap = 0, 1, 1
run = CommandListAfterSwap
unknown action

[KeySwap]
key = 5
condition = 1 == $black_active
type = toggle
$black = 1

[KeyOther]
key = no_modifier 6
type = activate
$pulse = 1
`

func TestParseDocumentPreservesPreambleAndDuplicateSections(t *testing.T) {
	t.Parallel()
	sections := parseSections(strings.ReplaceAll(strictFixture, "\n", "\r\n"))
	if !slices.Equal(sections[0].Lines, []string{"; preamble", ""}) {
		t.Fatalf("unexpected preamble: %#v", sections[0].Lines)
	}
	duplicates := 0
	for _, section := range sections {
		if section.Name != nil && *section.Name == "KeySwap" {
			duplicates++
		}
	}
	if duplicates != 2 {
		t.Fatalf("expected 2 KeySwap sections, got %d", duplicates)
	}
	document := parseDocument(strictFixture)
	kinds := make([]string, 0, len(document.Handlers[0].Entries))
	for _, entry := range document.Handlers[0].Entries {
		kinds = append(kinds, entry.Kind)
	}
	if !slices.Equal(kinds, []string{"assign", "run", "raw"}) {
		t.Fatalf("unexpected entry kinds: %v", kinds)
	}
	if !slices.Equal(document.Handlers[0].Assignments[0].Values, []string{"0", "1", "1"}) {
		t.Fatalf("unexpected assignment values: %v", document.Handlers[0].Assignments[0].Values)
	}
}

func TestParseDocumentGroupsNormalizedKeys(t *testing.T) {
	t.Parallel()
	if got := normalizeMenuMakerKey(" no_modifiers   5 "); got != "5" {
		t.Fatalf("normalize no_modifiers: %q", got)
	}
	if got := normalizeMenuMakerKey("no_modifier 5"); got != "no_modifier 5" {
		t.Fatalf("normalize no_modifier: %q", got)
	}
	document := parseDocument(strictFixture)
	if len(document.Slots) != 2 {
		t.Fatalf("unexpected slot count: %d", len(document.Slots))
	}
	if len(document.Slots[0].Handlers) != 2 {
		t.Fatalf("unexpected first slot handlers: %d", len(document.Slots[0].Handlers))
	}
	if document.Slots[1].Key != "no_modifier 6" {
		t.Fatalf("unexpected second slot key: %q", document.Slots[1].Key)
	}
}

func TestExtractActiveInputsNormalizesComparisons(t *testing.T) {
	t.Parallel()
	got := extractActiveInputs("1 == $black_active && $active == 0 || $form_active")
	want := []string{"$black_active == 1", "$active == 0", "$form_active"}
	if !slices.Equal(got, want) {
		t.Fatalf("extractActiveInputs: %v", got)
	}
	sections := parseSections("[KeyA]\ncondition = $active == 0 && $active\n$swap = 0,1")
	if collected := collectActiveInputs(sections); !slices.Equal(collected, []string{"$active == 0"}) {
		t.Fatalf("collectActiveInputs: %v", collected)
	}
}

func TestParseRejectsEmptyKeySections(t *testing.T) {
	t.Parallel()
	_, err := New().Parse(context.Background(), "[Constants]\nglobal $x = 0\n")
	if !errors.Is(err, ErrNoKeySections) {
		t.Fatalf("expected no key sections, got %v", err)
	}
}
