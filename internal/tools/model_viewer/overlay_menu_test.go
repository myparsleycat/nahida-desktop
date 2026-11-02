package modelviewer

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
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
	return buildModelViewerOverlayMenus(ctx, dir, []modelViewerGeometryScan{scan}, variables, nil, nil)
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
	if menu.X != 100 || menu.Y != 50 {
		t.Fatalf("corner = %v,%v, want where the mod draws the backdrop, 100,50", menu.X, menu.Y)
	}
	// The closed-menu gates are passed, and the list drawn outside the menu stays out.
	if len(menu.Pages[0].Quads) != 5 {
		t.Fatalf("quads = %d, want the backdrop, three slots and a decoration", len(menu.Pages[0].Quads))
	}

	// The slot changing a variable that nothing draws from leads nowhere, so it is no button.
	// Neither is the decoration, where no click branch matches.
	if len(menu.Pages[0].Buttons) != 2 {
		t.Fatalf("buttons = %+v, want hair and chest", menu.Pages[0].Buttons)
	}
	hair, chest := menu.Pages[0].Buttons[0], menu.Pages[0].Buttons[1]
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

const overlayClipSpaceTestShader = `
Texture1D<float4> IniParams : register(t120);
void main(out vs2ps output, uint vertex : SV_VertexID)
{
	switch(vertex) {
		case 0: output.pos.xy = float2(IniParams[5].x, IniParams[5].y); break;
		case 1: output.pos.xy = float2(IniParams[5].x, IniParams[6].y); break;
		case 2: output.pos.xy = float2(IniParams[6].x, IniParams[5].y); break;
		case 3: output.pos.xy = float2(IniParams[6].x, IniParams[6].y); break;
		default: output.pos.xy = 0; break;
	};
}
Texture2D<float4> tex : register(t100);
`

const overlayClipSpaceTestINI = `
[KeyOpen]
key = y
type = cycle
$menu = 0,1
[KeyClick]
condition = $menu == 1
key = VK_LBUTTON
run = CommandListSelect
[Constants]
global $menu = 0
global $top = 1
global $hat = 0
[Present]
if $menu == 1
    run = CustomShaderMenu
endif
[CustomShaderMenu]
vs = draw.hlsl
ps = draw.hlsl
x5 = -1.0
y5 = -1.0
x6 = 1.0
y6 = 1.0
ps-t100 = ResourceBack
Draw = 4,0
x5 = -0.5
y5 = -0.5
x6 = 0
y6 = 0.5
ps-t100 = ResourceIcon
Draw = 4,0
if $top == 1
    ps-t100 = ResourceMark
    Draw = 4,0
endif
x5 = 0
y5 = -0.5
x6 = 0.5
y6 = 0.5
ps-t100 = ResourceIcon
Draw = 4,0
if $hat == 1
    ps-t100 = ResourceMark
    Draw = 4,0
endif
[CommandListSelect]
if cursor_x > 0.25 && cursor_x < 0.5
    $top = ($top + 1) % 2
endif
if cursor_x > 0.5 && cursor_x < 0.75
    $hat = ($hat + 1) % 2
endif
[ResourceBack]
filename = back.buf
[ResourceIcon]
filename = icon.buf
[ResourceMark]
filename = mark.buf
`

