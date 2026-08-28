package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type touchInteractiveEntry struct {
	Draft     TouchComponentDraft
	Component TouchComponentAnalysis
	Asset     TouchGeneratedAssets
}
type touchZoneOverrides struct{ Radius, Strength, Falloff, MaxOffset, Damping, Spring [touchZoneChannels]float64 }

func supportsTouchFrameNumberGuard(version string) bool {
	parts, ok := touchVersionParts(version)
	if !ok {
		return false
	}
	minimum := []int{1, 0, 2}
	for i := range max(len(parts), len(minimum)) {
		left, right := 0, 0
		if i < len(parts) {
			left = parts[i]
		}
		if i < len(minimum) {
			right = minimum[i]
		}
		if left != right {
			return left > right
		}
	}
	return true
}
func touchVersionParts(value string) ([]int, bool) {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V")
	raw := strings.Split(value, ".")
	if len(raw) == 0 {
		return nil, false
	}
	parts := make([]int, len(raw))
	for i, item := range raw {
		if item == "" {
			return nil, false
		}
		number, err := strconv.Atoi(item)
		if err != nil {
			return nil, false
		}
		parts[i] = number
	}
	return parts, true
}

func compileTouchINI(sourcePath, targetPath string, analysis TouchModAnalysis, drafts []TouchComponentDraft, assets []TouchGeneratedAssets, namespace, varPrefix string, useFrameGuard bool) (string, int, error) {
	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", 0, err
	}
	backup := targetPath + ".bak-before-touch"
	if err = os.WriteFile(backup, raw, 0600); err != nil {
		return "", 0, err
	}
	iniDir := filepath.Dir(targetPath)
	rebased := make([]TouchGeneratedAssets, len(assets))
	for i, asset := range assets {
		rebased[i] = rebaseTouchAsset(asset, iniDir)
	}
	entries := []touchInteractiveEntry{}
	for _, draft := range drafts {
		if !draft.Interactive || len(draft.Zones) == 0 {
			continue
		}
		var component *TouchComponentAnalysis
		for i := range analysis.Components {
			if analysis.Components[i].ID == draft.ComponentID {
				component = &analysis.Components[i]
				break
			}
		}
		var asset *TouchGeneratedAssets
		for i := range rebased {
			if rebased[i].ComponentID == draft.ComponentID {
				asset = &rebased[i]
				break
			}
		}
		if component != nil && asset != nil {
			entries = append(entries, touchInteractiveEntry{draft, *component, *asset})
		}
	}
	if len(entries) == 0 {
		return "", 0, contractError("No interactive components available for INI compile")
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	components := make([]TouchComponentAnalysis, len(entries))
	for i := range entries {
		components[i] = entries[i].Component
	}
	text = ensureTouchConstants(text, varPrefix, components)
	text = ensureTouchKeys(text, varPrefix)
	text = ensureTouchPresent(text, varPrefix)
	text = patchTouchBlendSections(text, varPrefix, components)
	text = patchTouchIBSections(text, varPrefix, components, useFrameGuard)
	text, err = appendTouchRuntime(text, varPrefix, entries, entries[0].Asset.RelativeDir)
	if err != nil {
		return "", 0, err
	}
	if err = os.WriteFile(targetPath, []byte(strings.ReplaceAll(text, "\n", "\r\n")), 0600); err != nil {
		return "", 0, err
	}
	return backup, len(entries), nil
}

func rebaseTouchAsset(asset TouchGeneratedAssets, iniDir string) TouchGeneratedAssets {
	relative, _ := filepath.Rel(iniDir, filepath.Dir(asset.ParamsAbsolutePath))
	asset.RelativeDir = filepath.ToSlash(relative)
	for i, path := range asset.MaskPaths {
		asset.MaskPaths[i] = filepath.ToSlash(filepath.Join(relative, filepath.Base(path)))
	}
	for i := range asset.ObjectMapPaths {
		rel, _ := filepath.Rel(iniDir, asset.ObjectMapPaths[i].AbsolutePath)
		asset.ObjectMapPaths[i].RelativePath = filepath.ToSlash(rel)
	}
	rel, _ := filepath.Rel(iniDir, asset.ParamsAbsolutePath)
	asset.ParamsRelativePath = filepath.ToSlash(rel)
	rel, _ = filepath.Rel(iniDir, asset.PreviewAbsolutePath)
	asset.PreviewRelativePath = filepath.ToSlash(rel)
	return asset
}

