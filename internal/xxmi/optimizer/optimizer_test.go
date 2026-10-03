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

func TestOptimizeDisablesRogueD3DXIncludeOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		line string
	}{
		{name: "recursive mods", line: "include_recursive = Mods"},
		{name: "disabled exclusion", line: "exclude_recursive = DISABLED*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			mods := filepath.Join(root, "Mods")
			if err := os.MkdirAll(mods, 0o700); err != nil {
				t.Fatal(err)
			}
			ini := filepath.Join(mods, "shipped.ini")
			if err := os.WriteFile(ini, []byte("[Include]\n"+tc.line+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := Optimize(context.Background(), Options{
				Importer: "GIMI", ImporterFolder: root,
				CachePath: filepath.Join(root, "cache.json"), Prefix: "DISABLED_",
			})
			if err != nil || report.DisabledFiles != 1 {
				t.Fatalf("report = %+v, err = %v", report, err)
			}
			if _, err := os.Stat(filepath.Join(mods, "DISABLED_shipped.ini")); err != nil {
				t.Fatalf("rogue INI was not disabled: %v", err)
			}
		})
	}
}

func TestOptimizeDisablesModWithGlobalShaderRegexTrigger(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "Mods", "Example")
	if err := os.MkdirAll(mod, 0o700); err != nil {
		t.Fatal(err)
	}
	ini := filepath.Join(mod, "example.ini")
	if err := os.WriteFile(
		ini,
		[]byte("[ShaderRegexExample]\nrun = CommandListGlobal\n[CommandListGlobal]\nchecktextureoverride = ps-t0\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	options := Options{Importer: "GIMI", ImporterFolder: root,
		CachePath: filepath.Join(root, "cache.json"), Prefix: "DISABLED ", DryRun: true}
	preview, err := Optimize(context.Background(), options)
	if err != nil || preview.DisabledMods != 1 || len(preview.Changes) != 1 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if _, err := os.Stat(mod); err != nil {
		t.Fatal(err)
	}
	options.DryRun = false
	if _, err := Optimize(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Mods", "DISABLED Example", "example.ini")); err != nil {
		t.Fatal(err)
	}
}

func TestOptimizeDisablesDuplicateZZMILibrary(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ZZMI with spaces")
	library := filepath.Join(root, "Core", "ZZMI", "Libraries", "lib.ini")
	duplicate := filepath.Join(root, "Mods", "Pack", "lib.ini")
	late := filepath.Join(root, "Mods", "Late", "mod.ini")
	for path, content := range map[string]string{
		library:   "; packaged library\n\nnamespace = ZZMI\\Lib\n",
		duplicate: "namespace = zzmi\\lib\n",

		// A namespace line after other content is not a declaration, so the file is not a duplicate.
		late: "[Constants]\nnamespace = zzmi\\lib\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := Optimize(context.Background(), Options{
		Importer: "ZZMI", ImporterFolder: root, CachePath: filepath.Join(t.TempDir(), "cache.json"),
		Prefix: "DISABLED ", DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Changes) != 1 || report.Changes[0].Path != duplicate ||
		report.Changes[0].Reason != "duplicate packaged library namespace" {
		t.Fatalf("changes = %+v", report.Changes)
	}
}