// A shared-pattern selection menu places quads by clip space corners inside one
// custom shader, ships PNGs as .buf, and draws selection marks only for some values.
func TestModelViewerOverlayMenuReadsClipSpaceSelectionMenu(t *testing.T) {
	menus := buildOverlayClipSpaceTestMenus(t, overlayClipSpaceTestINI)
	if len(menus) != 1 {
		t.Fatalf("menus = %d, want 1", len(menus))
	}
	menu := menus[0]
	if menu.Width != 1200 || menu.Height != 700 {
		t.Fatalf("size = %vx%v, want the painted part of the backdrop, 1200x700", menu.Width, menu.Height)
	}
	if menu.X != 400 || menu.Y != 200 {
		t.Fatalf("corner = %v,%v, want the painted part of the backdrop, 400,200", menu.X, menu.Y)
	}

	quads := menu.Pages[0].Quads
	if len(quads) != 5 {
		t.Fatalf("quads = %+v, want the backdrop, two icons and a mark for each toggle", quads)
	}
	shownMark, hiddenMark := quads[2], quads[4]
	if filepath.Base(shownMark.Path) != "mark.buf" || shownMark.X != 80 || shownMark.Y != 70 || shownMark.Width != 480 ||
		shownMark.Opacity != 1 ||
		len(shownMark.States) != 1 ||
		shownMark.States[0].Value != 0 ||
		shownMark.States[0].Opacity != 0 {
		t.Fatalf("mark drawn by default = %+v, want it to disappear at 0", shownMark)
	}
	if filepath.Base(hiddenMark.Path) != "mark.buf" || hiddenMark.Opacity != 0 || len(hiddenMark.States) != 1 ||
		hiddenMark.States[0].Variable != "hat" || hiddenMark.States[0].Value != 1 ||
		hiddenMark.States[0].Opacity != 1 {
		t.Fatalf("mark drawn only at 1 = %+v, want it hidden until then", hiddenMark)
	}

	buttons := menu.Pages[0].Buttons
	if len(buttons) != 2 || buttons[0].Variable != "top" || buttons[1].Variable != "hat" || buttons[0].X != 80 ||
		buttons[1].X != 560 {
		t.Fatalf("buttons = %+v", buttons)
	}
}

// A shader whose only Draw is behind pre or in a list it runs draws once per Draw,
// and one whose Draw a failed condition skips draws nothing.
func TestModelViewerOverlayMenuRecordsEachReachedDraw(t *testing.T) {
	menus := buildOverlayClipSpaceTestMenus(t, `
[KeyClick]
condition = $menu == 1
key = VK_LBUTTON
run = CommandListSelect
[Constants]
global $menu = 0
global $top = 1
global $hat = 0
[Present]
if $menu == 1
    run = CustomShaderBack
    run = CustomShaderIcon
    run = CustomShaderSkipped
endif
[CustomShaderBack]
vs = draw.hlsl
ps = draw.hlsl
x5 = -1.0
y5 = -1.0
x6 = 1.0
y6 = 1.0
ps-t100 = ResourceBack
pre Draw = 4,0
[CustomShaderIcon]
vs = draw.hlsl
ps = draw.hlsl
x5 = -0.5
y5 = -0.5
x6 = 0
y6 = 0.5
ps-t100 = ResourceIcon
run = CommandListDraw
[CustomShaderSkipped]
vs = draw.hlsl
ps = draw.hlsl
ps-t100 = ResourceMark
if $hat == 1
    Draw = 4,0
endif
[CommandListDraw]
Draw = 4,0
[CommandListSelect]
if cursor_x > 0.25 && cursor_x < 0.5
    $top = ($top + 1) % 2
endif
[ResourceBack]
filename = back.buf
[ResourceIcon]
filename = icon.buf
[ResourceMark]
filename = mark.buf
`)
	if len(menus) != 1 {
		t.Fatalf("menus = %d, want 1", len(menus))
	}
	var drawn []string
	for _, quad := range menus[0].Pages[0].Quads {
		drawn = append(drawn, filepath.Base(quad.Path))
	}
	if !slices.Equal(drawn, []string{"back.buf", "icon.buf"}) {
		t.Fatalf("quads = %v, want the backdrop and the icon once each", drawn)
	}
}

func buildOverlayClipSpaceTestMenus(t *testing.T, ini string) []ModelViewerOverlayMenu {
	t.Helper()
	dir := t.TempDir()
	back := image.NewNRGBA(image.Rect(0, 0, 1920, 1080))
	draw.Draw(back, image.Rect(400, 200, 1600, 900), image.NewUniform(color.White), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, back); err != nil {
		t.Fatal(err)
	}
	signature := "\x89PNG\r\n\x1a\n"
	for name, content := range map[string]string{
		"draw.hlsl": overlayClipSpaceTestShader,
		"back.buf":  encoded.String(),
		"icon.buf":  signature,
		"mark.buf":  signature,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scan := modelViewerGeometryScan{
		sections: parseModINI(ini),
		resources: []modelViewerResource{
			{Name: "Back", Filename: "back.buf"},
			{Name: "Icon", Filename: "icon.buf"},
			{Name: "Mark", Filename: "mark.buf"},
		},
		iniDir: dir,
	}
	binary := []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}}
	return buildModelViewerOverlayMenus(
		context.Background(),
		dir,
		[]modelViewerGeometryScan{scan},
		[]ModelViewerVariable{{ID: "top", Values: binary}, {ID: "hat", Values: binary}},
		nil,
		nil,
	)
}

