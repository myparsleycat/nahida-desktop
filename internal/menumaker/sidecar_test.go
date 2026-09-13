package menumaker

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSidecarTXTReapplyUsesRuntimeOwner(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "Master.txt")
	mustWrite(t, source, []byte(sidecarFixture))
	req := MenuMakerApplyRequest{
		SourcePath: source, SourceSHA256: sha256Hex([]byte(sidecarFixture)), OutputININame: "menu.ini",
		Slots: parseDocument(sidecarFixture).Slots, Settings: defaultSettings(), Encoding: "utf8", Newline: "crlf",
	}
	result, err := New().ApplyBundle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(result.SourceINIPath)
	if err != nil {
		t.Fatal(err)
	}
	req.SourcePath = result.SourceINIPath
	req.SourceSHA256 = sha256Hex(raw)
	req.Slots = parseDocument(string(raw)).Slots
	if _, err := New().ApplyBundle(context.Background(), req); err != nil {
		t.Fatalf("reapply generated INI: %v", err)
	}
	assertFile(t, source, []byte(sidecarFixture))
}

func TestSidecarExportsPinImplicitNamespace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "d3dx.ini"), []byte("[Include]"))
	source := filepath.Join(root, "Mods", "Original", "Master.ini")
	text := strings.Replace(sidecarFixture, "namespace = Example\\Master\n", "", 1)
	slots := parseDocument(text).Slots
	// Export outside the injector tree, as with a desktop save or ZIP download.
	exportDir := t.TempDir()
	destination := filepath.Join(exportDir, "menu.ini")
	_, err := New().SaveINI(context.Background(), MenuMakerSaveINIRequest{
		SourcePath: source, SourceText: text, DestinationPath: destination,
		Slots: slots, Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Master.ini", "menu.ini"} {
		raw, err := os.ReadFile(filepath.Join(exportDir, name))
		if err != nil {
			t.Fatal(err)
		}
		namespace, err := sourceNamespace(filepath.Join(exportDir, name), string(raw))
		if err != nil || namespace != `Mods\Original\Master.ini` {
			t.Fatalf("%s namespace = %q, err = %v", name, namespace, err)
		}
	}
	archive := filepath.Join(exportDir, "menu.zip")
	_, err = New().SaveZIP(context.Background(), MenuMakerSaveZIPRequest{
		SourcePath: source, SourceText: text, DestinationPath: archive,
		Slots: slots, Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
	})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	for _, file := range z.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		namespace, err := sourceNamespace("", string(raw))
		if err != nil || namespace != `Mods\Original\Master.ini` {
			t.Fatalf("ZIP %s namespace = %q, err = %v", file.Name, namespace, err)
		}
	}
}

func TestSidecarExportIncludesBothINIs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := filepath.Join(root, "Master.ini")
	mustWrite(t, source, []byte(sidecarFixture))
	slots := parseDocument(sidecarFixture).Slots
	archive := filepath.Join(root, "export.zip")
	_, err := New().SaveZIP(context.Background(), MenuMakerSaveZIPRequest{
		SourcePath: source, SourceText: sidecarFixture, DestinationPath: archive, Slots: slots,
		Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
	})
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	if len(z.File) != 2 || z.File[0].Name != "menu.ini" || z.File[1].Name != "Master.ini" {
		t.Fatal("export missing linked INIs")
	}
	destination := filepath.Join(root, "export", "menu.ini")
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		t.Fatal(err)
	}
	_, err = New().SaveINI(context.Background(), MenuMakerSaveINIRequest{
		SourcePath: source, SourceText: sidecarFixture, DestinationPath: destination, Slots: slots,
		Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(destination), "Master.ini")); err != nil {
		t.Fatal(err)
	}
	assertFile(t, source, []byte(sidecarFixture))
}

