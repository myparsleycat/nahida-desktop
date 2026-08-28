package mod

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWriteClassicMergedINIWritesMergedAndDisablesSources(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "A")
	bDir := filepath.Join(root, "B")
	aIni := writeMergePackINI(t, aDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
[ResourcePosition]
filename = A.buf
`)
	bIni := writeMergePackINI(t, bDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
[ResourcePosition]
filename = B.buf
`)
	created := []mergeRollback{}
	if err := writeClassicMergedINI(root, []classicSource{
		{path: aIni, index: 0}, {path: bIni, index: 1},
	}, "vk_right", "vk_left", &created); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "merged.ini"))
	if err != nil {
		t.Fatal(err)
	}
	headerLines := []string{}
	for _, line := range strings.Split(string(text), "\n") {
		if strings.HasPrefix(line, "; Merged Mod:") {
			headerLines = append(headerLines, line)
		}
	}
	if len(headerLines) != 1 {
		t.Fatalf("headers = %#v", headerLines)
	}
	if !regexp.MustCompile(`A[\\/]CharA\.ini, .*B[\\/]CharA\.ini`).MatchString(headerLines[0]) {
		t.Fatalf("header = %s", headerLines[0])
	}
	for _, want := range []string{
		"; Constants ---------------------------",
		"; Shader ------------------------------",
		"; Overrides ---------------------------",
		"; CommandList -------------------------",
		"; Resources ---------------------------",
		"$swapvar = 0,1",
		"[KeySwap]",
		"back = vk_left",
		"[CommandListCharAPosition]",
		"[ResourcePosition.0]",
	} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if _, err := os.Stat(aIni); !os.IsNotExist(err) {
		t.Fatal("source ini remains")
	}
	if _, err := os.Stat(filepath.Join(aDir, "DISABLED_BACKUP_CharA.ini")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteClassicMergedINIDoesNotOverwriteExistingDisabledSource(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "A")
	bDir := filepath.Join(root, "B")
	aIni := writeMergePackINI(t, aDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
`)
	bIni := writeMergePackINI(t, bDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
`)
	disabledA := filepath.Join(aDir, "DISABLEDCharA.ini")
	if err := os.WriteFile(disabledA, []byte("user-disabled"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := []mergeRollback{}
	if err := writeClassicMergedINI(root, []classicSource{
		{path: aIni, index: 0}, {path: bIni, index: 1},
	}, "vk_right", "", &created); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(aIni); !os.IsNotExist(err) {
		t.Fatal("source remains")
	}
	got, err := os.ReadFile(disabledA)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user-disabled" {
		t.Fatalf("disabled = %s", got)
	}
	if _, err := os.Stat(filepath.Join(aDir, "DISABLED_BACKUP_CharA.ini")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteClassicMergedINIFormatsRelativeFilenameAndKeepsResourceRefs(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "A")
	aIni := writeMergePackINI(t, aDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
handling = skip
ps-t0 = 1
this = CommandListFace

[TextureOverrideCharAVertexLimitRaise]
hash = fedcba98
override_vertex_count = 50000
override_byte_stride = 40

[ResourcePosition]
filename = Relative.buf
`)
	created := []mergeRollback{}
	if err := writeClassicMergedINI(root, []classicSource{{path: aIni, index: 0}}, "vk_right", "", &created); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "merged.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`filename = \.\\A[\\/]Relative\.buf`).Match(text) {
		t.Fatalf("filename = %s", text)
	}
	if !regexp.MustCompile(`\[TextureOverrideCharAVertexLimitRaise\]\nhash = fedcba98\noverride_vertex_count = 50000\noverride_byte_stride = 40`).Match(text) {
		t.Fatalf("vertex limit = %s", text)
	}
	if !strings.Contains(string(text), "handling = skip") || !strings.Contains(string(text), "ps-t0 = 1") ||
		!strings.Contains(string(text), "this = CommandListFace") {
		t.Fatalf("commands = %s", text)
	}
	if strings.Contains(string(text), "ps-t0 = 1.0") || strings.Contains(string(text), "this = CommandListFace.0") {
		t.Fatalf("rewritten refs = %s", text)
	}
}

func TestWriteClassicMergedINIDoesNotRewriteComparisonControlFlow(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "A")
	aIni := writeMergePackINI(t, aDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
if DRAW_TYPE == 1
	vb0 = ResourcePosition
endif
[ResourcePosition]
filename = A.buf
`)
	created := []mergeRollback{}
	if err := writeClassicMergedINI(root, []classicSource{{path: aIni, index: 0}}, "vk_right", "", &created); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "merged.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`\[CommandListCharAPosition\]\nif \$swapvar == 0\n\tif DRAW_TYPE == 1\n\t\tvb0 = ResourcePosition\.0\n\tendif\nendif`).Match(text) {
		t.Fatalf("command list = %s", text)
	}
	if strings.Contains(string(text), "if DRAW_TYPE = = 1") || strings.Contains(string(text), "if DRAW_TYPE = 1") {
		t.Fatalf("rewritten DRAW_TYPE = %s", text)
	}
}

func TestWriteClassicMergedINIKeepsNestedDrawTypeBranches(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "CharCf0000Mod")
	aIni := writeMergePackINI(t, aDir, "CharC.ini", `[TextureOverrideCharCHairBlend]
hash = beef0002
handling = skip
vb2 = ResourceCharCHairBlend
if DRAW_TYPE == 1
	vb0 = ResourceCharCHairPosition
	draw = 2984, 0
endif
if DRAW_TYPE == 8
	Resource\SRMI\PositionBuffer = ref ResourceCharCHairPositionCS
	$\SRMI\vertcount = 2984
endif

[ResourceCharCHairBlend]
type = Buffer
stride = 32
filename = CharCHairBlend.buf

[ResourceCharCHairPosition]
type = Buffer
stride = 40
filename = CharCHairPosition.buf

[ResourceCharCHairPositionCS]
type = StructuredBuffer
stride = 40
filename = CharCHairPosition.buf
`)
	created := []mergeRollback{}
	if err := writeClassicMergedINI(root, []classicSource{{path: aIni, index: 0}}, "vk_right", "", &created); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "merged.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "[ResourceCharCHairBlend.0]") ||
		!strings.Contains(string(text), "[ResourceCharCHairPositionCS.0]") {
		t.Fatalf("resources = %s", text)
	}
	if strings.Contains(string(text), "if DRAW_TYPE = = 1") || strings.Contains(string(text), "if DRAW_TYPE = 1") {
		t.Fatalf("rewritten DRAW_TYPE = %s", text)
	}
}