const overlayPagedTestINI = `
[Constants]
global $hovering
global $hoveredSlot
global $clickedSlot
global $slot
global $hair = 0
global $chest = 0
global $page = 0
[KeyClick]
condition = $hovering == 1
key = VK_LBUTTON
run = CommandListClicked
[Present]
run = CommandListMenu
[CommandListClicked]
$clickedSlot = $hoveredSlot
if $clickedSlot == 1
  $hair = ($hair + 1) % 3
elif $clickedSlot == 2
  $chest = $chest + 1
elif $clickedSlot == -2
  $page = ($page - 1 + 3) % 3
elif $clickedSlot == -1
  $page = ($page + 1) % 3
endif
[CommandListMenu]
ps-t100 = ResourceBack
x87 = 200 / window_width
y87 = 300 / window_height
z87 = 100 / window_width
w87 = 50 / window_height
run = CustomShaderElement
$hovering = 0
if $page == 0
  run = CommandListPageZero
elif $page == 1
  run = CommandListPageOne
elif $page == 2
  run = CommandListPageTwo
endif
run = CommandListArrows
[CommandListPageZero]
$slot = 1
x87 = 64 / window_width
y87 = 64 / window_height
z87 = 120 / window_width
w87 = 70 / window_height
run = CommandListSlot
[CommandListPageOne]
$slot = 2
x87 = 64 / window_width
y87 = 64 / window_height
z87 = 180 / window_width
w87 = 170 / window_height
run = CommandListSlot
[CommandListPageTwo]
ps-t100 = ResourceCredit
x87 = 32 / window_width
y87 = 32 / window_height
z87 = 140 / window_width
w87 = 100 / window_height
run = CustomShaderElement
[CommandListSlot]
if cursor_x > z87 && cursor_x < z87 + x87 && cursor_y > w87 && cursor_y < w87 + y87
    $hovering = 1
    $hoveredSlot = $slot
    ps-t100 = ResourceHover
else
    ps-t100 = ResourceFrame
endif
run = CustomShaderElement
if $clickedSlot == $slot
    ps-t100 = ResourceHover
    run = CustomShaderElement
    $clickedSlot = 0
endif
[CommandListArrows]
ps-t100 = ResourceFrame
x87 = -32 / window_width
y87 = 32 / window_height
z87 = 100 / window_width
w87 = 200 / window_height
if cursor_x < z87 && cursor_x > z87 + x87 && cursor_y > w87 && cursor_y < w87 + y87
    $hovering = 1
    $hoveredSlot = -2
    ps-t100 = ResourceHover
endif
run = CustomShaderElement
ps-t100 = ResourceFrame
x87 = 32 / window_width
z87 = 300 / window_width
if cursor_x > z87 && cursor_x < z87 + x87 && cursor_y > w87 && cursor_y < w87 + y87
    $hovering = 1
    $hoveredSlot = -1
    ps-t100 = ResourceHover
endif
run = CustomShaderElement
[CustomShaderElement]
ps = .\ui.hlsl
Draw = 4,0
`

func overlayTestPageButtons(page ModelViewerOverlayPage) (back, forward int, toggles []ModelViewerOverlayButton) {
	back, forward = -1, -1
	for _, button := range page.Buttons {
		switch {
		case button.Page != nil && button.X == 0:
			back = *button.Page
		case button.Page != nil:
			forward = *button.Page
		default:
			toggles = append(toggles, button)
		}
	}
	return back, forward, toggles
}

