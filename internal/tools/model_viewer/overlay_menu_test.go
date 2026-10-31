package modelviewer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const overlayTestINI = `
[Constants]
global $menu = 0
global $active = 0
global $hovering
global $hoveredSlot
global $clickedSlot
global $slot
global $hair = 0
global $chest = 9
global $page = 0
global $ww
global $wh
global $cpx
global $cpy
[KeyMenu]
key = /
type = cycle
$menu = 0,1
[KeyClick]
condition = $hovering == 1 && $menu == 1
key = no_ctrl VK_LBUTTON
run = CommandListClicked
[Present]
post $active = 0
run = CommandListUnrelated
if $active
    if $menu
        run = CommandListMenu
    endif
endif
[CommandListUnrelated]
ps-t100 = ResourceCredit
x87 = 0.5
y87 = 0.5
z87 = 0
w87 = 0
run = CustomShaderElement
[CommandListClicked]
$clickedSlot = $hoveredSlot
if $clickedSlot == 1
  $hair = $hair + 1
  if $hair > 2
    $hair = 0
  endif
elif $clickedSlot == 2
  $chest = $chest + 1
  if $chest > 10
    $chest = 10
  endif
elif $clickedSlot == 3
  $page = 1
elif $clickedSlot == 4
  run = CommandListCloseMenu
endif
[CommandListCloseMenu]
$hovering = 0
$hair = 2
[CommandListMenu]
$ww = window_width
$wh = window_height
$cpx = cursor_x
$cpy = cursor_y
ps-t100 = ResourceBack
x87 = 200 / $ww
y87 = 300 / $wh
z87 = 100 / $ww
w87 = 50 / $wh
run = CustomShaderElement
$hovering = 0
x87 = 64 / $ww
y87 = 64 / $wh
z87 = 120 / $ww
w87 = 70 / $wh
$slot = 1
run = CommandListSlot
w87 = 170 / $wh
$slot = 2
run = CommandListSlot
w87 = 270 / $wh
$slot = 3
run = CommandListSlot
ps-t100 = ResourceFrame
x87 = 32 / $ww
y87 = 32 / $wh
z87 = 240 / $ww
w87 = 70 / $wh
run = CustomShaderElement
[CommandListSlot]
if $cpx > z87 && $cpx < z87 + x87 && $cpy > w87 && $cpy < w87 + y87
    $hovering = 1
    $hoveredSlot = $slot
    ps-t100 = ResourceHover
else
    ps-t100 = ResourceFrame
endif
run = CustomShaderElement
[CustomShaderElement]
ps = .\ui.hlsl
Draw = 4,0
[ResourceBack]
filename = back.png
[ResourceFrame]
filename = frame.png
[ResourceHover]
filename = hover.png
[ResourceCredit]
filename = credit.png
`

const overlayTestShader = `
Texture1D<float4> ConfigData : register(t120);
#define DIMENSIONS ConfigData[87].xy
#define SHIFT ConfigData[87].zw
Texture2D<float4> imageTex : register(t100);
`

func buildOverlayTestMenu(t *testing.T, frameFile string) []ModelViewerOverlayMenu {
	t.Helper()
	return buildOverlayTestMenuFrom(t, context.Background(), overlayTestINI, frameFile)
}

func buildOverlayTestMenuFrom(
	t *testing.T,
	ctx context.Context,
	ini, frameFile string,
) []ModelViewerOverlayMenu {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"ui.hlsl": overlayTestShader}
	for _, name := range []string{"back.png", "hover.png", "credit.png", frameFile} {
		files[name] = "x"
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scan := modelViewerGeometryScan{
		sections: parseModINI(ini),
		resources: []modelViewerResource{
			{Name: "Back", Filename: "back.png"},
			{Name: "Frame", Filename: frameFile},
			{Name: "Hover", Filename: "hover.png"},
			{Name: "Credit", Filename: "credit.png"},
		},
		iniDir: dir,
	}
	variables := []ModelViewerVariable{
		{ID: "hair", Label: "Hair", Values: []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}, {Value: 2.0}}},
		{ID: "chest", Label: "Chest", Slider: &ModelViewerSlider{Min: 0, Max: 10, Step: 1}},
	}
	return buildModelViewerOverlayMenus(ctx, dir, []modelViewerGeometryScan{scan}, variables, nil)
}

