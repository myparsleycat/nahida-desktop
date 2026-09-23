package platform

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Input sentinel errors. UPPER_SNAKE prefixes are part of the renderer and
// agent contract, so keep them stable and add detail after the prefix.
var (
	ErrInputTargetRequired    = errors.New("INPUT_TARGET_REQUIRED")
	ErrInputOptionInvalid     = errors.New("INPUT_OPTION_INVALID")
	ErrInputKeyInvalid        = errors.New("INPUT_KEY_INVALID")
	ErrInputKeyUnsupported    = errors.New("INPUT_KEY_UNSUPPORTED")
	ErrWindowNotFound         = errors.New("WINDOW_NOT_FOUND")
	ErrWindowNotForeground    = errors.New("WINDOW_NOT_FOREGROUND")
	ErrSendInputBlocked       = errors.New("SEND_INPUT_BLOCKED")
	ErrElevatedHelperRequired = errors.New("ELEVATED_HELPER_REQUIRED")
)

// inputErrorSentinels is the stable classification set shared with the elevated
// helper. Classification order is stable so an error that wraps more than one
// sentinel always reports the first.
var inputErrorSentinels = []error{
	ErrInputTargetRequired,
	ErrInputOptionInvalid,
	ErrInputKeyInvalid,
	ErrInputKeyUnsupported,
	ErrWindowNotFound,
	ErrWindowNotForeground,
	ErrSendInputBlocked,
	ErrElevatedHelperRequired,
}

// ClassifyInputError splits err into its stable UPPER_SNAKE code and the
// remaining detail. The elevated helper transports the code in a dedicated
// field because it cannot pass an error object across the pipe; the client
// rebuilds the sentinel so the renderer and agent see the same prefix and
// errors.Is result they see for a local target. The code is empty when err
// carries no input classification.
func ClassifyInputError(err error) (code string, detail string) {
	if err == nil {
		return "", ""
	}
	for _, sentinel := range inputErrorSentinels {
		if errors.Is(err, sentinel) {
			code = sentinel.Error()
			return code, strings.TrimPrefix(strings.TrimPrefix(err.Error(), code), ": ")
		}
	}
	return "", err.Error()
}

// InputErrorFromCode resolves a stable classification code back to its sentinel.
func InputErrorFromCode(code string) (error, bool) {
	for _, sentinel := range inputErrorSentinels {
		if sentinel.Error() == code {
			return sentinel, true
		}
	}
	return nil, false
}

// ElevatedHelperStatus is the renderer-facing snapshot of whether the helper
// setting is on and whether the helper process is actually connected.
type ElevatedHelperStatus struct {
	Enabled bool `json:"enabled"`
	Running bool `json:"running"`
}

// ElevatedInputSender handles requests that Windows UIPI prevents this
// process from delivering to a higher-integrity target.
type ElevatedInputSender interface {
	// EnsureReady reports whether the helper connection is live. It does not
	// launch the helper; a missing connection returns ErrElevatedHelperRequired.
	EnsureReady(context.Context) error
	SendKeys(context.Context, KeyRequest) (KeyResult, error)
}

// KeyDelivery selects how a key reaches the target window.
type KeyDelivery string

const (
	// KeyDeliveryForeground focuses the target window and injects real input
	// with SendInput. Games and other raw-input targets only react to this one.
	KeyDeliveryForeground KeyDelivery = "foreground"
	// KeyDeliveryMessage posts WM_KEYDOWN/WM_KEYUP, so the target window stays
	// in the background. Message-based applications only.
	KeyDeliveryMessage KeyDelivery = "message"
)

const (
	defaultKeyHold     = 30 * time.Millisecond
	defaultKeyInterval = 60 * time.Millisecond
	maxKeyHold         = 2 * time.Second
	maxKeyInterval     = 5 * time.Second
	maxKeyCount        = 16
	defaultWindowLimit = 50
	maxWindowLimit     = 200
)

// WindowTarget selects the window that receives the keys. Title, Process, and
// PID are combined with AND, and at least one of them must be set.
type WindowTarget struct {
	Title   string `json:"title,omitempty"`
	Process string `json:"process,omitempty"`
	PID     uint32 `json:"pid,omitempty"`
}

