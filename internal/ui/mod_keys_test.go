package ui

import (
	"testing"

	"github.com/myparsleycat/fairy/keyboard"
)

func TestRecordedKey(t *testing.T) {
	tests := []struct {
		name   string
		event  keyboard.KeyEvent
		others []string
		want   string
	}{
		{"letter", keyboard.KeyEvent{Key: 'K'}, nil, "k"},
		{
			"held modifiers",
			keyboard.KeyEvent{Key: 'K', Mods: keyboard.ModCtrl | keyboard.ModShift},
			nil,
			"ctrl shift k",
		},
		{"modifier of another binding", keyboard.KeyEvent{Key: 'K'}, []string{"ctrl k", "alt j"}, "no_ctrl k"},
		{
			"held modifier of another binding",
			keyboard.KeyEvent{Key: 'K', Mods: keyboard.ModCtrl},
			[]string{"ctrl k"},
			"ctrl k",
		},
		{"bare arrow binding", keyboard.KeyEvent{Key: keyboard.KeyUp}, []string{"shift up"}, "no_shift vk_up"},
		{"minus", keyboard.KeyEvent{Key: 0xBD}, nil, "_"},
		{"function key", keyboard.KeyEvent{Key: keyboard.KeyF5}, nil, "vk_f5"},
		{"modifier alone", keyboard.KeyEvent{Key: 0x11, Mods: keyboard.ModCtrl}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recordedKey(tt.event, tt.others); got != tt.want {
				t.Errorf("recordedKey() = %q, want %q", got, tt.want)
			}
		})
	}
}