func TestSidecarRemovedKeysRemainEditableAndRestorable(t *testing.T) {
	t.Parallel()
	settings := defaultSettings()
	settings.RemoveOriginalKeys = true
	doc := parseDocument(sidecarFixture)
	first, err := generateSidecar("Master.ini", sidecarFixture, doc.Slots, settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.SourceINIText, "[KeySwap]") {
		t.Fatal("keyboard binding still enabled")
	}
	reparsed := parseDocument(first.SourceINIText)
	if len(reparsed.Slots) != len(doc.Slots) {
		t.Fatal("removed keys cannot be edited again")
	}
	second, err := generateSidecar("Master.ini", first.SourceINIText, reparsed.Slots, settings)
	if err != nil {
		t.Fatal(err)
	}
	if second.SourceINIText != first.SourceINIText || second.INIText != first.INIText {
		t.Fatal("removed keys unstable on reapply")
	}
	settings.RemoveOriginalKeys = false
	restored, err := generateSidecar("Master.ini", first.SourceINIText, reparsed.Slots, settings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(restored.SourceINIText, "[KeySwap]\n") {
		t.Fatal("keyboard binding was not restored")
	}
}

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

func TestSidecarSharesNamespaceAndPreservesManagerGate(t *testing.T) {
	t.Parallel()
	doc := parseDocument(sidecarFixture)
	generated, err := generateSidecar("Master.ini", sidecarFixture, doc.Slots, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated.INIText, "namespace = Example\\Master\n") ||
		strings.Contains(
			generated.INIText,
			"[TextureOverride",
		) || strings.Contains(generated.SourceINIText, "[KeyGuiMenu]") {
		t.Fatal("menu and source were not separated")
	}
	for _, section := range parseSections(generated.INIText) {
		if section.Name != nil && (generatedKeyRe.MatchString(*section.Name) || *section.Name == "Present") {
			if !strings.Contains(strings.Join(section.Lines, "\n"), "$managed_slot_id == $\\manager\\active_slot") {
				t.Fatalf("manager gate missing from %s", *section.Name)
			}
		}
	}
	if strings.Contains(generated.INIText, "post $active = 0") {
		t.Fatal("menu must not reset the original flag")
	}
	if !strings.Contains(generated.SourceINIText, "run = CommandListCycleKeySwap") {
		t.Fatal("keyboard and menu must share cycle state")
	}
	nextDoc := parseDocument(generated.SourceINIText)
	next, err := generateSidecar("Master.ini", generated.SourceINIText, nextDoc.Slots, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if next.INIText != generated.INIText || next.SourceINIText != generated.SourceINIText {
		t.Fatal("reapplication changed output")
	}
}

func TestSidecarInstrumentsPositionInsideOriginalGuard(t *testing.T) {
	t.Parallel()
	for _, section := range []string{"TextureOverrideBodyPosition", "TextureOverrideBodyBlend"} {
		text := "namespace = Plain\n[Constants]\nglobal persist $swap = 0\nglobal $nhd_menu_active = 7\n" +
			"[KeySwap]\nkey = 5\n$swap = 0,1\n[" + section + "]\nhash = abcdef01\nif $\\manager\\slot == 5\nvb0 = ResourceBodyPosition\nendif\n"
		doc := parseDocument(text)
		result, err := generateSidecar("Mod.ini", text, doc.Slots, defaultSettings())
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(
			result.SourceINIText,
			"if $\\manager\\slot == 5\n$nhd_menu_active_2 = 1"+visibilityMarker+"\nvb0 = ResourceBodyPosition",
		) {
			t.Fatal("visibility hook escaped the original render guard")
		}
		for _, expected := range []string{"global $nhd_menu_active_2 = 0", "post $nhd_menu_active_2 = 0", "condition = $nhd_menu_active_2 == 1"} {
			if !strings.Contains(result.INIText, expected) {
				t.Fatalf("missing %s", expected)
			}
		}
		nextDoc := parseDocument(result.SourceINIText)
		next, err := generateSidecar("Mod.ini", result.SourceINIText, nextDoc.Slots, defaultSettings())
		if err != nil {
			t.Fatal(err)
		}
		if next.INIText != result.INIText || next.SourceINIText != result.SourceINIText {
			t.Fatal("generated flag was duplicated on reapply")
		}
	}
}

func TestSidecarRejectsUnknownVisibilityAndNamespaceCollision(t *testing.T) {
	t.Parallel()
	text := "namespace = Unknown\n[Constants]\nglobal $active = 1\n[KeySwap]\nkey = 5\ncondition = $active\n$x = 0,1\n"
	_, err := generateSidecar("Mod.ini", text, parseDocument(text).Slots, defaultSettings())
	if !errors.Is(err, ErrNoVisibility) {
		t.Fatalf("expected missing visibility, got %v", err)
	}
	text = strings.Replace(sidecarFixture, "global $active", "global $gui_menu = 0\nglobal $active", 1)
	_, err = generateSidecar("Mod.ini", text, parseDocument(text).Slots, defaultSettings())
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("expected namespace collision, got %v", err)
	}
}

func TestSidecarResolvesImplicitNamespaceWithoutChangingSourceNamespace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "d3dx.ini"), []byte("[Include]"))
	path := filepath.Join(root, "Mods", "Character", "Mod", "Mod.ini")
	text := strings.Replace(sidecarFixture, "namespace = Example\\Master\n", "", 1)
	result, err := generateSidecar(path, text, parseDocument(text).Slots, defaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.INIText, "namespace = Mods\\Character\\Mod\\Mod.ini\n") ||
		strings.Contains(result.SourceINIText, "namespace =") {
		t.Fatal("original implicit namespace changed")
	}
}

