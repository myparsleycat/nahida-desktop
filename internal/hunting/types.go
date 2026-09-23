// Package hunting owns user-controlled, recoverable 3DMigoto resource-hunting
// sessions prepared by Nahida Agent.
package hunting

import (
	"context"
	"errors"
	"time"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
)

var (
	ErrDisabled             = errors.New("HUNTING_DISABLED")
	ErrConfigUnsupported    = errors.New("HUNTING_CONFIG_UNSUPPORTED")
	ErrImagesRequired       = errors.New("HUNTING_IMAGES_REQUIRED")
	ErrWindowNotFound       = errors.New("HUNTING_WINDOW_NOT_FOUND")
	ErrWindowAmbiguous      = errors.New("HUNTING_WINDOW_AMBIGUOUS")
	ErrAlreadyActive        = errors.New("HUNTING_ALREADY_ACTIVE")
	ErrSessionNotFound      = errors.New("HUNTING_SESSION_NOT_FOUND")
	ErrSessionExpired       = errors.New("HUNTING_SESSION_EXPIRED")
	ErrSessionOwnerMismatch = errors.New("HUNTING_SESSION_OWNER_MISMATCH")
	ErrKeyConflict          = errors.New("HUNTING_KEY_CONFLICT")
	ErrCategoryRequired     = errors.New("HUNTING_CATEGORY_REQUIRED")
	ErrClipboardTimeout     = errors.New("HUNTING_CLIPBOARD_TIMEOUT")
	ErrCleanupIncomplete    = errors.New("HUNTING_CLEANUP_INCOMPLETE")
)

type Category string

const (
	CategoryIndexBuffer    Category = "index_buffer"
	CategoryVertexBuffer   Category = "vertex_buffer"
	CategoryVertexShader   Category = "vertex_shader"
	CategoryPixelShader    Category = "pixel_shader"
	CategoryComputeShader  Category = "compute_shader"
	CategoryGeometryShader Category = "geometry_shader"
	CategoryDomainShader   Category = "domain_shader"
	CategoryHullShader     Category = "hull_shader"
)

type Status string

const (
	StatusManual    Status = "manual"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
	StatusFailed    Status = "failed"
	StatusExpired   Status = "expired"
)

// Cleanup reports whether the pre-session hunting state was restored. Pending
// marks an incomplete cleanup that Cancel can retry; an incomplete cleanup
// without Pending is no longer retryable because the game process exited.
type Cleanup struct {
	Complete bool   `json:"complete"`
	Pending  bool   `json:"pending"`
	Error    string `json:"error,omitempty"`
}

type CategoryState struct {
	Category Category `json:"category"`
	Steps    int      `json:"steps"`
}

type Snapshot struct {
	ID                  string          `json:"id"`
	Status              Status          `json:"status"`
	ImporterKey         string          `json:"importerKey"`
	PID                 uint32          `json:"pid"`
	WindowTitle         string          `json:"windowTitle"`
	AvailableCategories []Category      `json:"availableCategories"`
	SelectedCategory    Category        `json:"selectedCategory,omitempty"`
	Categories          []CategoryState `json:"categories"`
	TotalSteps          int             `json:"totalSteps"`
	Hash                string          `json:"hash,omitempty"`
	StartedAt           string          `json:"startedAt"`
	ElapsedMs           int64           `json:"elapsedMs"`
	Cleanup             Cleanup         `json:"cleanup"`
}

type Inspection struct {
	ImporterKey         string              `json:"importerKey"`
	Window              platform.WindowInfo `json:"window"`
	AvailableCategories []Category          `json:"availableCategories"`
	VertexBufferScope   string              `json:"vertexBufferScope"`
	MIMEType            string              `json:"mimeType"`
	Width               int                 `json:"width"`
	Height              int                 `json:"height"`
	PNG                 []byte              `json:"-"`
}

type ImageResult struct {
	Snapshot Snapshot
	Window   platform.WindowInfo
	Width    int
	Height   int
	MIMEType string
	PNG      []byte
}

type ImporterResolver interface {
	ResolveHuntingRuntime(context.Context, string) (xxmi.HuntingRuntime, error)
}

type WindowInput interface {
	ListWindows(context.Context, platform.WindowFilter) ([]platform.WindowInfo, error)
	AcquireForeground(context.Context, platform.WindowTarget) (platform.ForegroundController, error)
	ValidateKeys([]string, []string) error
	// ProcessAlive reports whether the process is still running. Hunting uses
	// it to keep a failed cleanup retryable while the game hides its window.
	ProcessAlive(uint32) bool
}

type FrameCapturer interface {
	CaptureWindow(context.Context, platform.CaptureRequest) (platform.CaptureResult, error)
}

type ClipboardReader interface {
	ReadClipboardText() (platform.ClipboardText, error)
}

type Options struct {
	Importer  ImporterResolver
	Input     WindowInput
	Screen    FrameCapturer
	Clipboard ClipboardReader
	Report    func(error, string, map[string]any)
	Now       func() time.Time
	Wait      func(context.Context, time.Duration) error

	IdleTimeout    time.Duration
	SessionTimeout time.Duration
	WatchInterval  time.Duration
}