func ensureTouchConstants(text, varPrefix string, components []TouchComponentAnalysis) string {
	if strings.Contains(text, "global $"+varPrefix+"_active") {
		return text
	}
	lines := []string{"", "; Nahida Touch Profile state (" + varPrefix + ")"}
	for _, suffix := range []string{"active = 0", "initialized = 0", "detect_allowed = 0", "mode = 0", "lmb_down = 0", "rmb_down = 0", "x_down = 0", "modifier_down = 0", "lmb_prev = 0", "rmb_prev = 0", "combo_active = 0", "mouse_down = 0", "poke_sign = 0", "poke_mult = 1.0", "charge_sign = 0", "lmb_press_time = 0", "rmb_press_time = 0", "cursor_x = 0", "cursor_y = 0", "screen_w = 1", "screen_h = 1", "delta_time = 0.0166667", "prev_time = 0"} {
		lines = append(lines, "global $"+varPrefix+"_"+suffix)
	}
	for _, component := range components {
		lines = append(lines, "global $"+varPrefix+"_last_dispatch_"+touchToken(component.ID)+" = -1")
	}
	lines = append(lines, "")
	block := strings.Join(lines, "\n")
	re := regexp.MustCompile(`(?i)(\[Constants\][^\n]*\n)`)
	if re.MatchString(text) {
		return re.ReplaceAllString(text, "${1}"+block)
	}
	return "[Constants]\n" + block + "\n" + text
}

func ensureTouchKeys(text, varPrefix string) string {
	st := touchSectionToken(varPrefix)
	if strings.Contains(text, "[Key"+st+"LMB]") {
		return text
	}
	block := touchTemplate(`
[Key{{S}}LMB]
key = VK_LBUTTON
type = hold
${{V}}_lmb_down = 1
post ${{V}}_lmb_down = 0

[Key{{S}}RMB]
key = VK_RBUTTON
type = hold
${{V}}_rmb_down = 1
post ${{V}}_rmb_down = 0

[Key{{S}}X]
key = X
type = hold
${{V}}_x_down = 1
post ${{V}}_x_down = 0

[Key{{S}}Modifier]
key = VK_MENU
type = hold
${{V}}_modifier_down = 1
${{V}}_mode = 1
post ${{V}}_modifier_down = 0
post ${{V}}_mode = 0
`, varPrefix, st, "", "")
	re := regexp.MustCompile(`(?i)(\[Present\])`)
	if re.MatchString(text) {
		return re.ReplaceAllString(text, block+"\n${1}")
	}
	return strings.TrimRight(text, " \t\r\n") + "\n" + block + "\n"
}

func ensureTouchPresent(text, varPrefix string) string {
	st := touchSectionToken(varPrefix)
	re := regexp.MustCompile(`(?is)\[Present\]([\s\S]*?)(?:\n\[|$)`)
	loc := re.FindStringSubmatchIndex(text)
	if loc == nil {
		return strings.TrimRight(text, " \t\r\n") + "\n\n[Present]\nrun = CommandList" + st + "Present\npost $" + varPrefix + "_active = 0\n"
	}
	full := text[loc[0]:loc[1]]
	body := text[loc[2]:loc[3]]
	if !strings.Contains(body, "CommandList"+st+"Present") {
		body = "\nrun = CommandList" + st + "Present" + body
	}
	if !strings.Contains(body, "post $"+varPrefix+"_active = 0") {
		body += "post $" + varPrefix + "_active = 0\n"
	}
	return strings.Replace(text, full, "[Present]"+body, 1)
}

func patchTouchBlendSections(text, varPrefix string, components []TouchComponentAnalysis) string {
	for _, component := range components {
		if component.BlendSectionName == nil {
			continue
		}
		header := "[TextureOverride" + *component.BlendSectionName + "]"
		full, body, ok := matchTouchINISection(text, header)
		if !ok || strings.Contains(body, "$"+varPrefix+"_active = 1") {
			continue
		}
		active := regexp.MustCompile(`(?i)(\$active\s*=\s*1[^\n]*\n)`)
		if active.MatchString(body) {
			body = active.ReplaceAllString(body, "${1}\t$"+varPrefix+"_active = 1\n")
		} else {
			body += "\t$active = 1\n\t$" + varPrefix + "_active = 1\n"
		}
		text = strings.Replace(text, full, header+body, 1)
	}
	return text
}

