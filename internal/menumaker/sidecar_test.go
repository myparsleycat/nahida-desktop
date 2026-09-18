package menumaker

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const sidecarFixture = `namespace = Example\Master
[Constants]
global $active
global $managed_slot_id = 5
global persist $swap = 0
[Present]
if $managed_slot_id == $\manager\active_slot
post $active = 0
endif
[KeySwap]
key = ]
back = [
condition = $active == 1 && $managed_slot_id == $\manager\active_slot
type = cycle
$swap = 0,1,2
[TextureOverrideBodyPosition]
hash = af0ef73c
if $managed_slot_id == $\manager\active_slot
$active = 1
endif
`

func TestSingleINIContainsOriginalAndGeneratedMenu(t *testing.T) {
	t.Parallel()
	document := parseDocument(sidecarFixture)
	generated, err := generateSingleINI(sidecarFixture, document.Slots, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"namespace = Example\\Master",
		"[TextureOverrideBodyPosition]",
		"run = CommandListCycleKeySwap",
		generatedBegin,
		"[KeyGuiMenu]",
		generatedEnd,
	} {
		if !strings.Contains(generated.INIText, expected) {
			t.Fatalf("generated INI missing %q", expected)
		}
	}
	if strings.Count(generated.INIText, "[Present]") != 2 {
		t.Fatal("original and generated Present sections were not both preserved")
	}
}

func TestSingleINIReapplyIsIdempotent(t *testing.T) {
	t.Parallel()
	settings := defaultSettings()
	settings.RemoveOriginalKeys = true
	firstDocument := parseDocument(sidecarFixture)
	first, err := generateSingleINI(sidecarFixture, firstDocument.Slots, settings)
	if err != nil {
		t.Fatal(err)
	}
	secondDocument := parseDocument(first.INIText)
	second, err := generateSingleINI(first.INIText, secondDocument.Slots, settings)
	if err != nil {
		t.Fatal(err)
	}
	if second.INIText != first.INIText {
		t.Fatal("reapplying a generated INI changed the output")
	}
	if strings.Count(second.INIText, generatedBegin) != 1 {
		t.Fatal("generated menu block was duplicated")
	}
}

func TestSingleINIReappliesInstrumentedSidecarSource(t *testing.T) {
	t.Parallel()
	first, err := generateSingleINI(sidecarFixture, parseDocument(sidecarFixture).Slots, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	withoutMenu := strings.TrimSpace(strings.Split(first.INIText, generatedBegin)[0]) + "\n"
	document := parseDocument(withoutMenu)
	regenerated, err := generateSingleINI(withoutMenu, document.Slots, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if regenerated.INIText != first.INIText || strings.Count(regenerated.INIText, generatedBegin) != 1 {
		t.Fatal("instrumented source was not regenerated cleanly")
	}
}

func TestSingleINIRejectsUnknownVisibility(t *testing.T) {
	t.Parallel()
	text := "namespace = Unknown\n[Constants]\nglobal $active = 1\n[KeySwap]\nkey = 5\ncondition = $active\n$x = 0,1\n"
	_, err := generateSingleINI(text, parseDocument(text).Slots, defaultSettings())
	if !errors.Is(err, ErrNoVisibility) {
		t.Fatalf("expected missing visibility, got %v", err)
	}
}

func TestSingleINIIgnoresUnrelatedDisabledCondition(t *testing.T) {
	t.Parallel()
	text := sidecarFixture + "\n[CommandListDisabled]\ncondition = 0\n"
	generated, err := generateSingleINI(text, parseDocument(text).Slots, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated.INIText, "[CommandListDisabled]\ncondition = 0") {
		t.Fatal("preserved disabled condition was removed")
	}
}

func TestOwnedSidecarRequiresExactSourceMarker(t *testing.T) {
	t.Parallel()
	if !isOwnedSidecar(sidecarMarker+"Example.ini\r\n[Present]", `C:\\mods\\Example.ini`, "") {
		t.Fatal("matching sidecar marker was not recognized")
	}
	if !isOwnedSidecar(
		sidecarMarker+"Creator\\Example\r\n[Present]",
		`C:\\mods\\Example.ini`,
		"namespace = Creator\\Example\n[KeySwap]\n",
	) {
		t.Fatal("legacy namespace marker was not recognized")
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "d3dx.ini"), []byte("[Include]\n"))
	implicitPath := filepath.Join(root, "Mods", "Example.ini")
	if !isOwnedSidecar(sidecarMarker+"Mods\\Example.ini\n", implicitPath, "[KeySwap]\n") {
		t.Fatal("legacy implicit namespace marker was not recognized")
	}
	if isOwnedSidecar(sidecarMarker+"Other.ini\n", `C:\\mods\\Example.ini`, "") ||
		isOwnedSidecar("[Present]\n", `C:\\mods\\Example.ini`, "") {
		t.Fatal("unrelated menu was recognized as owned")
	}
}