func (t WindowTarget) empty() bool {
	return normalizeWindowText(t.Title) == "" && normalizeWindowText(t.Process) == "" && t.PID == 0
}

func (t WindowTarget) describe() string {
	parts := make([]string, 0, 3)
	if title := strings.TrimSpace(t.Title); title != "" {
		parts = append(parts, "title "+strconv.Quote(title))
	}
	if process := strings.TrimSpace(t.Process); process != "" {
		parts = append(parts, "process "+strconv.Quote(process))
	}
	if t.PID != 0 {
		parts = append(parts, "pid "+strconv.FormatUint(uint64(t.PID), 10))
	}
	if len(parts) == 0 {
		return "no target"
	}
	return strings.Join(parts, ", ")
}

// resolveDiagnosticFields describes a window resolution failure for the
// diagnostic log. The normalized title tells a title that only differs by
// invisible whitespace apart from a real mismatch.
func (t WindowTarget) resolveDiagnosticFields() map[string]any {
	fields := map[string]any{"target": t.describe()}
	if title := normalizeWindowText(t.Title); title != "" {
		fields["normalizedTitle"] = title
	}
	return fields
}

// WindowInfo describes a visible top-level window. Handle is informational
// only; resolve the window again for every request instead of reusing it.
type WindowInfo struct {
	Handle       string `json:"handle"`
	Title        string `json:"title"`
	ClassName    string `json:"className"`
	PID          uint32 `json:"pid"`
	ProcessName  string `json:"processName"`
	IsForeground bool   `json:"isForeground"`
	IsMinimized  bool   `json:"isMinimized"`
}

// WindowFilter narrows Input.ListWindows. Title and Process match case
// insensitive substrings, which makes the filter usable for window discovery.
type WindowFilter struct {
	Title   string `json:"title,omitempty"`
	Process string `json:"process,omitempty"`
	PID     uint32 `json:"pid,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// normalizeWindowText folds every Unicode whitespace sequence into one ASCII
// space. Actual window titles carry NBSP and other invisible spaces that
// render like a regular space, so matching compares the normalized form while
// WindowInfo keeps the original text for display.
func normalizeWindowText(value string) string {
	return strings.Join(strings.FieldsFunc(value, unicode.IsSpace), " ")
}

// KeyRequest is the payload of Input.SendKeys. Each Keys entry is one chord in
// the notation KeyNotation documents: "vk_f10", "f12", "ctrl alt vk_f5",
// "vk_decimal vk_numpad2", or a single character key. A plus sign is not a separator.
type KeyRequest struct {
	Target       WindowTarget `json:"target"`
	Keys         []string     `json:"keys"`
	Delivery     KeyDelivery  `json:"delivery,omitempty"`
	HoldMs       int          `json:"holdMs,omitempty"`
	IntervalMs   int          `json:"intervalMs,omitempty"`
	RestoreFocus *bool        `json:"restoreFocus,omitempty"`
}

// KeyNotation is the keys-parameter spelling for SendKeys. It is not the action
// description: that text is copied into the routing index on every turn.
// Letters, digits, and function keys can be written as typed. Every other key
// is listed with the accepted vk_ token first, so a caller copies that token
// rather than the rejected everyday name.
func KeyNotation() string {
	return strings.Join([]string{
		"Each keys item is one chord; its tokens are pressed together in order and released in reverse order.",
		`Separate chord keys with spaces, never '+': "ctrl f12" or "vk_decimal vk_numpad2", not "Ctrl+F12".`,
		"Letters, digits, and f1-f24 are accepted as written; vk_a, vk_7, and vk_f12 also work.",
		"Any other name is rejected before the window is targeted.",
		"Send the vk_ token exactly. Do not send the parenthetical label.",
		"Tokens: " + uncommonKeyTokens + ".",
		"Punctuation is the character itself: [ ] \\ ; ' , . / ` - = +.",
		"Modifiers are ctrl, alt, shift, and win.",
		"Gamepad names starting with xb_ cannot be sent.",
	}, " ")
}