func patchTouchIBSections(text, varPrefix string, components []TouchComponentAnalysis, useFrame bool) string {
	st := touchSectionToken(varPrefix)
	groups := map[string][]TouchComponentAnalysis{}
	order := []string{}
	for _, component := range components {
		if component.IBSectionName == nil {
			continue
		}
		name := *component.IBSectionName
		if _, ok := groups[name]; !ok {
			order = append(order, name)
		}
		groups[name] = append(groups[name], component)
	}
	for _, name := range order {
		header := "[TextureOverride" + name + "]"
		full, body, ok := matchTouchINISection(text, header)
		if !ok {
			continue
		}
		skip := false
		for _, component := range groups[name] {
			if strings.Contains(body, "CustomShader"+st+"Bake"+touchToken(component.ID)) {
				skip = true
			}
		}
		if skip {
			continue
		}
		injections := []string{}
		for _, component := range groups[name] {
			injections = append(injections, buildTouchIBInjection(component, varPrefix, st, useFrame))
		}
		inject := strings.Join(injections, "\n")
		ibRE := regexp.MustCompile(`(?im)^\s*ib\s*=\s*.+$`)
		if loc := ibRE.FindStringIndex(body); loc != nil {
			body = body[:loc[1]] + "\n" + strings.TrimRight(inject, "\n") + body[loc[1]:]
		} else {
			runRE := regexp.MustCompile(`(?im)^\s*run\s*=\s*CommandList[^\r\n]*$`)
			runs := runRE.FindAllStringIndex(body, -1)
			if len(runs) > 0 {
				loc := runs[len(runs)-1]
				body = body[:loc[1]] + "\n" + strings.TrimRight(inject, "\n") + body[loc[1]:]
			} else {
				body = inject + body
			}
		}
		text = strings.Replace(text, full, header+body, 1)
	}
	return text
}

func buildTouchIBInjection(component TouchComponentAnalysis, varPrefix, st string, useFrame bool) string {
	id := touchToken(component.ID)
	frame := "time"
	if useFrame {
		frame = "FRAME_NUMBER"
	}
	lines := touchTemplate(`if ${{V}}_detect_allowed == 1
	run = CustomShader{{S}}Bake{{ID}}
	run = CustomShader{{S}}Detect{{ID}}
endif
if ${{V}}_mode == 1
	if {{FRAME}} != ${{V}}_last_dispatch_{{ID}}
		run = CustomShader{{S}}Jiggle{{ID}}
		${{V}}_last_dispatch_{{ID}} = {{FRAME}}
	endif
	vb0 = Resource{{S}}TempVB{{ID}}
endif`, varPrefix, st, "", id)
	lines = strings.ReplaceAll(lines, "{{FRAME}}", frame)
	if component.VariantCondition == nil {
		return lines
	}
	indented := []string{}
	for _, line := range strings.Split(lines, "\n") {
		indented = append(indented, "\t"+line)
	}
	return "if " + *component.VariantCondition + "\n" + strings.Join(indented, "\n") + "\nendif"
}

func appendTouchRuntime(text, varPrefix string, entries []touchInteractiveEntry, runtimeDir string) (string, error) {
	st := touchSectionToken(varPrefix)
	if strings.Contains(text, "[CommandList"+st+"Present]") {
		return text, nil
	}
	overrides, err := buildTouchZoneOverrides(entries)
	if err != nil {
		return "", err
	}
	lines := []string{"", "; ---- Nahida Touch Profile runtime (" + varPrefix + ") ----", ""}
	lines = append(lines, buildTouchCursorPresent(varPrefix, st, entries)...)
	lines = append(lines, buildTouchSharedShaders(varPrefix, st, entries, overrides, runtimeDir)...)
	for _, entry := range entries {
		lines = append(lines, buildTouchComponentShaders(varPrefix, st, entry, overrides, runtimeDir)...)
	}
	lines = append(lines, buildTouchSharedResources(st)...)
	for _, entry := range entries {
		lines = append(lines, buildTouchComponentResources(st, entry)...)
	}
	return strings.TrimRight(text, " \t\r\n") + "\n" + strings.Join(lines, "\n") + "\n", nil
}

