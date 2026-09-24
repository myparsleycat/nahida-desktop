//go:build windows

package platform

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/rodrigocfd/windigo/co"
)

func TestParseKeyChordAcceptsToggleKeyNotation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec string
		keys []co.VK
	}{
		{name: "single key", spec: "vk_f10", keys: []co.VK{co.VK_F10}},
		{name: "uppercase", spec: "VK_F10", keys: []co.VK{co.VK_F10}},
		{name: "bare function key", spec: "f12", keys: []co.VK{co.VK_F12}},
		{name: "bare function key uppercase", spec: "F12", keys: []co.VK{co.VK_F12}},
		{name: "modified bare function key", spec: "ctrl f12", keys: []co.VK{co.VK_CONTROL, co.VK_F12}},
		{name: "modifier after the key", spec: "f12 ctrl", keys: []co.VK{co.VK_CONTROL, co.VK_F12}},
		{name: "two modifiers", spec: "ctrl alt vk_f5", keys: []co.VK{co.VK_CONTROL, co.VK_MENU, co.VK_F5}},
		{name: "repeated modifier", spec: "ctrl ctrl vk_a", keys: []co.VK{co.VK_CONTROL, co.VK_A}},
		{name: "negated modifiers are not pressed", spec: "no_ctrl no_alt vk_f1", keys: []co.VK{co.VK_F1}},
		{name: "win key", spec: "win vk_space", keys: []co.VK{co.VK_LWIN, co.VK_SPACE}},
		{name: "letter", spec: "a", keys: []co.VK{co.VK_A}},
		{name: "digit", spec: "7", keys: []co.VK{co.VK_7}},
		{name: "ini letter", spec: "VK_A", keys: []co.VK{co.VK_A}},
		{name: "ini digit", spec: "vk_7", keys: []co.VK{co.VK_7}},
		{name: "backslash", spec: `\`, keys: []co.VK{co.VK_OEM_5}},
		{name: "numpad key", spec: "vk_numpad7", keys: []co.VK{co.VK_NUMPAD7}},
		{name: "oem name", spec: "vk_oem_plus", keys: []co.VK{co.VK_OEM_PLUS}},
		{name: "extended key", spec: "vk_left", keys: []co.VK{co.VK_LEFT}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			chord, err := parseKeyChord(testCase.spec)
			if err != nil {
				t.Fatalf("parseKeyChord(%q) = %v", testCase.spec, err)
			}
			if !slices.Equal(chord.keys, testCase.keys) {
				t.Fatalf("parseKeyChord(%q).keys = %v, want %v", testCase.spec, chord.keys, testCase.keys)
			}
		})
	}
}

func TestParseKeyChordAcceptsMultipleNonModifierKeys(t *testing.T) {
	t.Parallel()

	if _, err := parseKeyChord("vk_decimal vk_numpad2"); err != nil {
		t.Fatalf("parseKeyChord(ZZMI compute-shader binding) = %v", err)
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
		{spec: "vk_unknown", want: ErrInputKeyInvalid},
		{spec: "Ctrl+F12", want: ErrInputKeyInvalid},
		{spec: "xb_a", want: ErrInputKeyUnsupported},
		{spec: "xb_left_trigger", want: ErrInputKeyUnsupported},
	}

	for _, testCase := range cases {
		if _, err := parseKeyChord(testCase.spec); !errors.Is(err, testCase.want) {
			t.Fatalf("parseKeyChord(%q) = %v, want %v", testCase.spec, err, testCase.want)
		}
	}
}

func TestKeyNotationCitesOnlyAcceptedTokens(t *testing.T) {
	t.Parallel()

	notation := KeyNotation()
	for _, phrase := range []string{`"ctrl f12"`, "f1-f24", "xb_", "vk_return (not enter)"} {
		if !strings.Contains(notation, phrase) {
			t.Fatalf("KeyNotation() missing %q", phrase)
		}
	}
	if strings.Contains(notation, "=vk_") {
		t.Fatal("KeyNotation() uses an assignment form a caller can copy as a key")
	}
	for _, token := range regexp.MustCompile(`vk_[a-z0-9]+`).FindAllString(notation, -1) {
		if _, ok := keyTokens[token]; !ok {
			t.Errorf("KeyNotation cites unknown token %q", token)
		}
		if _, err := parseKeyChord(token); err != nil {
			t.Errorf("parseKeyChord(%q) = %v", token, err)
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
		for _, token := range []string{
			fmt.Sprintf("vk_f%d", index),
			fmt.Sprintf("f%d", index),
		} {
			if _, ok := keyTokens[token]; !ok {
				t.Errorf("keyTokens is missing %q", token)
			}
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
