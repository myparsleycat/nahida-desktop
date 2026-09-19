//go:build windows

package platform

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/rodrigocfd/windigo/co"
)

func TestParseKeyChordAcceptsToggleKeyNotation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		spec      string
		modifiers []co.VK
		key       co.VK
	}{
		{name: "single key", spec: "vk_f10", key: co.VK_F10},
		{name: "uppercase", spec: "VK_F10", key: co.VK_F10},
		{name: "two modifiers", spec: "ctrl alt vk_f5", modifiers: []co.VK{co.VK_CONTROL, co.VK_MENU}, key: co.VK_F5},
		{name: "repeated modifier", spec: "ctrl ctrl vk_a", modifiers: []co.VK{co.VK_CONTROL}, key: co.VK_A},
		{name: "negated modifiers are not pressed", spec: "no_ctrl no_alt vk_f1", key: co.VK_F1},
		{name: "win key", spec: "win vk_space", modifiers: []co.VK{co.VK_LWIN}, key: co.VK_SPACE},
		{name: "letter", spec: "a", key: co.VK_A},
		{name: "digit", spec: "7", key: co.VK_7},
		{name: "ini letter", spec: "VK_A", key: co.VK_A},
		{name: "ini digit", spec: "vk_7", key: co.VK_7},
		{name: "backslash", spec: `\`, key: co.VK_OEM_5},
		{name: "numpad key", spec: "vk_numpad7", key: co.VK_NUMPAD7},
		{name: "oem name", spec: "vk_oem_plus", key: co.VK_OEM_PLUS},
		{name: "extended key", spec: "vk_left", key: co.VK_LEFT},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			chord, err := parseKeyChord(testCase.spec)
			if err != nil {
				t.Fatalf("parseKeyChord(%q) = %v", testCase.spec, err)
			}
			if chord.key != testCase.key {
				t.Fatalf("parseKeyChord(%q).key = 0x%X, want 0x%X", testCase.spec, chord.key, testCase.key)
			}
			if !slices.Equal(chord.modifiers, testCase.modifiers) {
				t.Fatalf("parseKeyChord(%q).modifiers = %v, want %v",
					testCase.spec, chord.modifiers, testCase.modifiers)
			}
		})
	}
}

func TestParseKeyChordRejectsInvalidNotation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		spec string
		want error
	}{
		{spec: "", want: ErrInputKeyInvalid},
		{spec: "   ", want: ErrInputKeyInvalid},
		{spec: "ctrl", want: ErrInputKeyInvalid},
		{spec: "vk_f10 vk_f11", want: ErrInputKeyInvalid},
		{spec: "vk_unknown", want: ErrInputKeyInvalid},
		{spec: "xb_a", want: ErrInputKeyUnsupported},
		{spec: "xb_left_trigger", want: ErrInputKeyUnsupported},
	}

	for _, testCase := range cases {
		if _, err := parseKeyChord(testCase.spec); !errors.Is(err, testCase.want) {
			t.Fatalf("parseKeyChord(%q) = %v, want %v", testCase.spec, err, testCase.want)
		}
	}
}

func TestKeyTokensMatchToggleKeyNotation(t *testing.T) {
	t.Parallel()

	// Tokens the toggle-key editor can store, from
	// frontend/src/components/mod/utils.ts and frontend/src/shared/key-formatter.ts.
	for _, token := range []string{
		"vk_up", "vk_down", "vk_left", "vk_right", "vk_return", "vk_space", "vk_tab", "vk_escape",
		"vk_back", "vk_delete", "vk_insert", "vk_home", "vk_end", "vk_prior", "vk_next", "vk_pause",
		"vk_snapshot", "vk_apps", "vk_multiply", "vk_add", "vk_subtract", "vk_decimal", "vk_divide",
		"[", "]", `\`, ";", "'", ",", ".", "/", "`", "-", "=", "+",
	} {
		if _, ok := keyTokens[token]; !ok {
			t.Errorf("keyTokens is missing %q", token)
		}
	}

	for index := 1; index <= 24; index++ {
		if _, ok := keyTokens[fmt.Sprintf("vk_f%d", index)]; !ok {
			t.Errorf("keyTokens is missing vk_f%d", index)
		}
	}
	for index := range 10 {
		for _, token := range []string{
			fmt.Sprintf("vk_numpad%d", index),
			fmt.Sprintf("vk_%d", index),
			string(rune('0' + index)),
		} {
			if _, ok := keyTokens[token]; !ok {
				t.Errorf("keyTokens is missing %q", token)
			}
		}
	}
	for letter := 'a'; letter <= 'z'; letter++ {
		for _, token := range []string{string(letter), "vk_" + string(letter)} {
			if _, ok := keyTokens[token]; !ok {
				t.Errorf("keyTokens is missing %q", token)
			}
		}
	}

	for _, token := range []string{"ctrl", "control", "alt", "shift", "win"} {
		if _, ok := keyModifierTokens[token]; !ok {
			t.Errorf("keyModifierTokens is missing %q", token)
		}
	}
	for _, token := range []string{"no_ctrl", "no_alt", "no_shift", "no_win"} {
		if _, ok := keyCancelTokens[token]; !ok {
			t.Errorf("keyCancelTokens is missing %q", token)
		}
	}
}

func TestKeyScanReportsScanCodeAndExtendedFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		vk       co.VK
		raw      uint32
		scan     uint16
		extended bool
	}{
		{name: "function key", vk: co.VK_F10, raw: 0x44, scan: 0x44},
		{name: "letter", vk: co.VK_A, raw: 0x1E, scan: 0x1E},
		{name: "extended key", vk: co.VK_LEFT, raw: 0x4B, scan: 0x4B, extended: true},
		{name: "extended prefix", vk: co.VK_DIVIDE, raw: 0xE035, scan: 0x35, extended: true},
		{name: "extended as reported by MapVirtualKey", vk: co.VK_F10, raw: 0xE044, scan: 0x44, extended: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			scan, extended := keyScan(testCase.vk, testCase.raw)
			if scan != testCase.scan || extended != testCase.extended {
				t.Fatalf("keyScan(0x%X, 0x%X) = 0x%X, %t, want 0x%X, %t",
					testCase.vk, testCase.raw, scan, extended, testCase.scan, testCase.extended)
			}
		})
	}
}

func TestKeyLParamCarriesScanCodeAndStateBits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		scan     uint16
		extended bool
		keyUp    bool
		want     uintptr
	}{
		{name: "key down", scan: 0x44, want: 1 | 0x44<<16},
		{name: "key up", scan: 0x44, keyUp: true, want: 1 | 0x44<<16 | 1<<30 | 1<<31},
		{name: "extended key down", scan: 0x4B, extended: true, want: 1 | 0x4B<<16 | 1<<24},
		{name: "extended key up", scan: 0x4B, extended: true, keyUp: true, want: 1 | 0x4B<<16 | 1<<24 | 1<<30 | 1<<31},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := keyLParam(testCase.scan, testCase.extended, testCase.keyUp)
			if got != testCase.want {
				t.Fatalf("keyLParam(0x%X, %t, %t) = 0x%X, want 0x%X",
					testCase.scan, testCase.extended, testCase.keyUp, got, testCase.want)
			}
		})
	}
}
