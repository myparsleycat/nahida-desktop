package xxmi

import (
	"context"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testPEImage builds the smallest image debug/pe accepts: a DOS stub, the PE signature, and a file header
// without sections. payload makes images with the same headers hash differently.
func testPEImage(machine, characteristics uint16, payload string) []byte {
	const headerOffset = 96
	image := make([]byte, headerOffset+4+20)
	copy(image, "MZ")
	binary.LittleEndian.PutUint32(image[0x3c:], headerOffset)
	copy(image[headerOffset:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(image[headerOffset+4:], machine)
	binary.LittleEndian.PutUint16(image[headerOffset+22:], characteristics)
	return append(image, payload...)
}

func testCustomDLLImage(payload string) []byte {
	return testPEImage(pe.IMAGE_FILE_MACHINE_AMD64, pe.IMAGE_FILE_DLL|pe.IMAGE_FILE_EXECUTABLE_IMAGE, payload)
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestImportCustomDLLStoresVerifiedCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	image := testCustomDLLImage("first")
	source := filepath.Join(t.TempDir(), "My Build.DLL")
	writeTestFile(t, source, image)

	dll, err := importCustomDLL(ctx, root, source)
	if err != nil {
		t.Fatal(err)
	}
	if dll.ID != hashBytes(image)[:12] || dll.SHA256 != hashBytes(image) || dll.Name != "My Build.DLL" ||
		dll.Size != int64(len(image)) {
		t.Fatalf("imported entry = %+v", dll)
	}
	stored, data, err := readCustomDLL(root, dll.ID)
	if err != nil || stored != dll || string(data) != string(image) {
		t.Fatalf("stored entry = %+v, %d bytes, err = %v", stored, len(data), err)
	}

	// The same bytes under another name reuse the entry instead of writing a second copy.
	renamed := filepath.Join(t.TempDir(), "d3d11.dll")
	writeTestFile(t, renamed, image)
	again, err := importCustomDLL(ctx, root, renamed)
	if err != nil || again != dll {
		t.Fatalf("repeat import = %+v, err = %v, want %+v", again, err, dll)
	}
	listed, err := listCustomDLLs(root)
	if err != nil || len(listed) != 1 || listed[0] != dll {
		t.Fatalf("listed entries = %+v, err = %v", listed, err)
	}

	cached := filepath.Join(root, "packages", customDLLPackage, dll.ID, customDLLName)
	writeTestFile(t, cached, []byte("damaged"))
	if _, _, err := readCustomDLL(root, dll.ID); err == nil {
		t.Fatal("damaged cached DLL passed the hash check")
	}
	if _, err := importCustomDLL(ctx, root, source); err != nil {
		t.Fatal(err)
	}
	if _, data, err := readCustomDLL(root, dll.ID); err != nil || string(data) != string(image) {
		t.Fatalf("repaired entry has %d bytes, err = %v", len(data), err)
	}
}

func TestImportCustomDLLRejectsUnloadableFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file string
		data       []byte
	}{
		{name: "wrong extension", file: "d3d11.txt", data: testCustomDLLImage("")},
		{name: "not an image", file: "d3d11.dll", data: []byte("plain text")},
		{
			name: "32-bit", file: "d3d11.dll",
			data: testPEImage(pe.IMAGE_FILE_MACHINE_I386, pe.IMAGE_FILE_DLL, ""),
		},
		{
			name: "executable", file: "d3d11.dll",
			data: testPEImage(pe.IMAGE_FILE_MACHINE_AMD64, pe.IMAGE_FILE_EXECUTABLE_IMAGE, ""),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := filepath.Join(t.TempDir(), tc.file)
			writeTestFile(t, source, tc.data)
			_, err := importCustomDLL(context.Background(), root, source)
			if err == nil || !strings.HasPrefix(err.Error(), "XXMI_CUSTOM_DLL_INVALID") {
				t.Fatalf("import error = %v", err)
			}
			if listed, err := listCustomDLLs(root); err != nil || len(listed) != 0 {
				t.Fatalf("rejected file left entries %+v, err = %v", listed, err)
			}
		})
	}

	t.Run("too large", func(t *testing.T) {
		t.Parallel()
		source := filepath.Join(t.TempDir(), "d3d11.dll")
		writeTestFile(t, source, testCustomDLLImage(""))
		if err := os.Truncate(source, customDLLSizeLimit+1); err != nil {
			t.Fatal(err)
		}
		_, err := importCustomDLL(context.Background(), t.TempDir(), source)
		if err == nil || !strings.HasPrefix(err.Error(), "XXMI_CUSTOM_DLL_INVALID") {
			t.Fatalf("import error = %v", err)
		}
	})
}

