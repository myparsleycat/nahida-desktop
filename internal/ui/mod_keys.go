package ui

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
)

var keyLabels = map[string]string{
	"VK_CONTROL": "Ctrl", "CONTROL": "Ctrl", "VK_LCONTROL": "Ctrl", "VK_RCONTROL": "Ctrl",
	"VK_MENU": "Alt", "ALT": "Alt", "VK_LMENU": "Alt", "VK_RMENU": "Alt",
	"VK_SHIFT": "Shift", "SHIFT": "Shift", "VK_LSHIFT": "Shift", "VK_RSHIFT": "Shift",
	"VK_LWIN": "Win", "VK_RWIN": "Win", "VK_APPS": "Menu",
	"VK_UP": "↑", "UP": "↑", "VK_DOWN": "↓", "DOWN": "↓", "VK_LEFT": "←", "LEFT": "←", "VK_RIGHT": "→", "RIGHT": "→",
	"VK_HOME": "Home", "VK_END": "End", "VK_PRIOR": "PgUp", "VK_NEXT": "PgDn",
	"VK_RETURN": "Enter", "ENTER": "Enter", "VK_BACK": "Backspace", "VK_TAB": "Tab", "VK_SPACE": "Space",
	"VK_ESCAPE": "Esc", "VK_DELETE": "Del", "VK_INSERT": "Ins", "VK_SNAPSHOT": "PrtSc", "VK_PAUSE": "Pause",
	"VK_OEM_3": "`", "VK_OEM_MINUS": "-", "VK_OEM_PLUS": "+", "VK_OEM_COMMA": ",", "VK_OEM_PERIOD": ".",
	"VK_OEM_1": ";", "VK_OEM_2": "/", "VK_OEM_4": "[", "VK_OEM_5": "\\", "VK_OEM_6": "]", "VK_OEM_7": "'",
	"XB_LEFT_TRIGGER": "LT", "XB_RIGHT_TRIGGER": "RT", "XB_LEFT_SHOULDER": "LB", "XB_RIGHT_SHOULDER": "RB",
	"XB_LEFT_THUMB": "LS", "XB_RIGHT_THUMB": "RS", "XB_DPAD_UP": "D-Pad ↑", "XB_DPAD_DOWN": "D-Pad ↓",
	"XB_DPAD_LEFT": "D-Pad ←", "XB_DPAD_RIGHT": "D-Pad →", "XB_A": "A", "XB_B": "B", "XB_X": "X", "XB_Y": "Y",
	"XB_START": "Start", "XB_BACK": "Back", "XB_GUIDE": "Guide",
}

var functionKey = regexp.MustCompile(`^F\d+$`)

// keyLabel names one 3DMigoto key token for display, as the React key formatter did. Negated modifiers such
// as no_ctrl show nothing.
func keyLabel(key string) string {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(strings.ToLower(key), "no_") {
		return ""
	}
	upper := strings.ToUpper(key)
	if label, ok := keyLabels[upper]; ok {
		return label
	}
	if stripped, ok := strings.CutPrefix(upper, "VK_"); ok {
		if len(stripped) == 1 || functionKey.MatchString(stripped) {
			return stripped
		}
		if strings.HasPrefix(stripped, "NUMPAD") {
			return strings.Replace(stripped, "NUMPAD", "Num", 1)
		}
	}
	if stripped, ok := strings.CutPrefix(upper, "XB_"); ok {
		return stripped
	}
	return strings.ToUpper(key[:1]) + key[1:]
}