func TestModelViewerOverlayMenuReproducesLayoutAndClicks(t *testing.T) {
	menus := buildOverlayTestMenu(t, "frame.png")
	if len(menus) != 1 {
		t.Fatalf("menus = %d, want 1", len(menus))
	}
	menu := menus[0]
	if menu.Width != 200 || menu.Height != 300 {
		t.Fatalf("size = %vx%v, want 200x300", menu.Width, menu.Height)
	}
	// The closed-menu gates are passed, and the list drawn outside the menu stays out.
	if len(menu.Quads) != 5 {
		t.Fatalf("quads = %d, want the backdrop, three slots and a decoration", len(menu.Quads))
	}

	// The slot changing a variable outside the toggles, like a page button, is no button.
	// Neither is the decoration, where no click branch matches.
	if len(menu.Buttons) != 2 {
		t.Fatalf("buttons = %+v, want hair and chest", menu.Buttons)
	}
	hair, chest := menu.Buttons[0], menu.Buttons[1]
	if hair.Variable != "hair" || hair.X != 20 || hair.Y != 20 || hair.Width != 64 {
		t.Fatalf("hair button = %+v", hair)
	}
	wantHair := []ModelViewerOverlayTransition{{From: 0, To: 1}, {From: 1, To: 2}, {From: 2, To: 0}}
	if len(hair.Transitions) != len(wantHair) {
		t.Fatalf("hair transitions = %v, want %v", hair.Transitions, wantHair)
	}
	for index, want := range wantHair {
		if hair.Transitions[index] != want {
			t.Fatalf("hair transitions = %v, want %v", hair.Transitions, wantHair)
		}
	}
	// A clamped step stops at the bound instead of wrapping.
	if chest.Variable != "chest" || len(chest.Transitions) != 10 || chest.Transitions[9].To != 10 {
		t.Fatalf("chest button = %+v", chest)
	}
	if len(hair.Hover) != 1 || hair.Hover[0].Quad != 1 || filepath.Base(hair.Hover[0].Path) != "hover.png" {
		t.Fatalf("hair hover = %+v", hair.Hover)
	}
}

func TestModelViewerOverlayMenuSkipsTexturesTheRendererCannotShow(t *testing.T) {
	if menus := buildOverlayTestMenu(t, "frame.tga"); len(menus) != 0 {
		t.Fatalf("menus = %+v, want none", menus)
	}
}

