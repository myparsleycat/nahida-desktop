package menumaker

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func defaultSettings() MenuMakerSettings {
	return MenuMakerSettings{
		Title:                "",
		MenuKey:              "alt",
		ClickModifier:        "alt",
		Columns:              3,
		Gap:                  14,
		BaseWidth:            1920,
		BaseHeight:           1080,
		PanelScale:           1,
		SlotAlignment:        "center",
		FallbackType:         "cycle",
		RemoveOriginalKeys:   false,
		HideUploadLabel:      true,
		UseOriginalININame:   true,
		ResetActiveOnPresent: false,
		ShowKeyHint:          true,
	}
}

func TestGenerateUsesCycleFallback(t *testing.T) {
	t.Parallel()
	document := parseDocument("[Constants]\nglobal $swap = 0\n[KeySwap]\nkey = 5\n$swap = 0, 1")
	output := generateFrom(document, document.Slots, defaultSettings())
	if defaultSettings().FallbackType != "cycle" {
		t.Fatal("expected cycle fallback")
	}
	if !strings.Contains(output, "global $ks_step_KeySwap = 0") || !strings.Contains(output, "[CommandListCycleKeySwap]") {
		t.Fatalf("missing cycle output:\n%s", output)
	}
	if strings.Contains(output, "[CommandListActivateKeySwap]") {
		t.Fatalf("unexpected activate output:\n%s", output)
	}
}

func TestGenerateRightClickRunsBackWithoutOriginalBackKey(t *testing.T) {
	t.Parallel()
	document := parseDocument("[Constants]\nglobal $swap = 0\n[KeySwap]\nkey = 5\n$swap = 0, 1, 2")
	output := generateFrom(document, document.Slots, defaultSettings())
	for _, snippet := range []string{
		"[KeyGuiRightClick]\ncondition = $gui_menu == 1 && $gui_hover == 1\nkey = no_ctrl no_shift alt VK_RBUTTON\nrun = CommandListGuiRightClick",
		"[CommandListGuiClick]\n$gui_clicked = $gui_hovered\nif $gui_clicked == 1\n  ; swap\n  run = CommandListCycleKeySwap\nendif",
		"[CommandListGuiRightClick]\n$gui_right_clicked = $gui_hovered\nif $gui_right_clicked == 1\n  ; swap\n  run = CommandListCycleKeySwapBack\nendif",
		"[CommandListCycleKeySwapBack]\n$ks_step_KeySwap = $ks_step_KeySwap - 1",
		"if $ks_step_KeySwap < 0\n  $ks_step_KeySwap = 2",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("missing %q in:\n%s", snippet, output)
		}
	}
	if strings.Contains(output, "[KeySwapBack]") {
		t.Fatalf("gui reverse should not invent a keyboard back key:\n%s", output)
	}

	toggled := parseDocument("[Constants]\nglobal $black = 0\n[KeyBlack]\nkey = 1\ntype = toggle\n$black = 1")
	toggleOutput := generateFrom(toggled, toggled.Slots, defaultSettings())
	if !strings.Contains(toggleOutput, "run = CommandListCycleKeyBlackBack") ||
		!strings.Contains(toggleOutput, "[CommandListCycleKeyBlackBack]") {
		t.Fatalf("toggle right-click should reverse:\n%s", toggleOutput)
	}

	pulsed := parseDocument("[Constants]\nglobal $pulse = 0\n[KeyPulse]\nkey = 1\ntype = activate\n$pulse = 1")
	pulseOutput := generateFrom(pulsed, pulsed.Slots, defaultSettings())
	rightClickStart := strings.Index(pulseOutput, "[CommandListGuiRightClick]")
	if rightClickStart < 0 {
		t.Fatalf("missing right-click command list:\n%s", pulseOutput)
	}
	rightClick := pulseOutput[rightClickStart:]
	if end := strings.Index(rightClick[1:], "\n["); end >= 0 {
		rightClick = rightClick[:end+1]
	}
	if strings.Contains(rightClick, "run = ") {
		t.Fatalf("activate right-click should not run a command:\n%s", rightClick)
	}
	if strings.Contains(pulseOutput, "CommandListCycleKeyPulseBack") {
		t.Fatalf("activate should not emit a reverse command:\n%s", pulseOutput)
	}
}