// uncommonKeyTokens lists accepted tokens before the name a caller would
// invent. Function keys, letters, and digits are omitted because those
// spellings are accepted directly.
const uncommonKeyTokens = "vk_return (not enter), vk_escape (not escape), vk_back (not backspace), " +
	"vk_tab (not tab), vk_space (not space), vk_left (not left), vk_up (not up), " +
	"vk_right (not right), vk_down (not down), vk_home (not home), vk_end (not end), " +
	"vk_prior (not pageup), vk_next (not pagedown), vk_insert (not insert), " +
	"vk_delete (not delete), vk_pause (not pause), vk_snapshot (not printscreen), " +
	"vk_apps (not menu), " +
	"vk_numpad0 (not numpad0), vk_numpad1 (not numpad1), vk_numpad2 (not numpad2), " +
	"vk_numpad3 (not numpad3), vk_numpad4 (not numpad4), vk_numpad5 (not numpad5), " +
	"vk_numpad6 (not numpad6), vk_numpad7 (not numpad7), vk_numpad8 (not numpad8), " +
	"vk_numpad9 (not numpad9), vk_multiply (numpad star), vk_add (numpad plus), " +
	"vk_subtract (numpad minus), vk_decimal (numpad decimal), vk_divide (numpad slash), " +
	"vk_lshift (left shift), vk_rshift (right shift), vk_lcontrol (left ctrl), " +
	"vk_rcontrol (right ctrl), vk_lmenu (left alt), vk_rmenu (right alt), " +
	"vk_lwin (left win), vk_rwin (right win)"

// KeyResult reports which window received the keys.
type KeyResult struct {
	Window   WindowInfo  `json:"window"`
	Delivery KeyDelivery `json:"delivery"`
	Keys     []string    `json:"keys"`
}

// keyOptions is the validated delivery and timing of one SendKeys request.
type keyOptions struct {
	delivery     KeyDelivery
	hold         time.Duration
	interval     time.Duration
	restoreFocus bool
}

func resolveKeyRequest(request KeyRequest) ([]string, keyOptions, error) {
	if request.Target.empty() {
		return nil, keyOptions{}, ErrInputTargetRequired
	}

	keys := make([]string, 0, len(request.Keys))
	for _, key := range request.Keys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	switch {
	case len(keys) == 0:
		return nil, keyOptions{}, fmt.Errorf("%w: no key was requested", ErrInputKeyInvalid)
	case len(keys) > maxKeyCount:
		return nil, keyOptions{}, fmt.Errorf("%w: at most %d keys per request", ErrInputKeyInvalid, maxKeyCount)
	}

	options := keyOptions{
		delivery:     KeyDeliveryForeground,
		hold:         defaultKeyHold,
		interval:     defaultKeyInterval,
		restoreFocus: true,
	}
	if request.Delivery != "" {
		options.delivery = request.Delivery
	}
	if options.delivery != KeyDeliveryForeground && options.delivery != KeyDeliveryMessage {
		return nil, keyOptions{}, fmt.Errorf("%w: unknown delivery %q", ErrInputOptionInvalid, request.Delivery)
	}

	hold, err := resolveKeyDuration("holdMs", request.HoldMs, defaultKeyHold, maxKeyHold)
	if err != nil {
		return nil, keyOptions{}, err
	}
	interval, err := resolveKeyDuration("intervalMs", request.IntervalMs, defaultKeyInterval, maxKeyInterval)
	if err != nil {
		return nil, keyOptions{}, err
	}
	options.hold, options.interval = hold, interval

	if request.RestoreFocus != nil {
		options.restoreFocus = *request.RestoreFocus
	}
	return keys, options, nil
}

func resolveKeyDuration(name string, value int, fallback, limit time.Duration) (time.Duration, error) {
	if value == 0 {
		return fallback, nil
	}
	duration := time.Duration(value) * time.Millisecond
	if duration < 0 || duration > limit {
		return 0, fmt.Errorf(
			"%w: %s must be between 0 and %d",
			ErrInputOptionInvalid,
			name,
			limit.Milliseconds(),
		)
	}
	return duration, nil
}