func TestModelViewerOverlayMenuFollowsToggleStateAndOwnVariable(t *testing.T) {
	dir := t.TempDir()
	const quadShader = "Texture1D<float4> IniParams : register(t120);\n" +
		"#define SIZE IniParams[87].xy\n#define OFFSET IniParams[87].zw\n" +
		"Texture2D<float4> tex : register(t100);\n"
	files := map[string]string{
		"draw.hlsl":   quadShader,
		"dimmed.hlsl": quadShader + "    color.a = color.a * 0.2;\n",
		"bg.dds":      "x",
		"icon.dds":    "x",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scan := modelViewerGeometryScan{
		sections: parseModINI(`
[Constants]
global $active
global $menu = 0
global $hovering
global $hoveredSlot
global $clickedSlot
global $armbinder = 0
global $gloves = 1
global $hold
[KeyHold]
key = VK_LBUTTON
type = hold
$hold = 1
[KeyClick]
condition = $hovering == 1 && $menu == 1
key = VK_LBUTTON
run = CommandListClicked
[Present]
if $active
    if $menu
        run = CommandListMenu
    endif
endif
[CommandListClicked]
$clickedSlot = $hoveredSlot
if $clickedSlot == 1
    $armbinder = 1 - $armbinder
    if $armbinder == 1
        $gloves = 0
    endif
endif
[CommandListMenu]
ps-t100 = ResourceBg
x87 = 0.5
y87 = 0.5
z87 = 0
w87 = 0
run = CustomShaderDraw
$hovering = 0
x87 = 0.1
y87 = 0.1
if $hold
    z87 = 0.25
endif
ps-t100 = ResourceIcon
if $armbinder == 0
    run = CustomShaderDraw
else
    run = CustomShaderDimmed
endif
if cursor_x > z87 && cursor_x < z87 + x87 && cursor_y > w87 && cursor_y < w87 + y87
    $hovering = 1
    $hoveredSlot = 1
endif
[CustomShaderDraw]
ps = draw.hlsl
[CustomShaderDimmed]
ps = dimmed.hlsl
[ResourceBg]
filename = bg.dds
[ResourceIcon]
filename = icon.dds
`),
		resources: []modelViewerResource{
			{Name: "Bg", Filename: "bg.dds"},
			{Name: "Icon", Filename: "icon.dds"},
		},
		iniDir: dir,
	}
	binary := []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}}
	variables := []ModelViewerVariable{
		{ID: "gloves", Values: binary},
		{ID: "armbinder", Values: binary},
	}

	menus := buildModelViewerOverlayMenus(context.Background(), dir, []modelViewerGeometryScan{scan}, variables, nil)
	if len(menus) != 1 || len(menus[0].Buttons) != 1 {
		t.Fatalf("menus = %+v, want one with one button", menus)
	}
	if button := menus[0].Buttons[0]; button.Variable != "armbinder" {
		t.Fatalf("button = %+v, want the variable the click sets first", button)
	}
	icon := menus[0].Quads[1]
	if icon.X != 0 {
		t.Fatalf("icon = %+v, want it placed as if no key were held", icon)
	}
	if icon.Opacity != 1 || len(icon.States) != 1 {
		t.Fatalf("icon = %+v, want one state", icon)
	}
	if state := icon.States[0]; state.Variable != "armbinder" || state.Value != 1 || state.Opacity != 0.2 {
		t.Fatalf("icon state = %+v, want armbinder=1 dimmed", state)
	}
}