func TestWriteClassicMergedINIAddsCreditInfoToPresent(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "A")
	aIni := writeMergePackINI(t, aDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition

[CommandListCreditInfo]
if $creditinfo == 0
	; credits
endif
`)
	created := []mergeRollback{}
	if err := writeClassicMergedINI(root, []classicSource{{path: aIni, index: 0}}, "vk_right", "", &created); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "merged.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`\[Present\]\npost \$active = 0\nrun = CommandListCreditInfo`).Match(text) {
		t.Fatalf("present = %s", text)
	}
	if !strings.Contains(string(text), "[CommandListCreditInfo]") {
		t.Fatalf("credit command missing: %s", text)
	}
}

func TestWriteClassicMergedINIEmitsOneCommaSeparatedMergedModLine(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "CharA, (Red Dress)")
	bDir := filepath.Join(root, "CharA, (Blue Dress)")
	aIni := writeMergePackINI(t, aDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
`)
	bIni := writeMergePackINI(t, bDir, "CharA.ini", `[TextureOverrideCharAPosition]
hash = abcdef01
vb0 = ResourcePosition
`)
	created := []mergeRollback{}
	if err := writeClassicMergedINI(root, []classicSource{
		{path: aIni, index: 0}, {path: bIni, index: 1},
	}, "vk_right", "", &created); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(root, "merged.ini"))
	if err != nil {
		t.Fatal(err)
	}
	headerLines := []string{}
	for _, line := range strings.Split(string(text), "\n") {
		if strings.HasPrefix(line, "; Merged Mod:") {
			headerLines = append(headerLines, line)
		}
	}
	if len(headerLines) != 1 {
		t.Fatalf("headers = %#v", headerLines)
	}
	if !strings.Contains(headerLines[0], "CharA, (Red Dress)") || !strings.Contains(headerLines[0], "CharA, (Blue Dress)") {
		t.Fatalf("header = %s", headerLines[0])
	}
	got := extractMergedModPaths(string(text))
	want := []string{`.\CharA, (Red Dress)\CharA.ini`, `.\CharA, (Blue Dress)\CharA.ini`}
	if !mergeStringSlicesEqual(got, want) {
		t.Fatalf("extracted = %#v want %#v", got, want)
	}
}

func TestParseClassicSectionsIgnoresCommentContainingEquals(t *testing.T) {
	t.Parallel()

	sections := parseClassicSections("[TextureOverrideBody]\n; vb0 = ResourceComment\nhash = abcdef01\nvb0 = ResourceBody\n", ".", 0)
	if len(sections) != 1 {
		t.Fatalf("sections = %#v", sections)
	}
	for _, entry := range sections[0].entries {
		if strings.HasPrefix(entry.key, ";") || entry.value == "ResourceComment" {
			t.Fatalf("comment parsed as entry: %#v", entry)
		}
	}
}