func TestModelViewerOverlayMenuFollowsPageArrows(t *testing.T) {
	menus := buildOverlayTestMenuFrom(t, context.Background(), overlayPagedTestINI, "frame.png")
	if len(menus) != 1 || len(menus[0].Pages) != 3 {
		t.Fatalf("menus = %+v, want one with three pages despite the click marker", menus)
	}
	menu := menus[0]
	// The left arrow extends past the backdrop, and the corner is shared by all pages.
	if menu.Width != 264 || menu.Height != 300 {
		t.Fatalf("size = %vx%v, want 264x300", menu.Width, menu.Height)
	}

	// Walk forward from the first page: the mod numbers its pages hair, chest, decoration.
	first, forward, firstToggles := overlayTestPageButtons(menu.Pages[0])
	if len(firstToggles) != 1 || firstToggles[0].Variable != "hair" || firstToggles[0].X != 52 {
		t.Fatalf("first page toggles = %+v, want hair alone", firstToggles)
	}
	want := []ModelViewerOverlayTransition{{From: 0, To: 1}, {From: 1, To: 2}, {From: 2, To: 0}}
	if !slices.Equal(firstToggles[0].Transitions, want) {
		t.Fatalf("hair transitions = %v, want %v", firstToggles[0].Transitions, want)
	}
	back2, forward2, chestToggles := overlayTestPageButtons(menu.Pages[forward])
	if len(chestToggles) != 1 || chestToggles[0].Variable != "chest" || chestToggles[0].X != 112 ||
		chestToggles[0].Y != 120 {
		t.Fatalf("second page toggles = %+v, want chest alone at its own place", chestToggles)
	}
	back3, forward3, decorationToggles := overlayTestPageButtons(menu.Pages[forward2])
	if len(decorationToggles) != 0 {
		t.Fatalf("third page toggles = %+v, want none", decorationToggles)
	}
	if forward3 != 0 || back2 != 0 || back3 != forward || first != forward2 {
		t.Fatalf(
			"pages do not cycle: forward %d %d %d, back %d %d %d",
			forward,
			forward2,
			forward3,
			first,
			back2,
			back3,
		)
	}

	paths := func(page ModelViewerOverlayPage) []string {
		names := make([]string, 0, len(page.Quads))
		for _, quad := range page.Quads {
			names = append(names, filepath.Base(quad.Path))
		}
		return names
	}
	if got, wantPaths := paths(menu.Pages[forward2]), []string{
		"back.png", "credit.png", "frame.png", "frame.png",
	}; !slices.Equal(got, wantPaths) {
		t.Fatalf("third page draws %v, want %v", got, wantPaths)
	}

	// The left arrow is drawn with a negative width: mirrored, placed left of its corner.
	arrow := menu.Pages[0].Quads[2]
	if !arrow.FlipX || arrow.X != 0 || arrow.Width != 32 {
		t.Fatalf("left arrow = %+v, want it mirrored at the menu corner", arrow)
	}
	for _, button := range menu.Pages[0].Buttons {
		if button.Page != nil && (len(button.Hover) != 1 || filepath.Base(button.Hover[0].Path) != "hover.png") {
			t.Fatalf("arrow hover = %+v", button.Hover)
		}
	}
}

