package efmioutline

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestClassifyDrawConditions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		body   string
		class  drawClass
		reason manualReason
	}{
		{
			name:  "material and outline block",
			body:  "if vs == 202 || vs == 203\n  if $part\n    drawindexed = 3, 0, 0\n  endif\nendif",
			class: classCandidate,
		},
		{
			name:  "pose and prepass block",
			body:  "if vs == 200 || vs == 201 || vs == 204\n  drawindexed = 3, 0, 0\nendif",
			class: classIgnore,
		},
		{
			name:  "no vs condition runs in both passes",
			body:  "if $part\n  drawindexed = 3, 0, 0\nendif",
			class: classIgnore,
		},
		{
			name:  "else of a pure vs chain",
			body:  "if vs == 201\n  ps-t0 = null\nelse\n  drawindexed = 3, 0, 0\nendif",
			class: classCandidate,
		},
		{
			name:  "else if is an elif",
			body:  "if vs == 200\n  ps-t0 = null\nelse if vs == 203\n  drawindexed = 3, 0, 0\nendif",
			class: classCandidate,
		},
		{
			name:   "else after a condition mixing vs and a variable",
			body:   "if vs == 201 && $x\n  ps-t0 = null\nelse\n  drawindexed = 3, 0, 0\nendif",
			class:  classManual,
			reason: reasonCondition,
		},
		{
			name:   "vs or a variable",
			body:   "if vs == 203 || $x\n  drawindexed = 3, 0, 0\nendif",
			class:  classManual,
			reason: reasonCondition,
		},
		{
			name:   "vs comparison the analyzer cannot evaluate",
			body:   "if vs >= 202\n  drawindexed = 3, 0, 0\nendif",
			class:  classManual,
			reason: reasonCondition,
		},
		{
			name:  "appended exclusion",
			body:  "if vs == 202 || vs == 203\n  if $part && vs != 203\n    drawindexed = 3, 0, 0\n  endif\nendif",
			class: classApplied,
		},
		{
			name:  "wrapped exclusion",
			body:  "if vs == 202 || vs == 203\n  if vs != 203\n    drawindexed = 3, 0, 0\n  endif\nendif",
			class: classApplied,
		},
		{
			name:  "author excludes both material and outline",
			body:  "if $part && vs != 202 && vs != 203\n  drawindexed = 3, 0, 0\nendif",
			class: classIgnore,
		},
		{
			name:   "endif without if",
			body:   "endif\nif vs == 203\n  drawindexed = 3, 0, 0\nendif",
			class:  classManual,
			reason: reasonStructure,
		},
		{
			name:   "if left open at the end of the section",
			body:   "if vs == 202 || vs == 203\n  drawindexed = 3, 0, 0",
			class:  classManual,
			reason: reasonStructure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			doc := parseDocument([]byte("[CommandList_Draw_Component0]\n" + test.body + "\n"))
			if len(doc.draws) != 1 {
				t.Fatalf("draws = %d, want 1", len(doc.draws))
			}
			draw := doc.draws[0]
			if draw.class != test.class || draw.reason != test.reason {
				t.Fatalf("class = %d (%q), want %d (%q)", draw.class, draw.reason, test.class, test.reason)
			}
		})
	}
}

// Mod files mix CRLF and LF and may start with a BOM; only the edited lines
// may change, and inserted lines take the terminator of the draw they wrap.
func TestApplyExclusionsPreservesBytes(t *testing.T) {
	t.Parallel()
	original := string(utf8BOM) + "[CommandList_Draw_Component0]\r\n" +
		"if vs == 202 || vs == 203\r\n" +
		"    if $a || $b\n" +
		"        drawindexed = 6, 0, 0\r\n" +
		"    endif\r\n" +
		"    if $c\r\n" +
		"        drawindexed = 3, 6, 0\n" +
		"    else\r\n" +
		"        drawindexed = 3, 9, 0\r\n" +
		"    endif\n" +
		"endif"
	want := string(utf8BOM) + "[CommandList_Draw_Component0]\r\n" +
		"if vs == 202 || vs == 203\r\n" +
		"    if ($a || $b) && vs != 203\n" +
		"        drawindexed = 6, 0, 0\r\n" +
		"    endif\r\n" +
		"    if $c\r\n" +
		"        if vs != 203\n" +
		"        drawindexed = 3, 6, 0\n" +
		"        endif\n" +
		"    else\r\n" +
		"        if vs != 203\r\n" +
		"        drawindexed = 3, 9, 0\r\n" +
		"        endif\r\n" +
		"    endif\n" +
		"endif"

	doc := parseDocument([]byte(original))
	parts, _ := doc.parts()
	if len(parts) != 3 {
		t.Fatalf("parts = %d, want 3", len(parts))
	}
	selected := map[string]bool{}
	for _, p := range parts {
		selected[p.id] = true
	}
	got, err := applyExclusions(doc, selected)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("output mismatch\n got: %q\nwant: %q", got, want)
	}
}

