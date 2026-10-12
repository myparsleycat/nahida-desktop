package infra

import (
	"strings"
	"testing"
)

func TestAnonymizeLogTextRemovesIdentities(t *testing.T) {
	homes := prepareHomeNeedles([]string{`C:\Users\Ann`})
	identities := append(
		newLogIdentities([]string{"Ann", "김나히"}, userNameToken),
		newLogIdentities([]string{"ANN-PC"}, computerNameToken)...,
	)

	input := strings.Join([]string{
		`2026-10-12 10:00:00.000 ERROR [Mod] open C:\Users\Ann\Mods\a.ini: denied`,
		`2026-10-12 10:00:01.000 WARN {"path":"D:\\Users\\guest\\Mods","owner":"ann"}`,
		`2026-10-12 10:00:02.000 WARN host ANN-PC channel planner E:\Games\김나히\Mods`,
		`2026-10-12 10:00:03.000 ERROR Authorization: Bearer abc.def`,
		`2026-10-12 10:00:04.000 WARN stat C:\Users\John Smith\Documents\a.ini: missing`,
	}, "\n")
	got := anonymizeLogText(input, homes, identities)

	for _, leaked := range []string{`Users\Ann`, `guest`, `"ann"`, "ANN-PC", "김나히", "abc.def", "Smith"} {
		if strings.Contains(got, leaked) {
			t.Errorf("anonymized log still contains %q:\n%s", leaked, got)
		}
	}
	// A name inside a longer word is not the account, and replacing it would
	// only damage the log.
	for _, kept := range []string{"channel", "planner", `%USERPROFILE%\Mods\a.ini`, `D:\\Users\\%USERNAME%\\Mods`} {
		if !strings.Contains(got, kept) {
			t.Errorf("anonymized log lost %q:\n%s", kept, got)
		}
	}
}

func TestNewLogIdentitiesSkipsNamesTooShortToMatchSafely(t *testing.T) {
	if got := newLogIdentities([]string{"a", " ", ""}, userNameToken); len(got) != 0 {
		t.Fatalf("identities = %d, want none", len(got))
	}
}
