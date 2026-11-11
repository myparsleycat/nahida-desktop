//go:build windows

package platform

import (
	"slices"
	"testing"
)

// The drive screen sorts names as the React screen's Intl.Collator with numeric, sensitivity "case" and
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