func TestDeployRuntimeAppliesCustomDLLOncePerSelection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := t.TempDir()
	importer := filepath.Join(base, "GIMI")
	libs := filepath.Join(base, "libs")
	for path, content := range map[string]string{
		filepath.Join(importer, "d3dx.ini"): "[Loader]", filepath.Join(importer, "d3d11.dll"): "third party",
		filepath.Join(libs, "d3d11.dll"): "xxmi", filepath.Join(libs, "d3dcompiler_47.dll"): "compiler",
	} {
		writeTestFile(t, path, []byte(content))
	}
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeXXMI, Migoto: MigotoOptions{UnsafeMode: true}}
	deploy := func(custom *customRuntimeDLL) runtimeManifest {
		t.Helper()
		if _, err := deployCustomRuntimeFiles(
			ctx, "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess, custom,
		); err != nil {
			t.Fatal(err)
		}
		if err := validateDeployedRuntime(importer, RuntimeXXMI); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(importer, runtimeManifestName))
		if err != nil {
			t.Fatal(err)
		}
		var manifest runtimeManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	backups := func() []string {
		t.Helper()
		found, err := filepath.Glob(filepath.Join(base, "backups", "GIMI *", "d3d11.dll"))
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	selection := func(content string) *customRuntimeDLL {
		return &customRuntimeDLL{id: hashBytes([]byte(content))[:12], data: []byte(content)}
	}
	first, second := selection("custom one"), selection("custom two")

	// A new selection replaces the file already in the folder and keeps a backup of it.
	manifest := deploy(first)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "custom one")
	assertFileContent(t, filepath.Join(importer, "d3dcompiler_47.dll"), "compiler")
	if manifest.Custom != first.id || manifest.UserManaged["d3d11.dll"] != hashBytes(first.data) ||
		manifest.Files["d3d11.dll"] != "" || !usesCustomDLL(importer) {
		t.Fatalf("manifest after selection = %+v", manifest)
	}
	if found := backups(); len(found) != 1 {
		t.Fatalf("backups after selection = %v", found)
	}
	assertFileContent(t, backups()[0], "third party")

	// Changes made on top of the applied selection, such as a fixer build, survive later deployments.
	writeTestFile(t, filepath.Join(importer, "d3d11.dll"), []byte("fixer build"))
	manifest = deploy(first)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer build")
	if manifest.Custom != first.id || manifest.UserManaged["d3d11.dll"] != hashBytes([]byte("fixer build")) {
		t.Fatalf("manifest after user change = %+v", manifest)
	}

	// Another selection replaces the changed file after backing it up.
	manifest = deploy(second)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "custom two")
	if manifest.Custom != second.id || len(backups()) != 2 {
		t.Fatalf("manifest after reselection = %+v, backups = %v", manifest, backups())
	}

	// Clearing the selection restores the signed DLL and backs up the previous custom DLL.
	manifest = deploy(nil)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "xxmi")
	if manifest.Custom != "" || manifest.Files["d3d11.dll"] != hashBytes([]byte("xxmi")) ||
		usesCustomDLL(importer) || len(backups()) != 3 {
		t.Fatalf("manifest after clearing = %+v, backups = %v", manifest, backups())
	}

	// A file changed after the selection was applied is the user's own and stays when the selection is cleared.
	deploy(first)
	writeTestFile(t, filepath.Join(importer, "d3d11.dll"), []byte("fixer build"))
	deploy(first)
	manifest = deploy(nil)
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "fixer build")
	if manifest.Custom != "" || manifest.UserManaged["d3d11.dll"] == "" {
		t.Fatalf("manifest after clearing a changed file = %+v", manifest)
	}
}

func TestDeployRuntimeReplacesCustomDLLWithoutUnsafeMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	base := t.TempDir()
	importer := filepath.Join(base, "GIMI")
	libs := filepath.Join(base, "libs")
	for path, content := range map[string]string{
		filepath.Join(importer, "d3dx.ini"): "[Loader]",
		filepath.Join(libs, "d3d11.dll"):    "xxmi", filepath.Join(libs, "d3dcompiler_47.dll"): "compiler",
	} {
		writeTestFile(t, path, []byte(content))
	}
	cfg := ImporterConfig{ImporterFolder: importer, Mode: RuntimeXXMI, Migoto: MigotoOptions{UnsafeMode: true}}
	custom := &customRuntimeDLL{id: hashBytes([]byte("custom"))[:12], data: []byte("custom")}
	if _, err := deployCustomRuntimeFiles(
		ctx, "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess, custom,
	); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "custom")

	// Without unsafe mode the service resolves no custom DLL, so the deployment receives none.
	cfg.Migoto.UnsafeMode = false
	if _, err := deployRuntimeFiles(ctx, "GIMI", cfg, libs, "xxmi-libs@1", base, false, noGameProcess); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, filepath.Join(importer, "d3d11.dll"), "xxmi")
	if err := validateXXMIRuntimeFiles(importer, libs, false); err != nil {
		t.Fatal(err)
	}
	backups, err := os.ReadDir(filepath.Join(base, "backups"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups after disabling unsafe mode = %v, err = %v", backups, err)
	}
	assertFileContent(t, filepath.Join(base, "backups", backups[0].Name(), customDLLName), "custom")
}