func TestSidecarApplyRejectsUnrelatedMenu(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "Master.ini")
	mustWrite(t, path, []byte(sidecarFixture))
	mustWrite(t, filepath.Join(root, "menu.ini"), []byte("user menu"))
	_, err := New().ApplyBundle(context.Background(), MenuMakerApplyRequest{
		SourcePath: path, SourceSHA256: sha256Hex([]byte(sidecarFixture)), OutputININame: "menu.ini",
		Slots: parseDocument(sidecarFixture).Slots, Settings: defaultSettings(), Encoding: "utf8", Newline: "lf",
	})
	if err == nil {
		t.Fatal("unrelated menu was overwritten")
	}
	assertFile(t, path, []byte(sidecarFixture))
	assertFile(t, filepath.Join(root, "menu.ini"), []byte("user menu"))
}

// Opt-in read-only compatibility survey. Never applies generated files to mods.
func TestSidecarLocalModSurvey(t *testing.T) {
	roots := os.Getenv("MENU_MAKER_MOD_ROOTS")
	if roots == "" {
		t.Skip("set MENU_MAKER_MOD_ROOTS to survey local mods")
	}
	for _, root := range filepath.SplitList(roots) {
		checked, supported := 0, 0
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if strings.HasPrefix(strings.ToLower(entry.Name()), "disabled") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if checked >= 60 {
				return fs.SkipAll
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".ini") {
				return nil
			}
			raw, err := readLimited(path, maxSourceBytes)
			if err != nil {
				return err
			}
			text, _, err := decodeText(raw)
			if err != nil {
				return err
			}
			doc := parseDocument(text)
			if len(doc.Slots) == 0 {
				return nil
			}
			checked++
			result, err := generateSidecar(path, text, doc.Slots, defaultSettings())
			if err != nil {
				t.Logf("unsupported %s: %v", path, err)
				return nil
			}
			supported++
			nextDoc := parseDocument(result.SourceINIText)
			next, err := generateSidecar(path, result.SourceINIText, nextDoc.Slots, defaultSettings())
			if err != nil || next.INIText != result.INIText || next.SourceINIText != result.SourceINIText {
				t.Errorf("unstable regeneration %s: %v", path, err)
				for _, pair := range [][2]string{{result.INIText, next.INIText}, {result.SourceINIText, next.SourceINIText}} {
					for i := range min(len(pair[0]), len(pair[1])) {
						if pair[0][i] != pair[1][i] {
							t.Logf(
								"first difference: %q -> %q",
								pair[0][max(0, i-80):min(len(pair[0]), i+150)],
								pair[1][max(0, i-80):min(len(pair[1]), i+150)],
							)
							break
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %d/%d supported", root, supported, checked)
	}
}