func TestModelViewerOverlayMenuPageVariations(t *testing.T) {
	replace := func(t *testing.T, ini, old, updated string) string {
		t.Helper()
		if !strings.Contains(ini, old) {
			t.Fatalf("fixture has no %q", old)
		}
		return strings.ReplaceAll(ini, old, updated)
	}

	t.Run("separate page conditions do not stack", func(t *testing.T) {
		ini := replace(t, overlayPagedTestINI, "elif $page == 1", "endif\nif $page == 1")
		ini = replace(t, ini, "elif $page == 2", "endif\nif $page == 2")
		// Lists that do not assign what the click key tests leave the menu's entry unmarked.
		ini = replace(t, ini, "$hovering = 0\nif $page == 0", "if $page == 0")
		ini = replace(t, ini, "condition = $hovering == 1\n", "")
		menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
		if len(menus) != 1 || len(menus[0].Pages) != 3 {
			t.Fatalf("menus = %+v, want three pages", menus)
		}
		for index, page := range menus[0].Pages {
			if len(page.Quads) != 4 {
				t.Fatalf("page %d quads = %d, want the backdrop, one element and two arrows", index, len(page.Quads))
			}
		}
	})

	t.Run("a first page opened by force is rebuilt", func(t *testing.T) {
		ini := replace(t, overlayPagedTestINI, "elif $page == 1", "endif\nif $page == 1")
		ini = replace(t, ini, "elif $page == 2", "endif\nif $page == 2")
		ini = replace(t, ini, "$hovering = 0\nif $page == 0", "if $page == 0")
		ini = replace(t, ini, "condition = $hovering == 1\n", "")
		// The first page's list marks nothing, so the page 1 condition is forced open at first.
		ini = replace(t, ini, "run = CommandListPageZero", "run = CommandListSwap")
		ini = replace(t, ini, "run = CommandListPageTwo", "run = CommandListPageZero")
		ini = replace(t, ini, "run = CommandListSwap", "run = CommandListPageTwo")
		menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
		if len(menus) != 1 || len(menus[0].Pages) != 3 {
			t.Fatalf("menus = %+v, want three pages", menus)
		}
		for index, page := range menus[0].Pages {
			if len(page.Quads) != 4 {
				t.Fatalf("page %d quads = %d, want the backdrop, one element and two arrows", index, len(page.Quads))
			}
		}
	})

	t.Run("finding the page number does not spend the budget again", func(t *testing.T) {
		busy := strings.Repeat("$slot = 1\n", 85000)
		ini := replace(t, overlayPagedTestINI, "[CommandListPageZero]\n", "[CommandListPageZero]\n"+busy)
		menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
		if len(menus) != 1 || len(menus[0].Pages) != 3 {
			t.Fatalf("menus = %d, want one with three pages", len(menus))
		}
	})

	t.Run("a selection copied from the hovered slot is no page", func(t *testing.T) {
		ini := replace(
			t,
			overlayPagedTestINI,
			"$clickedSlot = $hoveredSlot\n",
			"$clickedSlot = $hoveredSlot\n$selected = $hoveredSlot\n",
		)
		ini = replace(
			t,
			ini,
			"[CommandListArrows]\n",
			"[CommandListArrows]\nif $selected == 1\n    ps-t100 = ResourceHover\n    run = CustomShaderElement\nendif\n",
		)
		menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
		if len(menus) != 1 || len(menus[0].Pages) != 3 {
			t.Fatalf("menus = %+v, want the three pages alone", menus)
		}
	})

	t.Run("two pages keep both arrows", func(t *testing.T) {
		ini := replace(t, overlayPagedTestINI, "% 3", "% 2")
		ini = replace(t, ini, "- 1 + 3", "- 1 + 2")
		menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
		if len(menus) != 1 || len(menus[0].Pages) != 2 {
			t.Fatalf("menus = %+v, want two pages", menus)
		}
		back, forward, _ := overlayTestPageButtons(menus[0].Pages[0])
		if back != 1 || forward != 1 {
			t.Fatalf("arrows lead to %d and %d, want the other page from both", back, forward)
		}
	})

	t.Run("a page that cannot be drawn only loses its way in", func(t *testing.T) {
		ini := replace(t, overlayPagedTestINI, "ps-t100 = ResourceCredit", "ps-t100 = ResourceLive")
		ini += "[ResourceLive]\n"
		dir := t.TempDir()
		for name, content := range map[string]string{
			"ui.hlsl": overlayTestShader, "back.png": "x", "frame.png": "x", "hover.png": "x",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		scan := modelViewerGeometryScan{
			sections: parseModINI(ini),
			resources: []modelViewerResource{
				{Name: "Back", Filename: "back.png"},
				{Name: "Frame", Filename: "frame.png"},
				{Name: "Hover", Filename: "hover.png"},
				{Name: "Live"},
			},
			iniDir: dir,
		}
		variables := []ModelViewerVariable{
			{ID: "hair", Values: []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}, {Value: 2.0}}},
			{ID: "chest", Slider: &ModelViewerSlider{Min: 0, Max: 3, Step: 1}},
		}
		menus := buildModelViewerOverlayMenus(
			context.Background(), dir, []modelViewerGeometryScan{scan}, variables, nil, nil,
		)
		if len(menus) != 1 || len(menus[0].Pages) != 2 {
			t.Fatalf("menus = %+v, want the two pages that can be drawn", menus)
		}
		for index, page := range menus[0].Pages {
			for _, button := range page.Buttons {
				if button.Page != nil && (*button.Page < 0 || *button.Page >= len(menus[0].Pages)) {
					t.Fatalf("page %d button leads to %d", index, *button.Page)
				}
			}
		}
	})

	t.Run("unbounded stepping stops at the page limit", func(t *testing.T) {
		ini := replace(t, overlayPagedTestINI, "$page = ($page + 1) % 3", "$page = $page + 1")
		ini = replace(t, ini, "$page = ($page - 1 + 3) % 3", "$page = $page + 1")
		ini = replace(t, ini, "z87 = 300 / window_width", "z87 = (300 + $page) / window_width")
		menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
		if len(menus) != 1 || len(menus[0].Pages) != maxModelViewerOverlayPages {
			t.Fatalf("pages = %d, want the limit %d", len(menus[0].Pages), maxModelViewerOverlayPages)
		}
	})

	buildListed := func(
		t *testing.T,
		ini string,
		variables []ModelViewerVariable,
		menuOnly map[string]bool,
	) []ModelViewerOverlayMenu {
		t.Helper()
		dir := t.TempDir()
		for name, content := range map[string]string{
			"ui.hlsl": overlayTestShader, "back.png": "x", "frame.png": "x", "hover.png": "x", "credit.png": "x",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		scan := modelViewerGeometryScan{
			sections: parseModINI(ini),
			resources: []modelViewerResource{
				{Name: "Back", Filename: "back.png"},
				{Name: "Frame", Filename: "frame.png"},
				{Name: "Hover", Filename: "hover.png"},
				{Name: "Credit", Filename: "credit.png"},
			},
			iniDir: dir,
		}
		return buildModelViewerOverlayMenus(
			context.Background(), dir, []modelViewerGeometryScan{scan}, variables, nil, menuOnly,
		)
	}

	t.Run("a listed variable the draw overwrites stays a toggle", func(t *testing.T) {
		ini := replace(t, overlayPagedTestINI, "z87 = 120 / window_width", "z87 = (120 + $hair) / window_width")
		ini = replace(t, ini, "[CustomShaderElement]\n", "$hair = 0\n[CustomShaderElement]\n")
		variables := []ModelViewerVariable{
			{ID: "hair", Values: []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}, {Value: 2.0}}},
		}
		menus := buildListed(t, ini, variables, map[string]bool{"hair": true})
		if len(menus) != 1 || len(menus[0].stateVariables) != 0 {
			t.Fatalf("menus = %+v, want one claiming no listed variable", menus)
		}
		_, _, toggles := overlayTestPageButtons(menus[0].Pages[0])
		if len(toggles) != 1 || toggles[0].Variable != "hair" {
			t.Fatalf("toggles = %+v, want hair", toggles)
		}
	})

	t.Run("a listed page number leaves the toggles", func(t *testing.T) {
		variables := []ModelViewerVariable{
			{ID: "hair", Values: []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}, {Value: 2.0}}},
			{ID: "page", Values: []ModelViewerVariableValue{{Value: 0.0}, {Value: 1.0}, {Value: 2.0}}},
		}
		menuOnly := modelViewerOverlayMenuOnlyVariables(&ModelViewerTransport{
			Variables: variables,
			Meshes: []ModelViewerMeshTransport{
				{Conditions: ModelViewerDNF{{{Var: "hair", Value: "1"}}}},
			},
		})
		menus := buildListed(t, overlayPagedTestINI, variables, menuOnly)
		if len(menus) != 1 || len(menus[0].Pages) != 3 {
			t.Fatalf("menus = %+v, want three pages", menus)
		}
		for _, page := range menus[0].Pages {
			for _, button := range page.Buttons {
				if button.Variable == "page" {
					t.Fatalf("button = %+v, want the page number kept out of the toggles", button)
				}
			}
		}
		remaining := withoutModelViewerOverlayStateVariables(slices.Clone(variables), menus)
		if len(remaining) != 1 || remaining[0].ID != "hair" {
			t.Fatalf("variables = %+v, want hair alone", remaining)
		}
	})
}

