//go:build windows

package platform

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rodrigocfd/windigo/co"
)

// keyChord is one key press parsed from a key spec. Modifiers are pressed
// before the key and released in reverse order.
type keyChord struct {
	spec      string
	modifiers []co.VK
	key       co.VK
}

// keyModifierTokens are pressed down before the key and released afterwards.
var keyModifierTokens = map[string]co.VK{
	"ctrl":    co.VK_CONTROL,
	"control": co.VK_CONTROL,
	"alt":     co.VK_MENU,
	"shift":   co.VK_SHIFT,
	"win":     co.VK_LWIN,
}

// keyCancelTokens only document that a modifier must not be held. Injection
// presses what the spec asks for, so they carry no virtual key.
var keyCancelTokens = map[string]struct{}{
	"no_ctrl":  {},
	"no_alt":   {},
	"no_shift": {},
	"no_win":   {},
}

// extendedKeys reach the target as extended keys: bit 24 in the posted lParam
// and the extended flag on the injected scan code.
var extendedKeys = map[co.VK]struct{}{
	co.VK_RMENU: {}, co.VK_RCONTROL: {}, co.VK_LWIN: {}, co.VK_RWIN: {},
	co.VK_INSERT: {}, co.VK_DELETE: {}, co.VK_HOME: {}, co.VK_END: {},
	co.VK_PRIOR: {}, co.VK_NEXT: {}, co.VK_LEFT: {}, co.VK_UP: {},
	co.VK_RIGHT: {}, co.VK_DOWN: {}, co.VK_NUMLOCK: {}, co.VK_DIVIDE: {},
	co.VK_SNAPSHOT: {}, co.VK_APPS: {},
}

// keyTokens maps the tokens stored for 3dmigoto key bindings to virtual-key
// codes. Keep it aligned with frontend/src/components/mod/utils.ts (the
// toggle-key editor) and frontend/src/shared/key-formatter.ts.
var keyTokens = buildKeyTokens()

func buildKeyTokens() map[string]co.VK {
	tokens := map[string]co.VK{
		"vk_back":     co.VK_BACK,
		"vk_tab":      co.VK_TAB,
		"vk_return":   co.VK_RETURN,
		"vk_escape":   co.VK_ESCAPE,
		"vk_space":    co.VK_SPACE,
		"vk_prior":    co.VK_PRIOR,
		"vk_next":     co.VK_NEXT,
		"vk_end":      co.VK_END,
		"vk_home":     co.VK_HOME,
		"vk_left":     co.VK_LEFT,
		"vk_up":       co.VK_UP,
		"vk_right":    co.VK_RIGHT,
		"vk_down":     co.VK_DOWN,
		"vk_snapshot": co.VK_SNAPSHOT,
		"vk_insert":   co.VK_INSERT,
		"vk_delete":   co.VK_DELETE,
		"vk_pause":    co.VK_PAUSE,
		"vk_apps":     co.VK_APPS,
		"vk_multiply": co.VK_MULTIPLY,
		"vk_add":      co.VK_ADD,
		"vk_subtract": co.VK_SUBTRACT,
		"vk_decimal":  co.VK_DECIMAL,
		"vk_divide":   co.VK_DIVIDE,
		"vk_lshift":   co.VK_LSHIFT,
		"vk_rshift":   co.VK_RSHIFT,
		"vk_lcontrol": co.VK_LCONTROL,
		"vk_rcontrol": co.VK_RCONTROL,
		"vk_lmenu":    co.VK_LMENU,
		"vk_rmenu":    co.VK_RMENU,
		"vk_lwin":     co.VK_LWIN,
		"vk_rwin":     co.VK_RWIN,

		"vk_oem_1":      co.VK_OEM_1,
		"vk_oem_2":      co.VK_OEM_2,
		"vk_oem_3":      co.VK_OEM_3,
		"vk_oem_4":      co.VK_OEM_4,
		"vk_oem_5":      co.VK_OEM_5,
		"vk_oem_6":      co.VK_OEM_6,
		"vk_oem_7":      co.VK_OEM_7,
		"vk_oem_minus":  co.VK_OEM_MINUS,
		"vk_oem_plus":   co.VK_OEM_PLUS,
		"vk_oem_comma":  co.VK_OEM_COMMA,
		"vk_oem_period": co.VK_OEM_PERIOD,

		// Character keys as the toggle-key editor records them.
		"[":  co.VK_OEM_4,
		"]":  co.VK_OEM_6,
		"\\": co.VK_OEM_5,
		";":  co.VK_OEM_1,
		"'":  co.VK_OEM_7,
		",":  co.VK_OEM_COMMA,
		".":  co.VK_OEM_PERIOD,
		"/":  co.VK_OEM_2,
		"`":  co.VK_OEM_3,
		"-":  co.VK_OEM_MINUS,
		"_":  co.VK_OEM_MINUS,
		"=":  co.VK_OEM_PLUS,
		"+":  co.VK_OEM_PLUS,
	}

	// Character keys as the toggle-key editor records them, plus the VK_ names
	// that 3dmigoto INI files use for the same keys.
	for index := range 26 {
		letter := string(rune('a' + index))
		tokens[letter] = co.VK_A + co.VK(index)
		tokens["vk_"+letter] = co.VK_A + co.VK(index)
	}
	for index := range 10 {
		digit := string(rune('0' + index))
		tokens[digit] = co.VK_0 + co.VK(index)
		tokens["vk_"+digit] = co.VK_0 + co.VK(index)
		tokens[fmt.Sprintf("vk_numpad%d", index)] = co.VK_NUMPAD0 + co.VK(index)
	}
	// f1-f24 are the names callers type. vk_fN remains the form the toggle-key editor stores.
	for index := range 24 {
		tokens[fmt.Sprintf("vk_f%d", index+1)] = co.VK_F1 + co.VK(index)
		tokens[fmt.Sprintf("f%d", index+1)] = co.VK_F1 + co.VK(index)
	}
	return tokens
}