// zhuangFixture reproduces the structure of the Zhuang Fangyi Ink Cheongsam
// mod that was verified in game: the dp1 and dp2 skirt panels are drawn again
// from Component7, which must stay untouched.
func zhuangFixture() string {
	outline := func(ib string, draws ...string) string {
		body := "if vs == 202 || vs == 203\n    run = CustomShader_ExtractCB2\n    ib = ref " + ib + "\n    vb3 = vb0\n"
		for _, draw := range draws {
			body += "    if $" + draw + "\n        drawindexedinstanced = 7008, INSTANCE_COUNT, 46869, 0, FIRST_INSTANCE\n    endif\n"
		}
		return body + "endif\n"
	}
	pose := "if vs == 200 || vs == 201 || vs == 204 || vs == 205\n" +
		"    if vs == 205\n        run = CustomShader_ExtractCB3\n" +
		"    elif vs == 201 || vs == 204\n        run = CustomShader_ExtractCB2\n" +
		"    else\n        run = CustomShader_ExtractCB1\n    endif\n" +
		"    if $draw_component_0_0f9e1087_body1\n" +
		"        drawindexedinstanced = 102912, INSTANCE_COUNT, 53877, 0, FIRST_INSTANCE\n    endif\nendif\n"
	common := "if $draw_component_0_0f9e1087_tail\n" +
		"    drawindexedinstanced = 2067, INSTANCE_COUNT, 301137, 0, FIRST_INSTANCE\nendif\n"
	bodies := map[int]string{
		0: common + pose +
			outline("Resource_Component0_IB", "draw_component_0_0f9e1087_dp1", "draw_component_0_0f9e1087_dp1_001") +
			outline("Resource_Component4_IB", "draw_component_4_fe47dc61_qipao2"),
		4: outline("Resource_Component4_IB", "draw_component_4_fe47dc61_dp2", "draw_component_4_fe47dc61_dp2_001") +
			pose,
		7: outline("Resource_Component0_IB", "draw_component_0_0f9e1087_dp1", "draw_component_0_0f9e1087_dp1_001") +
			outline("Resource_Component4_IB", "draw_component_4_fe47dc61_dp2", "draw_component_4_fe47dc61_dp2_001"),
	}

	var out strings.Builder
	out.WriteString("[Constants]\nglobal $mod_enabled = 0\n\n")
	for _, component := range []int{0, 4, 7} {
		for _, suffix := range []string{"", "_lod"} {
			out.WriteString("[CommandList_Draw_Component" + strconv.Itoa(component) + suffix + "]\n")
			out.WriteString(bodies[component] + "post vs-cb2 = null\n\n")
		}
	}
	return strings.ReplaceAll(out.String(), "\n", "\r\n")
}

func TestZhuangSkirtPanelsChangeOnlyTheVerifiedLines(t *testing.T) {
	t.Parallel()
	original := []byte(zhuangFixture())
	doc := parseDocument(original)
	parts, _ := doc.parts()

	var got []string
	for _, p := range parts {
		got = append(got, p.section+" "+p.label)
	}
	want := []string{
		"CommandList_Draw_Component0 $draw_component_0_0f9e1087_dp1",
		"CommandList_Draw_Component0 $draw_component_0_0f9e1087_dp1_001",
		"CommandList_Draw_Component0 $draw_component_4_fe47dc61_qipao2",
		"CommandList_Draw_Component4 $draw_component_4_fe47dc61_dp2",
		"CommandList_Draw_Component4 $draw_component_4_fe47dc61_dp2_001",
		"CommandList_Draw_Component7 $draw_component_0_0f9e1087_dp1",
		"CommandList_Draw_Component7 $draw_component_0_0f9e1087_dp1_001",
		"CommandList_Draw_Component7 $draw_component_4_fe47dc61_dp2",
		"CommandList_Draw_Component7 $draw_component_4_fe47dc61_dp2_001",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("parts\n got: %q\nwant: %q", got, want)
	}

	selected := map[string]bool{}
	for _, p := range parts[:5] {
		if !strings.Contains(p.label, "qipao2") {
			selected[p.id] = true
		}
	}
	output, err := applyExclusions(doc, selected)
	if err != nil {
		t.Fatal(err)
	}

	before := bytes.Split(original, []byte("\r\n"))
	after := bytes.Split(output, []byte("\r\n"))
	if len(before) != len(after) {
		t.Fatalf("line count changed from %d to %d", len(before), len(after))
	}
	section := ""
	var changed []string
	for i := range before {
		if bytes.HasPrefix(before[i], []byte("[")) {
			section = string(before[i])
		}
		if !bytes.Equal(before[i], after[i]) {
			if !bytes.Equal(after[i], append(slices.Clone(before[i]), " && vs != 203"...)) {
				t.Fatalf("line %d: %q became %q", i+1, before[i], after[i])
			}
			changed = append(changed, section+" "+strings.TrimSpace(string(before[i])))
		}
	}
	wantChanged := []string{
		"[CommandList_Draw_Component0] if $draw_component_0_0f9e1087_dp1",
		"[CommandList_Draw_Component0] if $draw_component_0_0f9e1087_dp1_001",
		"[CommandList_Draw_Component0_lod] if $draw_component_0_0f9e1087_dp1",
		"[CommandList_Draw_Component0_lod] if $draw_component_0_0f9e1087_dp1_001",
		"[CommandList_Draw_Component4] if $draw_component_4_fe47dc61_dp2",
		"[CommandList_Draw_Component4] if $draw_component_4_fe47dc61_dp2_001",
		"[CommandList_Draw_Component4_lod] if $draw_component_4_fe47dc61_dp2",
		"[CommandList_Draw_Component4_lod] if $draw_component_4_fe47dc61_dp2_001",
	}
	if !slices.Equal(changed, wantChanged) {
		t.Fatalf("changed lines\n got: %q\nwant: %q", changed, wantChanged)
	}
}