func TestGeneratePreservesActivateKeys(t *testing.T) {
	t.Parallel()
	document := parseDocument("[Constants]\nglobal $pulse = 0\n[KeyPulse]\nkey = 1\ntype = activate\n$pulse = 1")
	output := generateFrom(document, document.Slots, defaultSettings())
	if !strings.Contains(output, "[KeyPulse]\nkey = 1\ntype = activate\n$pulse = 1") {
		t.Fatalf("missing original activate key:\n%s", output)
	}
	if !strings.Contains(output, "[CommandListActivateKeyPulse]") {
		t.Fatalf("missing activate command:\n%s", output)
	}
}

func TestGenerateKeepsUserSectionsWithGeneratedPrefixes(t *testing.T) {
	t.Parallel()
	document := parseDocument("[KeySwap]\nkey = 1\ntype = cycle\n$x = 0,1\n[CommandListGuiCustom]\n$x = 9\n[ResourceGuiCustom]\nfilename = custom.png\n[CustomShaderGuiDrawCustom]\ndraw = 3,0")
	output := generateFrom(document, document.Slots, defaultSettings())
	for _, snippet := range []string{
		"[CommandListGuiCustom]\n$x = 9",
		"[ResourceGuiCustom]\nfilename = custom.png",
		"[CustomShaderGuiDrawCustom]\ndraw = 3,0",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("missing %q in:\n%s", snippet, output)
		}
	}
}

func TestGenerateIndependentSameKeyHandlers(t *testing.T) {
	t.Parallel()
	document := parseDocument(strictFixture)
	output := generateFrom(document, document.Slots, defaultSettings())
	for _, snippet := range []string{
		"if $active == 1\n    run = CommandListCycleKeySwap\n  endif",
		"if 1 == $black_active\n    run = CommandListCycleKeySwap_2\n  endif",
		"if $active == 1\n    run = CommandListCycleKeySwapBack\n  endif",
		"if 1 == $black_active\n    run = CommandListCycleKeySwap_2Back\n  endif",
		"[CommandListCycleKeySwap]",
		"[CommandListCycleKeySwapBack]",
		"[CommandListCycleKeySwap_2Back]",
		"global $ks_step_KeySwap = 0",
		"global $ks_step_KeySwap_2 = 0",
		"$swap = 1\n  run = CommandListAfterSwap\n  unknown action",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("missing %q in:\n%s", snippet, output)
		}
	}
	if strings.Contains(output, "elif 1 == $black_active") {
		t.Fatalf("unexpected elif merge:\n%s", output)
	}
}

func TestGeneratePresentIdempotent(t *testing.T) {
	t.Parallel()
	document := parseDocument(strictFixture)
	first := generateFrom(document, document.Slots, defaultSettings())
	if !strings.Contains(first, "; user logic\npost $user = 1") {
		t.Fatalf("missing user present logic:\n%s", first)
	}
	reloaded := parseDocument(first)
	if len(reloaded.Slots) != len(document.Slots) {
		t.Fatalf("slot count changed: %d -> %d", len(document.Slots), len(reloaded.Slots))
	}
	second := generateFrom(reloaded, reloaded.Slots, defaultSettings())
	if strings.Count(second, "[KeyGuiMenu]") != 1 {
		t.Fatalf("duplicate KeyGuiMenu:\n%s", second)
	}
	if strings.Count(second, "run = CommandListGuiMenu") != 1 {
		t.Fatalf("duplicate CommandListGuiMenu runs:\n%s", second)
	}
}

func TestGenerateKeepsWrapAndRepeatedSteps(t *testing.T) {
	t.Parallel()
	document := parseDocument(strictFixture)
	output := generateFrom(document, document.Slots, defaultSettings())
	if !strings.Contains(output, "if $ks_step_KeySwap >= 3\n  $ks_step_KeySwap = 2") {
		t.Fatalf("missing wrap-false upper bound:\n%s", output)
	}
	if !strings.Contains(output, "if $ks_step_KeySwap < 0\n  $ks_step_KeySwap = 0") {
		t.Fatalf("missing wrap-false lower bound:\n%s", output)
	}
	if strings.Count(output, "$swap = 1") < 2 {
		t.Fatalf("expected repeated swap steps:\n%s", output)
	}
}

