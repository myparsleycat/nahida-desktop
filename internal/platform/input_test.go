package platform

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestResolveKeyRequestTrimsKeysAndAppliesDefaults(t *testing.T) {
	t.Parallel()

	keys, options, err := resolveKeyRequest(KeyRequest{
		Target: WindowTarget{Process: "StarRail.exe"},
		Keys:   []string{" vk_f10 ", "", "ctrl alt vk_f5"},
	})
	if err != nil {
		t.Fatalf("resolveKeyRequest = %v", err)
	}
	if want := []string{"vk_f10", "ctrl alt vk_f5"}; !slices.Equal(keys, want) {
		t.Fatalf("keys = %v, want %v", keys, want)
	}
	if options.delivery != KeyDeliveryForeground {
		t.Fatalf("delivery = %q, want %q", options.delivery, KeyDeliveryForeground)
	}
	if options.hold != defaultKeyHold || options.interval != defaultKeyInterval {
		t.Fatalf("hold = %v, interval = %v, want %v and %v", options.hold, options.interval,
			defaultKeyHold, defaultKeyInterval)
	}
	if !options.restoreFocus {
		t.Fatal("restoreFocus = false, want true")
	}
}

func TestResolveKeyRequestHonorsOverrides(t *testing.T) {
	t.Parallel()

	noRestore := false
	_, options, err := resolveKeyRequest(KeyRequest{
		Target:       WindowTarget{PID: 42},
		Keys:         []string{"vk_f10"},
		Delivery:     KeyDeliveryMessage,
		HoldMs:       120,
		IntervalMs:   250,
		RestoreFocus: &noRestore,
	})
	if err != nil {
		t.Fatalf("resolveKeyRequest = %v", err)
	}
	if options.delivery != KeyDeliveryMessage {
		t.Fatalf("delivery = %q, want %q", options.delivery, KeyDeliveryMessage)
	}
	if options.hold != 120*time.Millisecond || options.interval != 250*time.Millisecond {
		t.Fatalf("hold = %v, interval = %v, want 120ms and 250ms", options.hold, options.interval)
	}
	if options.restoreFocus {
		t.Fatal("restoreFocus = true, want false")
	}
}

func TestResolveKeyRequestRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request KeyRequest
		want    error
	}{
		{
			name:    "no target",
			request: KeyRequest{Keys: []string{"vk_f10"}},
			want:    ErrInputTargetRequired,
		},
		{
			name:    "blank title",
			request: KeyRequest{Target: WindowTarget{Title: "  "}, Keys: []string{"vk_f10"}},
			want:    ErrInputTargetRequired,
		},
		{
			name:    "no keys",
			request: KeyRequest{Target: WindowTarget{Process: "game.exe"}},
			want:    ErrInputKeyInvalid,
		},
		{
			name: "more keys than the cap",
			request: KeyRequest{
				Target: WindowTarget{Process: "game.exe"},
				Keys: []string{
					"a", "b", "c", "d", "e", "f", "g", "h", "i",
					"j", "k", "l", "m", "n", "o", "p", "q",
				},
			},
			want: ErrInputKeyInvalid,
		},
		{
			name:    "unknown delivery",
			request: KeyRequest{Target: WindowTarget{PID: 1}, Keys: []string{"vk_f10"}, Delivery: "hook"},
			want:    ErrInputOptionInvalid,
		},
		{
			name:    "negative hold",
			request: KeyRequest{Target: WindowTarget{PID: 1}, Keys: []string{"vk_f10"}, HoldMs: -1},
			want:    ErrInputOptionInvalid,
		},
		{
			name:    "hold above the cap",
			request: KeyRequest{Target: WindowTarget{PID: 1}, Keys: []string{"vk_f10"}, HoldMs: 2001},
			want:    ErrInputOptionInvalid,
		},
		{
			name:    "interval above the cap",
			request: KeyRequest{Target: WindowTarget{PID: 1}, Keys: []string{"vk_f10"}, IntervalMs: 5001},
			want:    ErrInputOptionInvalid,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := resolveKeyRequest(testCase.request); !errors.Is(err, testCase.want) {
				t.Fatalf("resolveKeyRequest = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestClassifyInputErrorRoundTrip(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: no visible window matches title %q", ErrWindowNotFound, "Game")
	code, detail := ClassifyInputError(err)
	if code != ErrWindowNotFound.Error() {
		t.Fatalf("ClassifyInputError code = %q, want %q", code, ErrWindowNotFound)
	}
	if want := `no visible window matches title "Game"`; detail != want {
		t.Fatalf("ClassifyInputError detail = %q, want %q", detail, want)
	}

	sentinel, ok := InputErrorFromCode(code)
	if !ok {
		t.Fatalf("InputErrorFromCode(%q) = not found", code)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("InputErrorFromCode(%q) = %v, want it to match %v", code, sentinel, err)
	}
}

func TestClassifyInputErrorWithoutCode(t *testing.T) {
	t.Parallel()

	code, detail := ClassifyInputError(errors.New("boom"))
	if code != "" || detail != "boom" {
		t.Fatalf("ClassifyInputError = (%q, %q), want (\"\", \"boom\")", code, detail)
	}
	if emptyCode, emptyDetail := ClassifyInputError(nil); emptyCode != "" || emptyDetail != "" {
		t.Fatalf("ClassifyInputError(nil) = (%q, %q), want empty", emptyCode, emptyDetail)
	}
	if _, ok := InputErrorFromCode("NOT_A_CODE"); ok {
		t.Fatal("InputErrorFromCode accepted an unknown code")
	}
}