// keyLabels splits a key binding into the labels of its keys.
func keyLabelsOf(binding string) []string {
	var labels []string
	for _, key := range strings.Split(binding, " ") {
		if label := keyLabel(key); label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

var recordedKeyNames = map[keyboard.Key]string{
	keyboard.KeyUp: "vk_up", keyboard.KeyDown: "vk_down", keyboard.KeyLeft: "vk_left", keyboard.KeyRight: "vk_right",
	keyboard.KeyEnter: "vk_return", keyboard.KeySpace: "vk_space", keyboard.KeyTab: "vk_tab",
	keyboard.KeyEscape: "vk_escape", keyboard.KeyBackspace: "vk_back", keyboard.KeyDelete: "vk_delete",
	keyboard.KeyInsert: "vk_insert", keyboard.KeyHome: "vk_home", keyboard.KeyEnd: "vk_end",
	keyboard.KeyPageUp: "vk_prior", keyboard.KeyPageDown: "vk_next", 0x13: "vk_pause", 0x2C: "vk_snapshot",
	0x5D: "vk_apps", 0x6A: "vk_multiply", 0x6B: "vk_add", 0x6D: "vk_subtract",
	0x6E: "vk_decimal", 0x6F: "vk_divide", 0xBA: ";", 0xBB: "=", 0xBC: ",", 0xBD: "_", 0xBE: ".", 0xBF: "/",
	0xC0: "`", 0xDB: "[", 0xDC: "\\", 0xDD: "]", 0xDE: "'",
}

// keyRecording is a 3DMigoto key binding being recorded.
type keyRecording struct {
	value     string
	otherKeys []string
	// beforeEnter is the binding a held Enter restores before saving, since its first press records vk_return.
	beforeEnter string
}

// keyRecorder records r's binding from the keys pressed over it, with a field below to edit the binding by hand.
// Holding Enter calls save.
func keyRecorder(r *keyRecording, label, manualKey string, save func()) *fairy.El {
	t := kit.T()
	keys := fairy.Row().Wrap().Gap(8).RowGap(8).Align(fairy.Center).Justify(fairy.JustifyCenter)
	for _, key := range keyLabelsOf(r.value) {
		keys.Child(fairy.Text(key).FontSize(14).PadXY(8, 4).Radius(4).Border(1, t.Border).Fill(t.Card))
	}
	if len(keyLabelsOf(r.value)) == 0 {
		keys.Child(fairy.Text(T("page.mod.key-recorder.prompt")).Color(t.MutedForeground))
	}
	// React's dashed border turns solid on focus, without changing colour.
	recorder := fairy.Col(keys).MinH(100).Pad(24).Center().Radius(6).Border(1, t.Border).BorderDashed().
		Fill(t.Muted.Alpha(0.3)).
		FocusFill(t.Foreground.Alpha(0.1)).FocusBorder(t.Border).Animate(motion.Fast).Focusable().AutoFocus().
		Cursor(fairy.CursorPointer).
		Label(label).OnKey(func(k keyboard.KeyEvent) bool {
		if k.Key == keyboard.KeyEnter && k.Mods == 0 {
			if k.Repeat {
				r.value = r.beforeEnter
				save()
				return true
			}
			r.beforeEnter = r.value
		}
		if binding := recordedKey(k, r.otherKeys); binding != "" {
			r.value = binding
		}
		return true
	})
	return fairy.Col(
		recorder,
		fairy.Col(
			fairy.Text(strings.ToUpper(T("page.mod.key-recorder.manual_edit"))).FontSize(10).SemiBold().
				Color(t.MutedForeground),
			kit.TextField(r.value, func(value string) { r.value = value },
				kit.Placeholder(T("page.mod.key-recorder.placeholder")),
				kit.Label(T("page.mod.key-recorder.manual_edit"))).Key(manualKey).Font(monoFont),
		).Gap(4),
	).Gap(16)
}

var keyModifiers = []string{"ctrl", "alt", "shift", "win", "no_ctrl", "no_alt", "no_shift", "no_win"}

// recordedKey turns a key press into a 3DMigoto binding, as the React key recorder did: a modifier that
// another binding of the same key holds is written as no_<modifier> when it is not pressed, so the two do not
// both fire. Modifier keys alone and keys without a name return "".
func recordedKey(k keyboard.KeyEvent, otherKeys []string) string {
	var name string
	switch {
	case recordedKeyNames[k.Key] != "":
		name = recordedKeyNames[k.Key]
	case k.Key >= 'A' && k.Key <= 'Z':
		name = strings.ToLower(string(rune(k.Key)))
	case k.Key >= '0' && k.Key <= '9':
		name = string(rune(k.Key))
	case k.Key >= 0x60 && k.Key <= 0x69:
		name = "vk_numpad" + string(rune('0'+k.Key-0x60))
	case k.Key >= keyboard.KeyF1 && k.Key <= 0x87:
		name = "vk_f" + strconv.Itoa(int(k.Key-keyboard.KeyF1)+1)
	default:
		return ""
	}

	held := map[string]bool{"ctrl": k.Mods&keyboard.ModCtrl != 0, "alt": k.Mods&keyboard.ModAlt != 0,
		"shift": k.Mods&keyboard.ModShift != 0}
	used := map[string]bool{}
	for _, other := range otherKeys {
		if bindingBase(other) != name {
			continue
		}
		for _, part := range strings.Fields(strings.ToLower(other)) {
			used[part] = true
		}
	}
	var parts []string
	for _, modifier := range []string{"ctrl", "alt", "shift"} {
		switch {
		case held[modifier]:
			parts = append(parts, modifier)
		case used[modifier]:
			parts = append(parts, "no_"+modifier)
		}
	}
	return strings.Join(append(parts, name), " ")
}

// bindingBase is the key of a binding without its modifiers, with bare arrow names spelled as virtual keys.
func bindingBase(binding string) string {
	for _, part := range strings.Fields(strings.ToLower(binding)) {
		if slices.Contains(keyModifiers, part) {
			continue
		}
		switch part {
		case "up", "down", "left", "right":
			return "vk_" + part
		}
		return part
	}
	return ""
}
