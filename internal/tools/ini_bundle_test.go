package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModINIBundleLoadsInDirectoryMergedReferences(t *testing.T) {
	root := t.TempDir()
	childDir := filepath.Join(root, "CharBMain")
	if err := os.MkdirAll(childDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childDir, "CharB.ini"), []byte(`[TextureOverrideCharBPosition]
hash = abcdef01
`), 0o600); err != nil {
		t.Fatal(err)
	}
	mergedPath := filepath.Join(root, "merged.ini")
	if err := os.WriteFile(mergedPath, []byte("; Merged Mods: "+filepath.Join("CharBMain", "CharB.ini")+"\n[TextureOverrideMergedPosition]\nhash = fedcba98\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, sections, sources, err := loadModINIBundleWithSources(mergedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
	foundChild := false
	for _, source := range sources {
		if filepath.Base(filepath.Dir(source)) == "CharBMain" {
			foundChild = true
		}
	}
	if !foundChild {
		t.Fatalf("sources = %#v", sources)
	}
	if names := sectionNames(sections); len(names) != 2 || names[0] != "MergedPosition" || names[1] != "CharBPosition" {
		t.Fatalf("sections = %#v", names)
	}
}

func TestLoadModINIBundleLoadsAbsoluteInDirectoryMergedReferences(t *testing.T) {
	root := t.TempDir()
	childDir := filepath.Join(root, "CharBMain")
	if err := os.MkdirAll(childDir, 0o700); err != nil {
		t.Fatal(err)
	}
	childIni := filepath.Join(childDir, "CharB.ini")
	if err := os.WriteFile(childIni, []byte(`[TextureOverrideCharBPosition]
hash = abcdef01
`), 0o600); err != nil {
		t.Fatal(err)
	}
	mergedPath := filepath.Join(root, "merged.ini")
	if err := os.WriteFile(mergedPath, []byte("; Merged Mod: "+childIni+"\n[TextureOverrideMergedPosition]\nhash = fedcba98\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, sections, sources, err := loadModINIBundleWithSources(mergedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
	if names := sectionNames(sections); len(names) != 2 || names[0] != "MergedPosition" || names[1] != "CharBPosition" {
		t.Fatalf("sections = %#v", names)
	}
}

func TestLoadModINIBundleRejectsOutsideAndMissingMergedReferences(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.ini"), []byte(`[TextureOverrideOutsidePosition]
hash = 11111111
`), 0o600); err != nil {
		t.Fatal(err)
	}
	mergedPath := filepath.Join(root, "merged.ini")
	if err := os.WriteFile(mergedPath, []byte("; Merged Mods: "+filepath.Join(outside, "secret.ini")+", ..\\"+filepath.Base(outside)+"\\secret.ini, missing.ini\n[TextureOverrideMergedPosition]\nhash = fedcba98\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, sections, sources, err := loadModINIBundleWithSources(mergedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v", sources)
	}
	if names := sectionNames(sections); len(names) != 1 || names[0] != "MergedPosition" {
		t.Fatalf("sections = %#v", names)
	}
}

func TestLoadModINIBundleRejectsDirectoryMergedReferences(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "CharBMain"), 0o700); err != nil {
		t.Fatal(err)
	}
	mergedPath := filepath.Join(root, "merged.ini")
	if err := os.WriteFile(mergedPath, []byte("; Merged Mods: CharBMain\n[TextureOverrideMergedPosition]\nhash = fedcba98\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, sections, sources, err := loadModINIBundleWithSources(mergedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %#v", sources)
	}
	if names := sectionNames(sections); len(names) != 1 || names[0] != "MergedPosition" {
		t.Fatalf("sections = %#v", names)
	}
}

func TestLoadModINIBundleLoadsCommaDirectoryMergedReference(t *testing.T) {
	root := t.TempDir()
	childDir := filepath.Join(root, "CharB, (Summer Outfit)")
	if err := os.MkdirAll(childDir, 0o700); err != nil {
		t.Fatal(err)
	}
	childIni := filepath.Join(childDir, "CharB.ini")
	if err := os.WriteFile(childIni, []byte(`[TextureOverrideCharBPosition]
hash = abcdef01
vb0 = ResourcePosition
`), 0o600); err != nil {
		t.Fatal(err)
	}
	mergedPath := filepath.Join(root, "merged.ini")
	if err := os.WriteFile(mergedPath, []byte("; Merged Mod: .\\"+filepath.Join("CharB, (Summer Outfit)", "CharB.ini")+"\n[TextureOverrideMergedPosition]\nhash = fedcba98\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, sections, sources, err := loadModINIBundleWithSources(mergedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 2 {
		t.Fatalf("sources = %#v", sources)
	}
	if names := sectionNames(sections); len(names) != 2 || names[0] != "MergedPosition" || names[1] != "CharBPosition" {
		t.Fatalf("sections = %#v", names)
	}
}

func sectionNames(sections []modINISection) []string {
	names := make([]string, len(sections))
	for index, section := range sections {
		names[index] = section.Name
	}
	return names
}