func TestModelViewerOverlayMenuReadsCustomShaderBodyAndRegisterZero(t *testing.T) {
	const ini = `
[Constants]
global $hovering
global $on = 0
[KeyClick]
condition = $hovering == 1
key = VK_LBUTTON
run = CommandListClicked
[Present]
run = CommandListMenu
[CommandListClicked]
$on = 1 - $on
[CommandListMenu]
x = 0.5
y = 0.5
z = 0
w = 0
run = CustomShaderBack
$hovering = 0
x0 = 0.1
y0 = 0.1
if cursor_x > z && cursor_x < z + x && cursor_y > w && cursor_y < w + y
    $hovering = 1
endif
run = CustomShaderIcon
[CustomShaderBack]
ps = ui.hlsl
ps-t0 = ResourceBack
[CustomShaderIcon]
ps = ui.hlsl
ps-t0 = ResourceIcon
`
	dir := t.TempDir()
	files := map[string]string{
		"ui.hlsl": "Texture1D<float4> IniParams : register(t120);\n#define SIZE IniParams[0].xy\n" +
			"#define OFFSET IniParams[0].zw\nTexture2D<float4> tex : register(t0);\n",
		"back.png": "x",
		"icon.png": "x",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	build := func(source string) []ModelViewerOverlayMenu {
		scan := modelViewerGeometryScan{
			sections: parseModINI(source),
			resources: []modelViewerResource{
				{Name: "Back", Filename: "back.png"},
				{Name: "Icon", Filename: "icon.png"},
				{Name: "NotShipped", Filename: "not_shipped.png"},
				{Name: "LiveTarget"},
			},
			iniDir: dir,
		}
		variables := []ModelViewerVariable{{ID: "on", Values: []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}}}}
		return buildModelViewerOverlayMenus(context.Background(), dir, []modelViewerGeometryScan{scan}, variables, nil)
	}

	menus := build(ini)
	if len(menus) != 1 || len(menus[0].Quads) != 2 || len(menus[0].Buttons) != 1 {
		t.Fatalf("menus = %+v, want two quads and one button", menus)
	}
	if button := menus[0].Buttons[0]; button.Width != 192 || button.Height != 108 {
		t.Fatalf("button = %+v, want the size set through x0 and y0", button)
	}

	// A texture filled at run time cannot be shown, so the menu must not appear without it.
	live := strings.Replace(ini, "run = CustomShaderIcon", "run = CustomShaderLive\nrun = CustomShaderIcon", 1) +
		"[CustomShaderLive]\nps = ui.hlsl\nps-t0 = ResourceLiveTarget\n"
	if menus := build(live); len(menus) != 0 {
		t.Fatalf("menus = %+v, want none", menus)
	}

	// The game draws nothing for a file the mod does not ship, so the rest still reproduces it.
	notShipped := strings.Replace(ini, "run = CustomShaderIcon", "run = CustomShaderGone\nrun = CustomShaderIcon", 1) +
		"[CustomShaderGone]\nps = ui.hlsl\nps-t0 = ResourceNotShipped\n"
	if menus := build(notShipped); len(menus) != 1 || len(menus[0].Quads) != 2 {
		t.Fatalf("menus = %+v, want the menu without the missing file", menus)
	}
}

func TestModelViewerMenuEffectKeepsEnclosingGuards(t *testing.T) {
	_, _, effects, ok := parseModelViewerMenuBranch([]string{
		"$ringgag = 1 - $ringgag",
		"if $ringgag == 0",
		"  if $tape == 0",
		"    $tape = 1",
		"  endif",
		"endif",
	})
	if !ok || len(effects) != 1 {
		t.Fatalf("effects = %+v", effects)
	}
	variable := ModelViewerVariable{ID: "ringgag", Effects: effects}

	state := map[string]any{"ringgag": float64(0), "tape": float64(0)}
	if next := applyViewerVariableSelection(state, variable, float64(1)); modelViewerString(next["tape"]) != "0" {
		t.Fatalf("tape = %v after enabling, want it left alone", next["tape"])
	}
	if next := applyViewerVariableSelection(state, variable, float64(0)); modelViewerString(next["tape"]) != "1" {
		t.Fatalf("tape = %v after disabling, want 1", next["tape"])
	}
}

func TestModelViewerOverlayMenuGivesUpOnTruncatedRuns(t *testing.T) {
	recursive := strings.Replace(overlayTestINI, "[CommandListSlot]\n", "[CommandListSlot]\nrun = CommandListSlot\n", 1)
	if menus := buildOverlayTestMenuFrom(t, context.Background(), recursive, "frame.png"); len(menus) != 0 {
		t.Fatalf("menus = %+v, want none", menus)
	}
}

func TestModelViewerOverlayMenuStopsWhenCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if menus := buildOverlayTestMenuFrom(t, ctx, overlayTestINI, "frame.png"); len(menus) != 0 {
		t.Fatalf("menus = %+v, want none", menus)
	}
}

func TestModelViewerOverlayDomainHasExactSliderValues(t *testing.T) {
	domain := modelViewerOverlayDomain(ModelViewerVariable{Slider: &ModelViewerSlider{Min: 0, Max: 1, Step: 0.01}})
	if len(domain) != 101 || domain[7] != 0.07 || domain[30] != 0.3 || domain[100] != 1 {
		t.Fatalf("domain = %v", domain)
	}
}