func TestGenerateMergeModesAndKeyDeletion(t *testing.T) {
	t.Parallel()
	document := parseDocument(strictFixture)
	ids := make([]string, 0, len(document.Slots))
	for _, slot := range document.Slots {
		ids = append(ids, slot.ID)
	}
	merged := mergeSlots(document.Slots, ids, "allKeys")
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged slot, got %d", len(merged))
	}
	allKeys := generateFrom(document, merged, defaultSettings())
	if !strings.Contains(allKeys, "key = no_modifiers 5\nkey = no_modifier 6") {
		t.Fatalf("missing allKeys keys:\n%s", allKeys)
	}
	guiOnly := generateFrom(document, mergeSlots(document.Slots, ids, "guiOnly"), defaultSettings())
	if !strings.Contains(guiOnly, "$swap = 0, 1, 1") {
		t.Fatalf("missing guiOnly original assignment:\n%s", guiOnly)
	}
	deletedSettings := defaultSettings()
	deletedSettings.RemoveOriginalKeys = true
	deleted := generateFrom(document, document.Slots, deletedSettings)
	if strings.Contains(deleted, "[KeySwap]") {
		t.Fatalf("original keys were not removed:\n%s", deleted)
	}
	if !strings.Contains(deleted, "[CommandListCycleKeySwap]") {
		t.Fatalf("missing generated cycle after key deletion:\n%s", deleted)
	}
}

func TestCalculateGeometryDefaultsZeroColumnsToThree(t *testing.T) {
	t.Parallel()
	document := parseDocument("[KeyA]\nkey = 1\n$x = 0,1\n[KeyB]\nkey = 2\n$y = 0,1\n[KeyC]\nkey = 3\n$z = 0,1\n[KeyD]\nkey = 4\n$w = 0,1")
	zero := defaultSettings()
	zero.Columns = 0
	got := calculateGeometry(document.Slots, zero)
	want := calculateGeometry(document.Slots, defaultSettings())
	if got.PanelWidth != want.PanelWidth || got.PanelHeight != want.PanelHeight {
		t.Fatalf("zero columns geometry %+v != default %+v", got, want)
	}
	one := defaultSettings()
	one.Columns = 1
	narrow := calculateGeometry(document.Slots, one)
	if got.PanelWidth <= narrow.PanelWidth || got.PanelHeight >= narrow.PanelHeight {
		t.Fatalf("zero-column default should stay 3-wide, got %+v vs 1-col %+v", got, narrow)
	}
}

func TestGenerateOmitsTitleWhenEmpty(t *testing.T) {
	t.Parallel()
	document := parseDocument("[KeyA]\nkey = 1\n$x = 0,1")
	geometry := calculateGeometry(document.Slots, defaultSettings())
	titledSettings := defaultSettings()
	titledSettings.Title = "GUI Menu"
	titledGeometry := calculateGeometry(document.Slots, titledSettings)
	if geometry.TitleHeight != 0 {
		t.Fatalf("expected no title height, got %d", geometry.TitleHeight)
	}
	if geometry.PanelHeight >= titledGeometry.PanelHeight {
		t.Fatalf("untitled panel should be shorter: %d >= %d", geometry.PanelHeight, titledGeometry.PanelHeight)
	}
	if geometry.Slots[0].Y != geometry.Padding {
		t.Fatalf("untitled slot y=%d padding=%d", geometry.Slots[0].Y, geometry.Padding)
	}
	output := generateFrom(document, document.Slots, defaultSettings())
	if strings.Contains(output, "ResourceGuiTitle") || strings.Contains(output, "res_gui\\title.png") {
		t.Fatalf("unexpected title resource:\n%s", output)
	}
	if !strings.Contains(generateFrom(document, document.Slots, titledSettings), "filename = res_gui\\title.png") {
		t.Fatal("missing titled resource")
	}
}

func TestGenerateScalesSlotDrawSize(t *testing.T) {
	t.Parallel()
	document := parseDocument("[KeyA]\nkey = 1\n$x = 0,1")
	scaled := defaultSettings()
	scaled.PanelScale = 2
	geometry := calculateGeometry(document.Slots, scaled)
	if geometry.SlotSize != baseSlotSize*2 {
		t.Fatalf("scaled slot size: %d", geometry.SlotSize)
	}
	if calculateGeometry(document.Slots, defaultSettings()).SlotSize != baseSlotSize {
		t.Fatal("default slot size mismatch")
	}
	output := generateFrom(document, document.Slots, scaled)
	if !strings.Contains(output, "x87 = "+strconv.Itoa(geometry.SlotSize)+" / $gui_ww") ||
		!strings.Contains(output, "y87 = "+strconv.Itoa(geometry.SlotSize)+" / $gui_wh") {
		t.Fatalf("missing scaled draw size:\n%s", output)
	}
}

