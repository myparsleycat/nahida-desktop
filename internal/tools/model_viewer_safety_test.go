package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelViewerSafetyAllowsOneParentAndRejectsFurtherEscape(t *testing.T) {
	root := t.TempDir()
	parentFile := filepath.Join(filepath.Dir(root), "escape.buf")
	resolved, err := resolveModelViewerResourcePath(root, root, `..\escape.buf`)
	if err != nil || resolved != parentFile {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
	if _, err := resolveModelViewerResourcePath(root, root, `..\..\escape.buf`); err == nil {
		t.Fatal("expected traversal beyond one parent to be rejected")
	}
	inside, err := resolveModelViewerResourcePath(root, filepath.Join(root, "parts"), `..\inside.buf`)
	if err != nil || inside != filepath.Join(root, "inside.buf") {
		t.Fatalf("inside=%q err=%v", inside, err)
	}
}

func TestSanitizeModelViewerLogValueStripsControlCharacters(t *testing.T) {
	got := sanitizeModelViewerLogValue("Body" + string(rune(10)) + "Diffuse" + string(rune(27)) + "[31m")
	if strings.ContainsRune(got, 10) || strings.ContainsRune(got, 27) || got != "BodyDiffuse[31m" {
		t.Fatalf("got %q", got)
	}
}

func TestModelViewerSafetySanitizesUnsafeResourcesInsteadOfFailingLoad(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.ToSlash(filepath.Join(filepath.Dir(dir), "outside.dds"))
	result := loadViewerMod(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
ps-t0 = ResourceUnsafe
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceUnsafe]
filename = `+abs)
	if len(result.Meshes) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestModelViewerSafetyRejectsOversizedBufferBeforeRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "huge.buf")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(maxModelViewerBufferFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	budget, err := newModelViewerLoadBudget(root)
	if err != nil {
		t.Fatal(err)
	}
	err = budget.validateResources(root, []modelViewerResource{{Name: "Huge", Filename: "huge.buf", Stride: 40}})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

func TestModelViewerSafetyRejectsExcessiveDrawCount(t *testing.T) {
	var ini strings.Builder
	ini.WriteString("[TextureOverrideBody]\nib = ResourceIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n")
	for range maxModelViewerDraws + 1 {
		ini.WriteString("drawindexed = 3, 0, 0\n")
	}
	_, err := collectModelViewerDirectDrawRecords(parseModINI(ini.String()), nil)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(maxModelViewerDraws)) {
		t.Fatalf("err = %v", err)
	}
}

func TestModelViewerSafetyDoesNotCountConditionalBufferVariantsAsAuthoredDraws(t *testing.T) {
	var ini strings.Builder
	ini.WriteString("[TextureOverrideBody]\nib = ResourceIB\n")
	for index := range 13 {
		fmt.Fprintf(&ini, "if $choice%d == 1\n", index)
		ini.WriteString("vb0 = ResourceA\nelse\nvb0 = ResourceB\nendif\n")
	}
	ini.WriteString("drawindexed = 3, 0, 0\n")
	records, err := collectModelViewerDirectDrawRecords(parseModINI(ini.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("expected conditional buffer variants")
	}
}
