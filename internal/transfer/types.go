package transfer

import (
	"context"
	"time"
)

const (
	updateEventName = "transfer:update"
	emitInterval    = 500 * time.Millisecond
	speedWindow     = 5 * time.Second
	mib             = 1024 * 1024
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusPreparing Status = "preparing"
	StatusProgress  Status = "progress"
	StatusCompleted Status = "completed"
	StatusPaused    Status = "paused"
	StatusCanceled  Status = "canceled"
	StatusError     Status = "error"
)

type WindowProgressMode string

const (
	WindowProgressNormal        WindowProgressMode = "normal"
	WindowProgressIndeterminate WindowProgressMode = "indeterminate"
	WindowProgressError         WindowProgressMode = "error"
	WindowProgressPaused        WindowProgressMode = "paused"
)

type WindowProgress struct {
	Value float64
	Mode  WindowProgressMode
}

type PlanPhase string

const (
	PlanPermissionCheck PlanPhase = "permission_check"
	PlanParentLookup    PlanPhase = "parent_lookup"
	PlanFileValidation  PlanPhase = "file_validation"
	PlanDedupLookup     PlanPhase = "dedup_lookup"
	PlanProcessing      PlanPhase = "processing"
)

type DownloadFile struct {
	ID         string  `json:"id" cbor:"id"`
	FileID     string  `json:"fileId" cbor:"fileId"`
	ParentID   *string `json:"parentId" cbor:"parentId"`
	Name       string  `json:"name" cbor:"name"`
	Size       int64   `json:"size" cbor:"size"`
	UncompSize *int64  `json:"uncompSize,omitempty" cbor:"uncompSize,omitempty"`
	CompAlg    *string `json:"compAlg" cbor:"compAlg"`
	URL        string  `json:"url" cbor:"url"`
	URLOrigin  string  `json:"urlOrigin,omitempty" cbor:"urlOrigin,omitempty"`
}

func LogicalFileBytes(file DownloadFile) int64 {
	if file.UncompSize != nil {
		return *file.UncompSize
	}
	return file.Size
}

type Root struct {
	ID       string  `json:"id" cbor:"id"`
	ParentID *string `json:"parentId" cbor:"parentId"`
	Name     string  `json:"name" cbor:"name"`
}

type Directory struct {
	ID       string  `json:"id" cbor:"id"`
	ParentID *string `json:"parentId" cbor:"parentId"`
	Name     string  `json:"name" cbor:"name"`
}

type DestinationKind string

const (
	DestinationFile      DestinationKind = "file"
	DestinationDirectory DestinationKind = "directory"
)

type DestinationTarget struct {
	Path string          `json:"path"`
	Kind DestinationKind `json:"kind"`
}

type Data struct {
	Root  *Root          `json:"root,omitempty"`
	Files []DownloadFile `json:"files"`
	Dirs  []Directory    `json:"dirs"`
}

// Snapshot is the renderer-safe transfer shape. The payload Data and runner
// state are deliberately absent, matching Electron's getDisplayTransfers.
type Snapshot struct {
	PID                string              `json:"pid"`
	Type               string              `json:"type"`
	QueueGroupID       *uint64             `json:"queueGroupId,omitempty"`
	CurrentID          string              `json:"currentId,omitempty"`
	Status             Status              `json:"status"`
	TotalSize          int64               `json:"totalSize"`
	TransferredSize    int64               `json:"transferedSize"`
	Progress           float64             `json:"progress"`
	Speed              float64             `json:"speed"`
	ETA                float64             `json:"eta"`
	StartTime          int64               `json:"startTime"`
	Name               string              `json:"name"`
	TotalFiles         int                 `json:"totalFiles"`
	TransferredFiles   int                 `json:"transferedFiles"`
	FailedFiles        int                 `json:"failedFiles"`
	Path               string              `json:"path,omitempty"`
	DestinationPaths   []string            `json:"destinationPaths,omitempty"`
	DestinationTargets []DestinationTarget `json:"destinationTargets,omitempty"`
	Error              string              `json:"error,omitempty"`
	ErrorCode          string              `json:"errorCode,omitempty"`
	PlanPhase          *PlanPhase          `json:"planPhase,omitempty"`
	PlanProgress       *float64            `json:"planProgress,omitempty"`
}

type Record struct {
	Snapshot
	Data Data `json:"data"`
}

type CreateParams struct {
	PID                string
	Type               string
	Name               string
	Path               string
	DestinationPaths   []string
	DestinationTargets []DestinationTarget
	CurrentID          string
	InitialStatus      Status
	Data               Data
	QueueGroupID       *uint64
	ManualStart        bool
	RestartData        any
}

// Runner performs one transfer attempt. Implementations must stop promptly
// when ctx is cancelled so pause, cancel, and shutdown preserve queue order.
type Runner func(ctx context.Context, transfer *Transfer, pid string) error

type Settings interface {
	GetPowerSaveBlockInTransfer(context.Context) (bool, error)
	GetDownloadBandwidthLimitMibps(context.Context) (int, error)
}

type startNotificationSettings interface {
	GetMoveTransferPageWhenStartTransfer(context.Context) (bool, error)
}

type Logger interface {
	Error(msg any, where string)
}

type Options struct {
	Settings           Settings
	Log                Logger
	ReportFailure      func(error, map[string]any) error
	EventEmit          func(name string, data ...any)
	PreventSuspension  func(bool) error
	SyncWindowProgress func(*WindowProgress)
	Now                func() time.Time
}

type Updates struct {
	Status             *Status
	CurrentID          *string
	TotalSize          *int64
	TotalFiles         *int
	TransferredSize    *int64
	Progress           *float64
	TransferredFiles   *int
	FailedFiles        *int
	Path               *string
	DestinationPaths   []string
	DestinationTargets []DestinationTarget
	Error              *string
	ErrorCode          *string
	PlanPhase          *PlanPhase
	PlanProgress       *float64
	ClearCurrentID     bool
	ClearError         bool
	ClearErrorCode     bool
	ClearPlanPhase     bool
	ClearPlanProgress  bool
}