func TestGenerateAlignsAssetsAfterSkipAndReorder(t *testing.T) {
	t.Parallel()
	document := parseDocument(strictFixture)
	slots := []MenuMakerSlot{{Skip: true}, document.Slots[0]}
	slots[0] = document.Slots[1]
	slots[0].Skip = true
	slots[1] = document.Slots[0]
	geometry := calculateGeometry(slots, defaultSettings())
	indexes := make([]int, 0, len(geometry.Slots))
	for _, position := range geometry.Slots {
		indexes = append(indexes, position.AssetIndex)
	}
	if !slices.Equal(indexes, []int{1}) {
		t.Fatalf("unexpected asset indexes: %v", indexes)
	}
	output := generateFrom(document, slots, defaultSettings())
	if !strings.Contains(output, "filename = res_gui\\slot_01.png") {
		t.Fatalf("missing slot 01:\n%s", output)
	}
	if strings.Contains(output, "filename = res_gui\\slot_02.png") {
		t.Fatalf("unexpected slot 02:\n%s", output)
	}
}

func TestGenerateSlotTexturesForToggleCycleActivate(t *testing.T) {
	t.Parallel()
	toggled := parseDocument("[Constants]\nglobal $black = 0\n[KeyBlack]\nkey = 1\ntype = toggle\n$black = 1")
	constants := map[string]string{"$black": "0"}
	states := slotValueStates(toggled.Slots[0], defaultSettings(), constants)
	got := make([][2]string, 0, len(states))
	for _, state := range states {
		got = append(got, [2]string{state.Value, state.FileSuffix})
	}
	if !slices.Equal(got, [][2]string{{"0", ""}, {"1", "_s2"}}) {
		t.Fatalf("toggle states: %v", got)
	}
	if cond := slotActiveCondition(toggled.Slots[0], defaultSettings(), constants); cond != "$black == 1" {
		t.Fatalf("toggle active condition: %q", cond)
	}
	toggleOutput := generateFrom(toggled, toggled.Slots, defaultSettings())
	for _, snippet := range []string{
		"if $black == 1",
		"ps-t100 = ResourceGuiSlot1S2",
		"ps-t100 = ResourceGuiSlotHover1S2",
		"filename = res_gui\\slot_01_s2.png",
		"filename = res_gui\\slot_hover_01_s2.png",
	} {
		if !strings.Contains(toggleOutput, snippet) {
			t.Fatalf("missing %q in:\n%s", snippet, toggleOutput)
		}
	}
	if strings.Contains(toggleOutput, "ResourceGuiSlotOn") {
		t.Fatalf("unexpected On resource:\n%s", toggleOutput)
	}

	cycled := parseDocument("[Constants]\nglobal $swap = 0\n[KeySwap]\nkey = 5\ntype = cycle\n$swap = 0, 1, 2")
	values := []string{}
	for _, state := range slotValueStates(cycled.Slots[0], defaultSettings(), map[string]string{"$swap": "0"}) {
		values = append(values, state.Value)
	}
	if !slices.Equal(values, []string{"0", "1", "2"}) {
		t.Fatalf("cycle values: %v", values)
	}
	cycleOutput := generateFrom(cycled, cycled.Slots, defaultSettings())
	for _, snippet := range []string{
		"if $swap == 1",
		"elif $swap == 2",
		"ps-t100 = ResourceGuiSlot1S3",
		"filename = res_gui\\slot_01_s3.png",
	} {
		if !strings.Contains(cycleOutput, snippet) {
			t.Fatalf("missing %q in:\n%s", snippet, cycleOutput)
		}
	}

	pulsed := parseDocument("[Constants]\nglobal $pulse = 0\n[KeyPulse]\nkey = 1\ntype = activate\n$pulse = 1")
	if len(slotValueStates(pulsed.Slots[0], defaultSettings(), nil)) != 0 {
		t.Fatal("activate keys should not produce value states")
	}
	if slotActiveCondition(pulsed.Slots[0], defaultSettings(), nil) != "" {
		t.Fatal("activate keys should not have an active condition")
	}
	pulseOutput := generateFrom(pulsed, pulsed.Slots, defaultSettings())
	if strings.Contains(pulseOutput, "ResourceGuiSlot1S2") || strings.Contains(pulseOutput, "slot_01_s2") {
		t.Fatalf("unexpected activate variants:\n%s", pulseOutput)
	}

	dual := parseDocument("[KeyA]\nkey = 1\ntype = toggle\n$a = 1\n[KeyB]\nkey = 2\ntype = toggle\n$b = 1")
	merged := mergeSlots(dual.Slots, []string{dual.Slots[0].ID, dual.Slots[1].ID}, "allKeys")
	if cond := slotActiveCondition(merged[0], defaultSettings(), nil); cond != "($a == 1) || ($b == 1)" {
		t.Fatalf("merged active condition: %q", cond)
	}
	states = slotValueStates(merged[0], defaultSettings(), nil)
	if len(states) == 0 || states[0].Variable != "$a" {
		t.Fatalf("merged value state variable: %+v", states)
	}
}

