package xxmi

import "testing"

func TestGetMatchingImporterMatchesElectronWuwaKeywords(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"명조", "묑조 모드", "Wuthering Waves"} {
		got := GetMatchingImporter(name, []string{"WWMI"})
		if got == nil || *got != "WWMI" {
			t.Fatalf("GetMatchingImporter(%q) = %v, want WWMI", name, got)
		}
	}
	if got := GetMatchingImporter("뭉조", []string{"WWMI"}); got != nil {
		t.Fatalf("non-Electron typo matched: %q", *got)
	}
}
