//go:build windows

package platform

import (
	"slices"
	"testing"
)

// Names shown to the user sort as the React drive screen's Intl.Collator with numeric, sensitivity "case" and
// caseFirst "lower" did. ICU ignores the attributes silently when their constants are wrong.
func TestNaturalCompareOrdersDigitsByValue(t *testing.T) {
	t.Parallel()

	compare := newNaturalCompareFor("en-US")
	values := []string{"item10", "Item2", "item2", "item1"}
	slices.SortStableFunc(values, compare)
	want := []string{"item1", "item2", "Item2", "item10"}
	if !slices.Equal(values, want) {
		t.Fatalf("natural order = %q, want %q", values, want)
	}
	if got := compare("résumé", "resume"); got != 0 {
		t.Fatalf("compare(résumé, resume) = %d, want 0 with accents ignored", got)
	}
}

// Names that differ only in white space keep a fixed order: equal without spaces, they fall back to the full names.
func TestNameCompareIgnoresSpacingFirst(t *testing.T) {
	t.Parallel()

	natural := newNaturalCompareFor("en-US")
	for _, tc := range []struct {
		name          string
		ignoreSpacing bool
		want          []string
	}{
		{"ignore spacing", true, []string{"a1", "a 2", "a2", "a 10"}},
		{"keep spacing", false, []string{"a 2", "a 10", "a1", "a2"}},
	} {
		values := []string{"a 10", "a2", "a 2", "a1"}
		slices.SortStableFunc(values, nameCompare(natural, tc.ignoreSpacing))
		if !slices.Equal(values, tc.want) {
			t.Fatalf("%s: order = %q, want %q", tc.name, values, tc.want)
		}
	}
}