func TestGenerateAssetPathsSkipAndTitle(t *testing.T) {
	t.Parallel()
	document := parseDocument("[KeyA]\nkey = 1\n$x = 0,1\n[KeyB]\nkey = 2\n$y = 0,1")
	slots := []MenuMakerSlot{document.Slots[0], document.Slots[1]}
	slots[0].Skip = true
	paths := assetPaths(slots, defaultSettings(), parseInitialConstants(document.Sections))
	want := []string{
		"res_gui/draw_2d.hlsl",
		"res_gui/bg.png",
		"res_gui/slot_01.png",
		"res_gui/slot_hover_01.png",
		"res_gui/slot_01_s2.png",
		"res_gui/slot_hover_01_s2.png",
	}
	if !slices.Equal(paths, want) {
		t.Fatalf("asset paths: %v", paths)
	}
	titled := defaultSettings()
	titled.Title = "GUI Menu"
	titledPaths := assetPaths(slots, titled, parseInitialConstants(document.Sections))
	if titledPaths[2] != "res_gui/title.png" {
		t.Fatalf("titled asset paths: %v", titledPaths)
	}
	pulsed := parseDocument("[KeyPulse]\nkey = 1\ntype = activate\n$pulse = 1")
	if got := assetPaths(pulsed.Slots, defaultSettings(), nil); !slices.Equal(got, []string{
		"res_gui/draw_2d.hlsl", "res_gui/bg.png", "res_gui/slot_01.png", "res_gui/slot_hover_01.png",
	}) {
		t.Fatalf("activate asset paths: %v", got)
	}
}

func generateFrom(document MenuMakerDocument, slots []MenuMakerSlot, settings MenuMakerSettings) string {
	geometry := calculateGeometry(slots, settings)
	return generateINI(document, slots, settings, geometry, parseInitialConstants(document.Sections))
}

func mergeSlots(slots []MenuMakerSlot, selectedIDs []string, mode string) []MenuMakerSlot {
	selected := make([]MenuMakerSlot, 0, len(selectedIDs))
	idSet := map[string]bool{}
	for _, id := range selectedIDs {
		idSet[id] = true
	}
	for _, slot := range slots {
		if idSet[slot.ID] {
			selected = append(selected, slot)
		}
	}
	if len(selected) < 2 {
		return slots
	}
	primary := selected[0]
	originalKeys := []string{}
	names := make([]string, 0, len(selected))
	ids := make([]string, 0, len(selected))
	for _, slot := range selected {
		originalKeys = append(originalKeys, slot.OriginalKeys...)
		names = append(names, slot.Name)
		ids = append(ids, slot.ID)
	}
	merged := primary
	merged.ID = "merged-" + strings.Join(ids, "-")
	merged.Handlers = uniqueHandlers(selected)
	merged.OriginalKeys = uniqueNormalizedKeys(originalKeys)
	merged.Name = strings.Join(names, " + ")
	merged.MergeMode = mode
	out := make([]MenuMakerSlot, 0, len(slots)-len(selected)+1)
	for _, slot := range slots {
		if slot.ID == primary.ID {
			out = append(out, merged)
			continue
		}
		if idSet[slot.ID] {
			continue
		}
		out = append(out, slot)
	}
	return out
}