func matchTouchINISection(text, header string) (string, string, bool) {
	re := regexp.MustCompile(`(?is)` + regexp.QuoteMeta(header) + `([^\[]*)`)
	match := re.FindStringSubmatch(text)
	if len(match) != 2 {
		return "", "", false
	}
	return match[0], match[1], true
}
func touchToken(value string) string {
	return regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(value, "")
}
func touchSectionToken(value string) string {
	parts := regexp.MustCompile(`[^a-zA-Z0-9]+`).Split(value, -1)
	out := ""
	for _, part := range parts {
		if part != "" {
			out += strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return out
}
func touchTemplate(value, varPrefix, st, runtime, id string) string {
	replacements := map[string]string{"{{V}}": varPrefix, "{{S}}": st, "{{R}}": runtime, "{{ID}}": id}
	for from, to := range replacements {
		value = strings.ReplaceAll(value, from, to)
	}
	return value
}

func buildTouchCursorPresent(varPrefix, st string, entries []touchInteractiveEntry) []string {
	pins := ""
	for _, entry := range entries {
		pins += "\t\trun = CustomShader" + st + "Pin" + touchToken(entry.Component.ID) + "\n"
	}
	template := `[CommandList{{S}}Cursor]
${{V}}_screen_w = 1
${{V}}_screen_h = 1
if window_width > 0 && window_height > 0 && window_width <= 8192 && window_height <= 8192
	${{V}}_screen_w = window_width
	${{V}}_screen_h = window_height
elif rt_width > 0 && rt_height > 0 && rt_width <= 8192 && rt_height <= 8192
	${{V}}_screen_w = rt_width
	${{V}}_screen_h = rt_height
elif res_width > 0 && res_height > 0 && res_width <= 8192 && res_height <= 8192
	${{V}}_screen_w = res_width
	${{V}}_screen_h = res_height
endif
if cursor_x > 0 && cursor_y > 0 && cursor_x < 1 && cursor_y < 1
	${{V}}_cursor_x = cursor_x * ${{V}}_screen_w
	${{V}}_cursor_y = (1.0 - cursor_y) * ${{V}}_screen_h
elif cursor_window_x > 0 && cursor_window_y > 0 && cursor_window_x < 1 && cursor_window_y < 1
	${{V}}_cursor_x = cursor_window_x * ${{V}}_screen_w
	${{V}}_cursor_y = (1.0 - cursor_window_y) * ${{V}}_screen_h
elif cursor_screen_x >= 0 && cursor_screen_y >= 0 && cursor_screen_x <= ${{V}}_screen_w && cursor_screen_y <= ${{V}}_screen_h
	${{V}}_cursor_x = cursor_screen_x
	${{V}}_cursor_y = ${{V}}_screen_h - cursor_screen_y
else
	${{V}}_cursor_x = -1
	${{V}}_cursor_y = -1
endif
x24 = ${{V}}_cursor_x
y24 = ${{V}}_cursor_y
z24 = ${{V}}_screen_w
w24 = ${{V}}_screen_h

[CommandList{{S}}Present]
if ${{V}}_prev_time == 0
	${{V}}_delta_time = 0.0166667
else
	${{V}}_delta_time = (time - ${{V}}_prev_time) * 60.0
	if ${{V}}_delta_time > 0.100
		${{V}}_delta_time = 0.100
	elif ${{V}}_delta_time < 0.001
		${{V}}_delta_time = 0.001
	endif
endif
${{V}}_prev_time = time

${{V}}_mouse_down = 0
${{V}}_poke_sign = 0
if ${{V}}_modifier_down == 1
	if (${{V}}_lmb_down == 1 && ${{V}}_rmb_down == 1) || ${{V}}_x_down == 1
		${{V}}_mouse_down = 1
	endif
endif
if ${{V}}_mouse_down == 1
	${{V}}_combo_active = 1
endif

if ${{V}}_modifier_down == 1 && ${{V}}_combo_active == 0
	if ${{V}}_lmb_prev == 1 && ${{V}}_lmb_down == 0
		${{V}}_poke_sign = -1
		${{V}}_poke_mult = time - ${{V}}_lmb_press_time
	elif ${{V}}_rmb_prev == 1 && ${{V}}_rmb_down == 0
		${{V}}_poke_sign = 1
		${{V}}_poke_mult = time - ${{V}}_rmb_press_time
	endif
endif
if ${{V}}_poke_sign != 0
	if ${{V}}_poke_mult < 0.25
		${{V}}_poke_mult = 0.25
	elif ${{V}}_poke_mult > 1.0
		${{V}}_poke_mult = 1.0
	endif
else
	${{V}}_poke_mult = 1.0
endif

${{V}}_charge_sign = 0
if ${{V}}_modifier_down == 1 && ${{V}}_combo_active == 0
	if ${{V}}_lmb_down == 1 && ${{V}}_rmb_down == 0
		${{V}}_charge_sign = -1
	elif ${{V}}_rmb_down == 1 && ${{V}}_lmb_down == 0
		${{V}}_charge_sign = 1
	endif
endif
if ${{V}}_lmb_down == 1 && ${{V}}_lmb_prev == 0
	${{V}}_lmb_press_time = time
endif
if ${{V}}_rmb_down == 1 && ${{V}}_rmb_prev == 0
	${{V}}_rmb_press_time = time
endif
${{V}}_lmb_prev = ${{V}}_lmb_down
${{V}}_rmb_prev = ${{V}}_rmb_down
if ${{V}}_lmb_down == 0 && ${{V}}_rmb_down == 0 && ${{V}}_x_down == 0
	${{V}}_combo_active = 0
endif

if ${{V}}_active == 1 && ${{V}}_mode == 1
	if ${{V}}_initialized == 0
		run = CustomShader{{S}}PinDetected
{{PINS}}		${{V}}_initialized = 1
	else
		run = CustomShader{{S}}PinDetected
{{PINS}}		run = CustomShader{{S}}UpdateScreenState
	endif
	${{V}}_detect_allowed = 1
else
	${{V}}_detect_allowed = 0
endif
run = CommandList{{S}}Cursor
`
	template = strings.ReplaceAll(template, "{{PINS}}", pins)
	return strings.Split(touchTemplate(template, varPrefix, st, "", ""), "\n")
}

func buildTouchSharedShaders(varPrefix, st string, entries []touchInteractiveEntry, overrides touchZoneOverrides, runtime string) []string {
	lines := strings.Split(touchTemplate(`[CustomShader{{S}}PinDetected]
cs = {{R}}/rzm_pin_detected.hlsl
x24 = ${{V}}_cursor_x
y24 = ${{V}}_cursor_y
z24 = ${{V}}_screen_w
w24 = ${{V}}_screen_h
cs-u0 = Resource{{S}}DetectID
cs-u1 = Resource{{S}}PinnedID
cs-u2 = Resource{{S}}PinnedInfo
dispatch = 1, 1, 1
post cs-u0 = null
post cs-u1 = null
post cs-u2 = null
`, varPrefix, st, runtime, ""), "\n")
	for _, entry := range entries {
		id := touchToken(entry.Component.ID)
		lines = append(lines, strings.Split(touchTemplate(`[CustomShader{{S}}Pin{{ID}}]
cs = {{R}}/rzm_pin_detected.hlsl
cs-u0 = Resource{{S}}ComponentDetect{{ID}}
cs-u1 = Resource{{S}}PinnedComponentID{{ID}}
cs-u2 = Resource{{S}}PinnedComponentInfo{{ID}}
dispatch = 1, 1, 1
post cs-u0 = null
post cs-u1 = null
post cs-u2 = null
`, varPrefix, st, runtime, id), "\n")...)
	}
	screen := strings.Split(touchTemplate(`[CustomShader{{S}}UpdateScreenState]
local $cursor_x_past
local $cursor_y_past
local $was_mouse_down
if ${{V}}_mouse_down == 1
	if $was_mouse_down == 0
		$cursor_x_past = ${{V}}_cursor_x
		$cursor_y_past = ${{V}}_cursor_y
	endif
	$was_mouse_down = 1
	w67 = 1
else
	$was_mouse_down = 0
	$cursor_x_past = 0
	$cursor_y_past = 0
	w67 = 0
endif
cs = {{R}}/rzm_jiggle_screen_state.hlsl
x67 = $cursor_x_past
y67 = $cursor_y_past`, varPrefix, st, runtime, ""), "\n")
	screen = append(screen, touchBasePhysicsLines()...)
	screen = append(screen, strings.Split(touchTemplate(`x69 = ${{V}}_cursor_x
y69 = ${{V}}_cursor_y
z69 = ${{V}}_screen_w
w69 = ${{V}}_screen_h
x72 = 0
y72 = 1.0
z72 = 0.333333
w72 = 0.333333
x73 = 1.0
y73 = 1.0
z73 = 1.0
x76 = ${{V}}_delta_time
y76 = 3.0
z76 = 3.0`, varPrefix, st, runtime, ""), "\n")...)
	screen = append(screen, touchZoneOverrideLines(overrides)...)
	screen = append(screen, strings.Split(touchTemplate(`x84 = ${{V}}_poke_sign
y84 = ${{V}}_poke_mult
z84 = 8.0
w84 = ${{V}}_charge_sign
x99 = 1
y99 = 1
z99 = 1
w99 = 1
x100 = 1
y100 = 1
z100 = 1
w100 = 1
x112 = 1
y112 = 1
z112 = 1
w112 = 1
cs-t67 = Resource{{S}}PinnedInfo
cs-u0 = Resource{{S}}ScreenState
cs-u1 = Resource{{S}}PathProgress
dispatch = 1, 1, 1
post cs-t67 = null
post cs-u0 = null
post cs-u1 = null
`, varPrefix, st, runtime, ""), "\n")...)
	return append(lines, screen...)
}

func buildTouchComponentShaders(varPrefix, st string, entry touchInteractiveEntry, overrides touchZoneOverrides, runtime string) []string {
	id := touchToken(entry.Component.ID)
	maskBase := touchMaskResourceToken(entry.Asset.AssetPrefix)
	params := touchParamsResourceToken(entry.Asset.AssetPrefix)
	first, count := 0, max(entry.Component.IndexCount, 1)
	for _, objectMap := range entry.Component.ObjectMaps {
		if objectMap.Label == "nude" {
			first, count = objectMap.FirstIndex, objectMap.IndexCount
			break
		}
	}
	if len(entry.Component.ObjectMaps) > 0 && first == 0 {
		first, count = entry.Component.ObjectMaps[0].FirstIndex, entry.Component.ObjectMaps[0].IndexCount
	} else if len(entry.Component.ObjectMaps) == 0 && len(entry.Component.DrawRanges) > 0 {
		first, count = entry.Component.DrawRanges[0].FirstIndex, entry.Component.DrawRanges[0].IndexCount
	}
	samples := touchBakeOffsets(first, count, touchBakeSamples)
	lines := []string{"[CustomShader" + st + "Bake" + id + "]", "run = BuiltInCommandListUnbindAllRenderTargets", "clear = Resource" + st + "BakeRT 0.0"}
	for index := range samples {
		lines = append(lines, "run = CustomShader"+st+"Bake"+id+strconv.Itoa(index))
	}
	lines = append(lines, "")
	for index, sample := range samples {
		lines = append(lines, "[CustomShader"+st+"Bake"+id+strconv.Itoa(index)+"]", "gs = "+runtime+"/rzm_gs_probe.hlsl", "gs-t1 = Resource"+derefString(entry.Component.IndexResourceName), "ps = "+runtime+"/rzm_gs_probe.hlsl", "topology = point_list", "o0 = set_viewport no_view_cache Resource"+st+"BakeRT", "x26 = "+strconv.Itoa(index), "y26 = "+strconv.Itoa(sample), fmt.Sprintf("drawindexed = 1, %d, 0", sample), "")
	}
	lines = append(lines, "[CustomShader"+st+"Detect"+id+"]", "cs = "+runtime+"/rzm_object_detect.hlsl", fmt.Sprintf("x28 = %d", entry.Draft.ObjectID), "cs-t0 = vb0", "cs-t1 = ib")
	if len(entry.Asset.ObjectMapPaths) >= 2 && entry.Component.Kind == "body" {
		clothed, nude := entry.Asset.ObjectMapPaths[0], entry.Asset.ObjectMapPaths[1]
		for _, objectMap := range entry.Asset.ObjectMapPaths {
			if strings.Contains(strings.ToLower(objectMap.Label), "clothed") {
				clothed = objectMap
			}
			if strings.Contains(strings.ToLower(objectMap.Label), "nude") {
				nude = objectMap
			}
		}
		lines = append(lines, "if $body <= 1", "\tcs-t2 = Resource"+st+touchObjectMapResourceToken(entry.Asset.AssetPrefix, clothed.Label), "else", "\tcs-t2 = Resource"+st+touchObjectMapResourceToken(entry.Asset.AssetPrefix, nude.Label), "endif")
	} else {
		label := "main"
		if len(entry.Asset.ObjectMapPaths) > 0 {
			label = entry.Asset.ObjectMapPaths[0].Label
		}
		lines = append(lines, "cs-t2 = Resource"+st+touchObjectMapResourceToken(entry.Asset.AssetPrefix, label))
	}
	lines = append(lines,
		"cs-t3 = Resource"+st+"BakeRT", "cs-t4 = Resource"+st+maskBase+"0", "cs-t5 = Resource"+st+maskBase+"1", "cs-t7 = Resource"+st+maskBase+"2", "cs-t6 = Resource"+st+"ViewportAPI", "cs-u0 = Resource"+st+"DetectID", "cs-u1 = Resource"+st+"ComponentDetect"+id, "cs-u2 = Resource"+st+"DebugDetect"+id,
		"x24 = $"+varPrefix+"_cursor_x", "y24 = $"+varPrefix+"_cursor_y", "z24 = $"+varPrefix+"_screen_w", "w24 = $"+varPrefix+"_screen_h", "x25 = $"+varPrefix+"_mouse_down", "x26 = 48.0", "w26 = 8.0", "x27 = $"+varPrefix+"_cursor_x", "y27 = $"+varPrefix+"_cursor_y", "z27 = $"+varPrefix+"_screen_w", "w27 = $"+varPrefix+"_screen_h", "x85 = 0", "y85 = 0", "z85 = 1", "w85 = 1", "x86 = 1", "x74 = 0", "dispatch = 1, 1, 1", "post cs-u0 = null", "post cs-u1 = null", "post cs-u2 = null", "",
		"[CustomShader"+st+"Jiggle"+id+"]", "local $cursor_x_past", "local $cursor_y_past", "local $was_mouse_down", "if $"+varPrefix+"_mouse_down == 1", "\tif $was_mouse_down == 0", "\t\t$cursor_x_past = $"+varPrefix+"_cursor_x", "\t\t$cursor_y_past = $"+varPrefix+"_cursor_y", "\tendif", "\t$was_mouse_down = 1", "\tw67 = 1", "else", "\t$was_mouse_down = 0", "\t$cursor_x_past = 0", "\t$cursor_y_past = 0", "\tw67 = 0", "endif", "cs = "+runtime+"/rzm_jiggle_interaction.hlsl", "x67 = $cursor_x_past", "y67 = $cursor_y_past")
	lines = append(lines, touchBasePhysicsLines()...)
	lines = append(lines, "x69 = $"+varPrefix+"_cursor_x", "y69 = $"+varPrefix+"_cursor_y", "z69 = $"+varPrefix+"_screen_w", "w69 = $"+varPrefix+"_screen_h", "x72 = 1", "y72 = 1.0", "z72 = 0.333333", "w72 = 0.333333", "x73 = 1.0", "y73 = 1.0", "x76 = $"+varPrefix+"_delta_time", "y76 = 3.0", "z76 = 3.0")
	lines = append(lines, touchZoneOverrideLines(overrides)...)
	lines = append(lines, "x99 = 1", "y99 = 1", "z99 = 1", "w99 = 1", "x100 = 1", "y100 = 1", "z100 = 1", "w100 = 1", "x112 = 1", "y112 = 1", "z112 = 1", "w112 = 1", "cs-t67 = Resource"+st+"PinnedComponentInfo"+id, "cs-t68 = Resource"+st+params, "cs-t65 = Resource"+st+maskBase+"0", "cs-t66 = Resource"+st+maskBase+"1", "cs-t69 = Resource"+st+maskBase+"2", "cs-t71 = Resource"+st+"ScreenState", "cs-t74 = Resource"+st+"PathProgress", "cs-u6 = Resource"+st+"JiggleState"+id, "Resource"+st+"TempVB"+id+" = vb0", "cs-t24 = vb0", "cs-u5 = copy Resource"+st+"TempVB"+id, fmt.Sprintf("dispatch = (%d + 255) // 256, 1, 1", entry.Component.VertexCount), "vb0 = null", "Resource"+st+"TempVB"+id+" = copy cs-u5", "cs-u5 = null", "post cs-u6 = null", "post cs-t71 = null", "")
	return lines
}

func buildTouchSharedResources(st string) []string {
	return strings.Split(touchTemplate(`[Resource{{S}}DetectID]
type = RWBuffer
format = R32G32B32A32_FLOAT
array = 15

[Resource{{S}}PinnedID]
type = RWBuffer
format = R32_FLOAT
array = 1

[Resource{{S}}PinnedInfo]
type = RWBuffer
format = R32G32B32A32_FLOAT
array = 15

[Resource{{S}}ScreenState]
type = RWBuffer
format = R32G32B32A32_FLOAT
array = 15

[Resource{{S}}PathProgress]
type = RWBuffer
format = R32_FLOAT
array = 12

[Resource{{S}}ViewportAPI]
type = RWBuffer
format = R32_FLOAT
array = 16

[Resource{{S}}BakeRT]
type = Texture2D
mode = mono
width = 8
height = 2
mips = 1
array = 1
msaa = 1
msaa_quality = 0
format = DXGI_FORMAT_R32G32B32A32_FLOAT
bind_flags = render_target shader_resource
`, "", st, "", ""), "\n")
}

func buildTouchComponentResources(st string, entry touchInteractiveEntry) []string {
	id := touchToken(entry.Component.ID)
	mask := touchMaskResourceToken(entry.Asset.AssetPrefix)
	params := touchParamsResourceToken(entry.Asset.AssetPrefix)
	lines := []string{}
	for _, objectMap := range entry.Asset.ObjectMapPaths {
		lines = append(lines, "[Resource"+st+touchObjectMapResourceToken(entry.Asset.AssetPrefix, objectMap.Label)+"]", "type = Buffer", "format = R32G32B32A32_FLOAT", "filename = "+objectMap.RelativePath, "")
	}
	for index, path := range entry.Asset.MaskPaths {
		lines = append(lines, "[Resource"+st+mask+strconv.Itoa(index)+"]", "type = Buffer", "format = R32G32B32A32_FLOAT", "filename = "+path, "")
	}
	lines = append(lines, "[Resource"+st+params+"]", "type = Buffer", "format = R32G32B32A32_FLOAT", "filename = "+entry.Asset.ParamsRelativePath, "", "[Resource"+st+"ComponentDetect"+id+"]", "type = RWBuffer", "format = R32G32B32A32_FLOAT", "array = 15", "", "[Resource"+st+"PinnedComponentID"+id+"]", "type = RWBuffer", "format = R32_FLOAT", "array = 1", "", "[Resource"+st+"PinnedComponentInfo"+id+"]", "type = RWBuffer", "format = R32G32B32A32_FLOAT", "array = 15", "", "[Resource"+st+"DebugDetect"+id+"]", "type = RWBuffer", "format = R32G32B32A32_FLOAT", "array = 23", "", "[Resource"+st+"JiggleState"+id+"]", "type = RWBuffer", "format = R32G32B32A32_FLOAT", "array = 10", "", "[Resource"+st+"TempVB"+id+"]", "type = RWBuffer", "")
	return lines
}

func buildTouchZoneOverrides(entries []touchInteractiveEntry) (touchZoneOverrides, error) {
	var out touchZoneOverrides
	for _, entry := range entries {
		for _, zone := range entry.Draft.Zones {
			if zone.Channel < 0 || zone.Channel >= touchZoneChannels {
				return out, contractError(fmt.Sprintf("Touch zone channel out of range: %d", zone.Channel))
			}
			params := resolveTouchJiggleParams(zone.Settings, entry.Draft.ObjectID)
			values := []struct {
				name   string
				target *[touchZoneChannels]float64
				value  float64
			}{{"radius", &out.Radius, params.Radius}, {"strength", &out.Strength, params.Strength}, {"falloff", &out.Falloff, params.Falloff}, {"maxOffset", &out.MaxOffset, params.MaxOffset}, {"damping", &out.Damping, params.GrabDamping / defaultTouchJiggleParams.GrabDamping}, {"spring", &out.Spring, params.GrabSpring / defaultTouchJiggleParams.GrabSpring}}
			for _, item := range values {
				current := item.target[zone.Channel]
				if current != 0 && mathAbs(current-item.value) > 1e-6 {
					return out, contractError(fmt.Sprintf("Touch zone channel %d has conflicting %s overrides", zone.Channel, item.name))
				}
				item.target[zone.Channel] = item.value
			}
		}
	}
	return out, nil
}
func touchBasePhysicsLines() []string {
	p := defaultTouchJiggleParams
	return []string{"x68 = " + formatTouchNumber(p.Radius), "y68 = " + formatTouchNumber(p.Strength), "z68 = " + formatTouchNumber(p.Falloff), "w68 = " + formatTouchNumber(p.DragScale), "x70 = " + formatTouchNumber(p.GrabDamping), "y70 = " + formatTouchNumber(p.GrabSpring), "z70 = " + formatTouchNumber(p.ReleaseDamping), "w70 = " + formatTouchNumber(p.ReleaseSpring), "x71 = " + formatTouchNumber(p.MaxOffset), "y71 = " + formatTouchNumber(p.ReleaseKick), "z71 = " + formatTouchNumber(p.MouseYDirection), "w71 = " + formatTouchNumber(p.TargetFollow)}
}
func touchZoneOverrideLines(o touchZoneOverrides) []string {
	lines := []string{}
	for _, entry := range []struct {
		a, b, c int
		values  [touchZoneChannels]float64
	}{{77, 78, 103, o.Radius}, {79, 80, 106, o.Strength}, {81, 82, 109, o.MaxOffset}, {101, 102, 116, o.Falloff}, {122, 123, 124, o.Damping}, {125, 126, 127, o.Spring}} {
		slots := []int{entry.a, entry.b, entry.c}
		for group, slot := range slots {
			names := []string{"x", "y", "z", "w"}
			for i := range 4 {
				lines = append(lines, names[i]+strconv.Itoa(slot)+" = "+formatTouchNumber(entry.values[group*4+i]))
			}
		}
	}
	return lines
}
func formatTouchNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }
func touchBakeOffsets(first, count, samples int) []int {
	out := make([]int, samples)
	if count <= 1 {
		for i := range out {
			out[i] = first
		}
		return out
	}
	for i := range out {
		out[i] = first + (i*(count-1))/(samples-1)
	}
	return out
}
func touchComponentKindToken(prefix string) string {
	match := regexp.MustCompile(`(?i)(Body|Leg|Hair|Mesh)$`).FindStringSubmatch(prefix)
	if len(match) == 2 {
		return strings.ToUpper(match[1][:1]) + strings.ToLower(match[1][1:])
	}
	return regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(prefix, "")
}
func touchMaskResourceToken(prefix string) string { return touchComponentKindToken(prefix) + "Masks" }
func touchParamsResourceToken(prefix string) string {
	return touchComponentKindToken(prefix) + "Params"
}
func touchObjectMapResourceToken(prefix, label string) string {
	kind := touchComponentKindToken(prefix)
	if label == "main" || label == "skin" {
		return kind + "ObjectMap"
	}
	return kind + strings.ToUpper(label[:1]) + label[1:] + "ObjectMap"
}
func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