func TestModelViewerOverlayMenuSkipsTexturesTheRendererCannotShow(t *testing.T) {
	if menus := buildOverlayTestMenu(t, "frame.tga"); len(menus) != 0 {
		t.Fatalf("menus = %+v, want none", menus)
	}
}

func TestModelViewerOverlayMenuKeepsClampedButtonsAtDefaultBounds(t *testing.T) {
	for _, initial := range []string{"0", "10"} {
		t.Run(initial, func(t *testing.T) {
			ini := strings.Replace(overlayTestINI, "global $chest = 9", "global $chest = "+initial, 1)
			ini = strings.Replace(ini, "$page = 1", "$chest = $chest - 1\n"+
				"  if $chest < 0\n    $chest = 0\n  endif", 1)
			menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
			if len(menus) != 1 {
				t.Fatalf("menus = %d, want 1", len(menus))
			}
			var buttons []ModelViewerOverlayButton
			for _, button := range menus[0].Pages[0].Buttons {
				if button.Variable == "chest" {
					buttons = append(buttons, button)
				}
			}
			if len(buttons) != 2 {
				t.Fatalf("chest buttons = %+v, want increase and decrease", buttons)
			}
			for index, button := range buttons {
				if len(button.Transitions) != 10 {
					t.Fatalf("button %d transitions = %v, want 10", index, button.Transitions)
				}
				for step, transition := range button.Transitions {
					want := ModelViewerOverlayTransition{From: float64(step), To: float64(step + 1)}
					if index == 1 {
						want = ModelViewerOverlayTransition{From: float64(step + 1), To: float64(step)}
					}
					if transition != want {
						t.Fatalf("button %d transition = %+v, want %+v", index, transition, want)
					}
				}
			}
		})
	}
}

