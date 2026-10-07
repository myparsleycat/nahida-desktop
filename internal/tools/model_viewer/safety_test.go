package modelviewer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func TestModelViewerSafetyRejectsDirectoryLinkEscape(t *testing.T) {
	for _, kind := range []string{"junction", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			modDir := filepath.Join(base, "MyMod")
			outsideDir := filepath.Join(base, "Outside")
			for _, dir := range []string{modDir, outsideDir} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(outsideDir, "position.buf"), make([]byte, 3*40), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(modDir, "linked")
			createViewerDirectoryLink(t, kind, outsideDir, link)
			if _, err := os.ReadFile(filepath.Join(link, "position.buf")); err != nil {
				t.Fatalf("directory link does not redirect reads: %v", err)
			}

			if _, err := resolveModelViewerResourcePath(modDir, modDir, `linked\position.buf`); err == nil {
				t.Fatal("directory link escaping the mod folder was resolved")
			}
			if path, ok := resolveModelViewerModBufferPath(modDir, `linked\position.buf`); ok {
				t.Fatalf("directory link escape was accepted as a mod buffer path: %s", path)
			}
		})
	}
}

func TestModelViewerSafetyKeepsDirectoryLinkedModFiles(t *testing.T) {
	for _, kind := range []string{"junction", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			modDir := filepath.Join(base, "MyMod")
			meshDir := filepath.Join(modDir, "meshes")
			if err := os.MkdirAll(meshDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(meshDir, "position.buf"), make([]byte, 3*40), 0o600); err != nil {
				t.Fatal(err)
			}
			createViewerDirectoryLink(t, kind, meshDir, filepath.Join(modDir, "linked"))
			path, ok := resolveModelViewerModBufferPath(modDir, `linked\position.buf`)
			if !ok {
				t.Fatal("directory link inside the mod folder was rejected")
			}
			if data, err := os.ReadFile(path); err != nil || len(data) != 3*40 {
				t.Fatalf("resolved path %q is not readable: %v", path, err)
			}

			alias := filepath.Join(base, "Alias")
			createViewerDirectoryLink(t, kind, modDir, alias)
			path, ok = resolveModelViewerModBufferPath(alias, `linked\position.buf`)
			if !ok {
				t.Fatal("mod folder reached through a directory link was rejected")
			}
			if data, err := os.ReadFile(path); err != nil || len(data) != 3*40 {
				t.Fatalf("resolved path %q is not readable: %v", path, err)
			}
		})
	}
}

// createViewerDirectoryLink links target into link as the requested kind.
// Windows junctions need no symbolic link privilege, unlike directory symlinks,
// so tests that cover both skip when the platform cannot create one.
func createViewerDirectoryLink(t *testing.T, kind, target, link string) {
	t.Helper()
	if kind == "symlink" {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("directory symlinks unavailable: %v", err)
		}
		return
	}
	if runtime.GOOS != "windows" {
		t.Skip("junctions are Windows only")
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, out)
	}
}

func TestSanitizeModelViewerLogValueStripsControlCharacters(t *testing.T) {
	got := sanitizeModelViewerLogValue("Body" + string(rune(10)) + "Diffuse" + string(rune(27)) + "[31m")
	if strings.ContainsRune(got, 10) || strings.ContainsRune(got, 27) || got != "BodyDiffuse[31m" {
		t.Fatalf("got %q", got)
	}
}

func TestModelViewerSafetySanitizesUnsafeResourcesInsteadOfFailingLoad(t *testing.T) {
	// Every case points at a decodable texture, so only the path policy decides whether it loads.
	cases := []struct {
		name     string
		filename func(base, dir string) string
		wantKey  string
	}{
		{
			name:     "relative path inside the mod",
			filename: func(_, _ string) string { return "inside.png" },
			wantKey:  "diffuse::inside.png",
		},
		{
			name:     "absolute path inside the mod",
			filename: func(_, dir string) string { return filepath.ToSlash(filepath.Join(dir, "inside.png")) },
		},
		{
			name:     "absolute path outside the mod",
			filename: func(base, _ string) string { return filepath.ToSlash(filepath.Join(base, "outside.png")) },
		},
		{
			name:     "traversal beyond one parent",
			filename: func(_, _ string) string { return `..\..\..\outside.png` },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "Game", "Mods", "MyMod")
			writeTextureFile(t, dir, "inside.png", encodeTinyPNG())
			writeTextureFile(t, base, "outside.png", encodeTinyPNG())

			result := loadViewerMod(t, dir, `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
ps-t0 = ResourceBodyDiffuse
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceBodyDiffuse]
filename = `+tc.filename(base, dir))
			if len(result.Meshes) != 1 {
				t.Fatalf("result = %#v", result)
			}
			if key := texKey(result.Meshes[0]); key != tc.wantKey {
				t.Fatalf("texKey = %q, want %q (textures=%#v)", key, tc.wantKey, result.Textures)
			}
			if tc.wantKey == "" && len(result.Textures) != 0 {
				t.Fatalf("unsafe texture was loaded: %#v", result.Textures)
			}
		})
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
