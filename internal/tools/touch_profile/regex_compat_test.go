package touchprofile

import "testing"

func TestTouchAssetPrefixRemovesOnlyFirstPositionToken(t *testing.T) {
	t.Parallel()

	component := TouchComponentAnalysis{Name: "PositionHatPosition", Kind: "other"}
	if got := touchAssetPrefix(component, "Alice"); got != "AliceHatPosition" {
		t.Fatalf("touchAssetPrefix = %q, want %q", got, "AliceHatPosition")
	}
}