func TestModelViewerOverlayMenuPrefersChangedVariableOverInitialization(t *testing.T) {
	ini := strings.Replace(overlayTestINI, "$chest = $chest + 1", "$hair = 0\n  $chest = $chest + 1", 1)
	menus := buildOverlayTestMenuFrom(t, context.Background(), ini, "frame.png")
	if len(menus) != 1 || len(menus[0].Pages[0].Buttons) != 2 {
		t.Fatalf("menus = %+v, want hair and chest buttons", menus)
	}
	chest := menus[0].Pages[0].Buttons[1]
	if chest.Variable != "chest" {
		t.Fatalf("second button = %+v, want chest instead of the unchanged hair initializer", chest)
	}
	for _, transition := range chest.Transitions {
		if transition.From == 9 && transition.To == 10 {
			return
		}
	}
	t.Fatalf("chest transitions = %+v, want the initial value 9 to advance to 10", chest.Transitions)
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

	menus := buildModelViewerOverlayMenus(
		context.Background(),
		dir,
		[]modelViewerGeometryScan{scan},
		variables,
		nil,
		nil,
	)
	if len(menus) != 1 || len(menus[0].Pages[0].Buttons) != 1 {
		t.Fatalf("menus = %+v, want one with one button", menus)
	}
	if button := menus[0].Pages[0].Buttons[0]; button.Variable != "armbinder" {
		t.Fatalf("button = %+v, want the variable the click sets first", button)
	}
	icon := menus[0].Pages[0].Quads[1]
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
		return buildModelViewerOverlayMenus(
			context.Background(),
			dir,
			[]modelViewerGeometryScan{scan},
			variables,
			nil,
			nil,
		)
	}

	menus := build(ini)
	if len(menus) != 1 || len(menus[0].Pages[0].Quads) != 2 || len(menus[0].Pages[0].Buttons) != 1 {
		t.Fatalf("menus = %+v, want two quads and one button", menus)
	}
	if button := menus[0].Pages[0].Buttons[0]; button.Width != 192 || button.Height != 108 {
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
	if menus := build(notShipped); len(menus) != 1 || len(menus[0].Pages[0].Quads) != 2 {
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
