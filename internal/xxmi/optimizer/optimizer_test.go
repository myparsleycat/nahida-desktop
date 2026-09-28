package optimizer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOptimizePreviewApplyAndCache(t *testing.T) {
	root := t.TempDir()
	mods := filepath.Join(root, "Mods")
	if err := os.MkdirAll(mods, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(mods, "effect.ini")
	original := []byte("\xef\xbb\xbf[TextureOverrideExample]\r\nchecktextureoverride = ib\r\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	options := Options{Importer: "WWMI", ImporterFolder: root,
		CachePath: filepath.Join(root, "cache", "WWMI.json"), Prefix: "DISABLED ", DryRun: true}
	preview, err := Optimize(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if preview.EditedFiles != 1 || preview.EditedLines != 1 || len(preview.Changes) != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != string(original) {
		t.Fatalf("dry run changed file: %q, %v", data, err)
	}
	options.DryRun = false
	if _, err := Optimize(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `$\WWMIv1\enable_ib_callbacks = 1`) {
		t.Fatalf("INI was not sanitized: %q, %v", data, err)
	}
	backup, err := os.ReadFile(path + ".xxmi_bak")
	if err != nil || string(backup) != string(original) {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	again, err := Optimize(context.Background(), options)
	if err != nil || len(again.Changes) != 0 {
		t.Fatalf("cached run = %+v, %v", again, err)
	}
}

func TestOptimizeDisablesRogueAndSkipsExcluded(t *testing.T) {
	root := t.TempDir()
	mods := filepath.Join(root, "Mods")
	for _, folder := range []string{mods, filepath.Join(mods, "DISABLED test")} {
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, "rogue.ini"), []byte("[Loader]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	options := Options{Importer: "GIMI", ImporterFolder: root,
		CachePath: filepath.Join(root, "cache.json"), Prefix: "DISABLED_"}
	report, err := Optimize(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if report.DisabledFiles != 1 {
		t.Fatalf("report = %+v", report)
	}
	if _, err := os.Stat(filepath.Join(mods, "DISABLED_rogue.ini")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mods, "DISABLED test", "rogue.ini")); err != nil {
		t.Fatal(err)
	}
}