func parseKeyChord(spec string) (keyChord, error) {
	tokens := strings.Fields(strings.ToLower(spec))
	if len(tokens) == 0 {
		return keyChord{}, fmt.Errorf("%w: empty key", ErrInputKeyInvalid)
	}

	chord := keyChord{spec: strings.TrimSpace(spec)}
	for _, token := range tokens {
		if _, ok := keyCancelTokens[token]; ok {
			continue
		}
		if modifier, ok := keyModifierTokens[token]; ok {
			if !slices.Contains(chord.modifiers, modifier) {
				chord.modifiers = append(chord.modifiers, modifier)
			}
			continue
		}
		if strings.HasPrefix(token, "xb_") {
			return keyChord{}, fmt.Errorf("%w: %s is a gamepad key", ErrInputKeyUnsupported, token)
		}
		key, ok := keyTokens[token]
		if !ok {
			return keyChord{}, fmt.Errorf("%w: unknown key %q", ErrInputKeyInvalid, token)
		}
		if chord.key != 0 {
			return keyChord{}, fmt.Errorf("%w: %s names more than one key", ErrInputKeyInvalid, chord.spec)
		}
		chord.key = key
	}
	if chord.key == 0 {
		return keyChord{}, fmt.Errorf("%w: %s names no key", ErrInputKeyInvalid, chord.spec)
	}
	return chord, nil
}

// keyScan reduces a MapVirtualKey result to the scan code and the extended
// flag. MapVirtualKey reports an extended key either through the virtual key
// itself or through a 0xE0 high byte in the result.
func keyScan(vk co.VK, raw uint32) (uint16, bool) {
	_, extended := extendedKeys[vk]
	return uint16(raw & 0xFF), extended || raw>>8 == 0xE0
}

// keyLParam builds a WM_KEYDOWN/WM_KEYUP lParam: repeat count 1, the scan code
// in bits 16-23, the extended flag in bit 24, and the previous-key-state and
// transition bits for a key-up.
func keyLParam(scan uint16, extended, keyUp bool) uintptr {
	lparam := uintptr(1) | uintptr(scan)<<16
	if extended {
		lparam |= 1 << 24
	}
	if keyUp {
		lparam |= 1<<30 | 1<<31
	}
	return lparam
}