func TestCustomDLLSelectionAndPruning(t *testing.T) {
	// The package cache lives under the user profile, so the service is pointed at a disposable one.
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()
	service := New()
	client := newXXMITestClient(t)
	service.UseClient(client)
	service.findProcess = noGameProcess
	useBuiltinLauncher(t, service)
	folder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := service.EnableImporter(ctx, "GIMI", folder); err != nil {
		t.Fatal(err)
	}
	importDLL := func(payload string) CustomDLL {
		t.Helper()
		source := filepath.Join(t.TempDir(), "d3d11.dll")
		writeTestFile(t, source, testCustomDLLImage(payload))
		dll, err := service.ImportCustomDLL(ctx, source)
		if err != nil {
			t.Fatal(err)
		}
		return dll
	}
	own := importDLL("own")

	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	cfg.CustomDLL = "000000000000"
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err == nil ||
		!strings.HasPrefix(err.Error(), "XXMI_CUSTOM_DLL_MISSING") {
		t.Fatalf("unknown custom DLL error = %v", err)
	}
	cfg.CustomDLL = "not-an-id"
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err == nil {
		t.Fatal("malformed custom DLL ID was saved")
	}
	cfg.CustomDLL = own.ID
	cfg.XXMIVersion = VersionPin{Follow: "latest"}
	cfg.Migoto.UnsafeMode = true
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	// Imports stay protected while this session may still have pending selections or unsaved drafts.
	shared := importDLL("shared")
	if err := service.SetSharedCustomDLL(ctx, shared.ID); err != nil {
		t.Fatal(err)
	}

	resolve := func(cfg ImporterConfig) string {
		t.Helper()
		id, err := service.customDLLID(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if id := resolve(cfg); id != own.ID {
		t.Fatalf("own selection resolved %q, want %q", id, own.ID)
	}
	following := cfg
	following.XXMIVersion = VersionPin{Follow: followShared}
	if id := resolve(following); id != shared.ID {
		t.Fatalf("shared selection resolved %q, want %q", id, shared.ID)
	}
	safe := cfg
	safe.Migoto.UnsafeMode = false
	legacy := cfg
	legacy.Mode = RuntimeLegacy
	if resolve(safe) != "" || resolve(legacy) != "" {
		t.Fatal("custom DLL resolved without unsafe mode or in legacy mode")
	}

	overview, err := service.GetOverview(ctx)
	if err != nil || overview.SharedCustomDLL != shared.ID || len(overview.CustomDLLs) != 2 {
		t.Fatalf("overview = %+v, err = %v", overview, err)
	}

	// Dropping the last reference preserves this session's imports.
	if err := service.SetSharedCustomDLL(ctx, ""); err != nil {
		t.Fatal(err)
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	listed, err := listCustomDLLs(root)
	if err != nil || len(listed) != 2 {
		t.Fatalf("entries after clearing the shared selection = %+v, err = %v", listed, err)
	}

	// A fresh service represents the next app session: unused imports can now be pruned.
	service = New()
	service.UseClient(client)
	if err := service.SetSharedCustomDLL(ctx, ""); err != nil {
		t.Fatal(err)
	}
	listed, err = listCustomDLLs(root)
	if err != nil || len(listed) != 1 || listed[0].ID != own.ID {
		t.Fatalf("entries after clearing the shared selection = %+v, err = %v", listed, err)
	}
	if err := service.SetSharedCustomDLL(ctx, shared.ID); err == nil ||
		!strings.HasPrefix(err.Error(), "XXMI_CUSTOM_DLL_MISSING") {
		t.Fatalf("pruned custom DLL error = %v", err)
	}
}

func TestCustomDLLOverlappingSelectionsPreserveImports(t *testing.T) {
	for _, selection := range []string{"shared", "importer"} {
		t.Run(selection, func(t *testing.T) {
			t.Setenv("USERPROFILE", t.TempDir())
			ctx := context.Background()
			service := New()
			service.UseClient(newXXMITestClient(t))
			cfg, err := service.GetImporterConfig(ctx, "GIMI")
			if err != nil {
				t.Fatal(err)
			}
			selectDLL := func(id string) error {
				if selection == "shared" {
					return service.SetSharedCustomDLL(ctx, id)
				}
				next := cfg
				next.CustomDLL = id
				return service.SaveImporterConfig(ctx, "GIMI", next)
			}
			firstPath := filepath.Join(t.TempDir(), "first.dll")
			secondPath := filepath.Join(t.TempDir(), "second.dll")
			writeTestFile(t, firstPath, testCustomDLLImage("first drop"))
			writeTestFile(t, secondPath, testCustomDLLImage("second drop"))
			first, err := service.ImportCustomDLL(ctx, firstPath)
			if err != nil {
				t.Fatal(err)
			}

			// Both imports finish before the first selection's cleanup; the second request then saves its ID.
			secondImported := make(chan error, 1)
			firstSelected := make(chan error, 1)
			secondSelected := make(chan error, 1)
			go func() {
				second, err := service.ImportCustomDLL(ctx, secondPath)
				secondImported <- err
				if firstErr := <-firstSelected; firstErr != nil {
					secondSelected <- firstErr
					return
				}
				if err == nil {
					err = selectDLL(second.ID)
				}
				secondSelected <- err
			}()
			importErr := <-secondImported
			selectErr := selectDLL(first.ID)
			firstSelected <- selectErr
			secondErr := <-secondSelected
			if importErr != nil || selectErr != nil || secondErr != nil {
				t.Fatalf(
					"overlapping selection errors: import=%v, first=%v, second=%v",
					importErr,
					selectErr,
					secondErr,
				)
			}
			root, err := xxmiCacheRoot()
			if err != nil {
				t.Fatal(err)
			}
			for _, data := range [][]byte{testCustomDLLImage("first drop"), testCustomDLLImage("second drop")} {
				if _, _, err := readCustomDLL(root, hashBytes(data)[:12]); err != nil {
					t.Fatalf("imported DLL was removed: %v", err)
				}
			}
		})
	}
}

func TestCustomDLLSelectionChangeBacksUpPrunedDLL(t *testing.T) {
	for _, selection := range []string{"replace", "clear"} {
		t.Run(selection, func(t *testing.T) {
			t.Setenv("USERPROFILE", t.TempDir())
			ctx := context.Background()
			client := newXXMITestClient(t)
			service := New()
			service.UseClient(client)
			folder := filepath.Join(t.TempDir(), "GIMI")
			writeTestFile(t, filepath.Join(folder, "d3dx.ini"), []byte("[Loader]"))
			cfg, err := service.GetImporterConfig(ctx, "GIMI")
			if err != nil {
				t.Fatal(err)
			}
			cfg.ImporterFolder = folder
			cfg.XXMIVersion = VersionPin{Follow: "latest"}
			cfg.Migoto.UnsafeMode = true
			firstData := testCustomDLLImage("previous session")
			firstPath := filepath.Join(t.TempDir(), "first.dll")
			writeTestFile(t, firstPath, firstData)
			first, err := service.ImportCustomDLL(ctx, firstPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(firstPath); err != nil {
				t.Fatal(err)
			}
			cfg.CustomDLL = first.ID
			if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
				t.Fatal(err)
			}
			root, err := xxmiCacheRoot()
			if err != nil {
				t.Fatal(err)
			}
			libs := filepath.Join(t.TempDir(), "libs")
			writeTestFile(t, filepath.Join(libs, customDLLName), []byte("official"))
			writeTestFile(t, filepath.Join(libs, "d3dcompiler_47.dll"), []byte("compiler"))
			if _, err := deployCustomRuntimeFiles(
				ctx, "GIMI", cfg, libs, "xxmi-libs@1", root, false, noGameProcess,
				&customRuntimeDLL{id: first.ID, data: firstData},
			); err != nil {
				t.Fatal(err)
			}

			// In a later session, changing the selection prunes the previous DLL before deployment.
			service = New()
			service.UseClient(client)
			cfg.CustomDLL = ""
			var custom *customRuntimeDLL
			if selection == "replace" {
				data := testCustomDLLImage("replacement")
				path := filepath.Join(t.TempDir(), "second.dll")
				writeTestFile(t, path, data)
				second, err := service.ImportCustomDLL(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				cfg.CustomDLL = second.ID
				custom = &customRuntimeDLL{id: second.ID, data: data}
			}
			if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
				t.Fatal(err)
			}
			if _, _, err := readCustomDLL(root, first.ID); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("previous DLL cache was not pruned: %v", err)
			}
			if _, err := deployCustomRuntimeFiles(
				ctx, "GIMI", cfg, libs, "xxmi-libs@1", root, false, noGameProcess, custom,
			); err != nil {
				t.Fatal(err)
			}
			wanted := "official"
			if custom != nil {
				wanted = string(custom.data)
			}
			assertFileContent(t, filepath.Join(folder, customDLLName), wanted)
			backups, err := os.ReadDir(filepath.Join(root, "backups"))
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups after selection change = %v, err = %v", backups, err)
			}
			assertFileContent(t, filepath.Join(root, "backups", backups[0].Name(), customDLLName), string(firstData))
		})
	}
}
