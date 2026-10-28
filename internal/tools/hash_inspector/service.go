// Package hashinspector shows what the game drew under the cursor: the draws, their
// hashes, and the mods and command list lines behind them, read from the
// forked 3DMigoto DLL through internal/gamelink.
package hashinspector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/xxmi"
)

const (
	EventState   = "tools:hash-inspector-state"
	EventCapture = "tools:hash-inspector-capture"

	keptCaptures = 20
	// pickTimeout clears a pick the DLL armed but never answered, so the next one is not refused.
	pickTimeout = 15 * time.Second
	requestWait = 5 * time.Second
)

const (
	ErrorNotConnected = "HASH_INSPECTOR_NOT_CONNECTED"
	ErrorUnsupported  = "HASH_INSPECTOR_UNSUPPORTED"
	ErrorInProgress   = "HASH_INSPECTOR_IN_PROGRESS"
	ErrorNotFound     = "HASH_INSPECTOR_NOT_FOUND"
)

type Status string

const (
	StatusUnsupported  Status = "unsupported"
	StatusNotRunning   Status = "not-running"
	StatusConnecting   Status = "connecting"
	StatusBusy         Status = "busy"
	StatusConnected    Status = "connected"
	StatusIncompatible Status = "incompatible"
)

// Unsupported reasons of a State.
const (
	ReasonProvider = "provider"
	ReasonRenderer = "renderer"
)

type State struct {
	Importer   string           `json:"importer"`
	Status     Status           `json:"status"`
	Reason     string           `json:"reason"`
	DLLVersion string           `json:"dllVersion"`
	InspectKey string           `json:"inspectKey"`
	Picking    bool             `json:"picking"`
	Captures   []CaptureSummary `json:"captures"`
}

type Link interface {
	States() []gamelink.State
	Request(ctx context.Context, importer string, m gamelink.Message) (gamelink.Message, error)
	Subscribe(s gamelink.Subscriber) func()
}

type Importers interface {
	ImporterDLLFeatures(ctx context.Context, importer string) (xxmi.DLLFeatures, error)
	GetImporterConfig(ctx context.Context, importer string) (xxmi.ImporterConfig, error)
}

type Options struct {
	Log       *infra.Log
	EventEmit func(string, ...any)
	Link      Link
	Importers Importers
	Now       func() time.Time
}

type Service struct {
	opts Options

	mu          sync.Mutex
	data        *appdata.Store
	captures    map[string][]Capture
	pending     map[string]*pendingPick
	unsubscribe func()
}

type pendingPick struct {
	captureID uint64
	armedAt   time.Time
	// completed tracks results received while the arm reply is still pending.
	completed map[uint64]struct{}
}

func NewWithOptions(opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{opts: opts, captures: map[string][]Capture{}, pending: map[string]*pendingPick{}}
}

func (s *Service) UseAppData(data *appdata.Store) {
	s.mu.Lock()
	s.data = data
	s.mu.Unlock()
}

// Start subscribes to the channel; results of in-game key picks arrive without a request.
func (s *Service) Start() {
	if s.opts.Link == nil {
		return
	}
	unsubscribe := s.opts.Link.Subscribe(gamelink.Subscriber{OnMessage: s.receive, OnChange: s.linkChanged})
	s.mu.Lock()
	s.unsubscribe = unsubscribe
	s.mu.Unlock()
}

func (s *Service) Shutdown() error {
	s.mu.Lock()
	unsubscribe := s.unsubscribe
	s.unsubscribe = nil
	s.mu.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
	return nil
}

func (s *Service) State(ctx context.Context, importer string) (State, error) {
	importer, err := normalizeImporter(importer)
	if err != nil {
		return State{}, err
	}
	state := State{Importer: importer, Status: StatusNotRunning}
	if s.opts.Importers == nil || s.opts.Link == nil {
		state.Status, state.Reason = StatusUnsupported, ReasonProvider
		return state, nil
	}

	cfg, err := s.opts.Importers.GetImporterConfig(ctx, importer)
	if err != nil {
		return State{}, err
	}
	state.InspectKey = cfg.Migoto.InspectKey
	features, err := s.opts.Importers.ImporterDLLFeatures(ctx, importer)
	if err != nil {
		return State{}, err
	}
	state.Status, state.Reason, state.DLLVersion = s.linkStatus(importer, features)

	captures, err := s.loadCaptures(importer)
	if err != nil {
		return State{}, err
	}
	state.Captures = make([]CaptureSummary, 0, len(captures))
	for _, c := range captures {
		state.Captures = append(state.Captures, c.summary())
	}
	s.mu.Lock()
	_, state.Picking = s.livePending(importer)
	s.mu.Unlock()
	return state, nil
}

func (s *Service) linkStatus(importer string, features xxmi.DLLFeatures) (Status, string, string) {
	status := StatusNotRunning
	for _, link := range s.opts.Link.States() {
		if link.Importer != importer {
			continue
		}
		switch link.Status {
		case gamelink.StatusConnected:
			if link.Info.RenderAPI != gamelink.RenderD3D11 || link.Info.Capabilities&gamelink.CapabilityPickD3D11 == 0 {
				return StatusUnsupported, ReasonRenderer, link.Info.DLLVersion
			}
			return StatusConnected, "", link.Info.DLLVersion
		case gamelink.StatusIncompatible:
			status = StatusIncompatible
		case gamelink.StatusBusy:
			if status == StatusNotRunning || status == StatusConnecting {
				status = StatusBusy
			}
		case gamelink.StatusConnecting:
			if status == StatusNotRunning {
				status = StatusConnecting
			}
		}
	}
	// A running game whose DLL never serves the channel stays "connecting", which only the release can explain.
	if !features.DesktopChannel && status != StatusIncompatible && status != StatusBusy {
		return StatusUnsupported, ReasonProvider, ""
	}
	return status, "", ""
}

// Pick asks the DLL to capture what is under the cursor in the next frame; the
// result arrives as an EventCapture.
func (s *Service) Pick(ctx context.Context, importer string) (returnErr error) {
	importer, err := normalizeImporter(importer)
	if err != nil {
		return err
	}
	stage := "state"
	defer func() {
		if returnErr != nil && !isContract(returnErr) {
			returnErr = infra.ReportError(s.opts.Log, returnErr, "HashInspector.Pick", infra.Diagnostic{
				Operation: "pick", Stage: stage, Fields: map[string]any{"importer": importer},
			})
		}
	}()

	state, err := s.State(ctx, importer)
	if err != nil {
		return err
	}
	switch state.Status {
	case StatusConnected:
	case StatusUnsupported, StatusIncompatible:
		return infra.ContractError(ErrorUnsupported + ":" + string(state.Status) + " " + state.Reason)
	default:
		return infra.ContractError(ErrorNotConnected + ":" + string(state.Status))
	}
	s.mu.Lock()
	if _, picking := s.livePending(importer); picking {
		s.mu.Unlock()
		return infra.ContractError(ErrorInProgress + ":" + importer)
	}
	pick := &pendingPick{armedAt: s.opts.Now(), completed: map[uint64]struct{}{}}
	s.pending[importer] = pick
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if returnErr != nil && s.pending[importer] == pick {
			delete(s.pending, importer)
		}
		s.mu.Unlock()
		s.emit(EventState, importer)
	}()

	stage = "arm"
	requestCtx, cancel := context.WithTimeout(ctx, requestWait)
	defer cancel()
	reply, err := s.opts.Link.Request(requestCtx, importer, gamelink.PickArm{Cursor: gamelink.CursorCurrent})
	var remote *gamelink.RemoteError
	if errors.As(err, &remote) && remote.Code == gamelink.ErrorBusy {
		return infra.ContractError(ErrorInProgress + ":" + importer)
	}
	if errors.Is(err, gamelink.ErrNotConnected) {
		return infra.ContractError(ErrorNotConnected + ":" + importer)
	}
	if err != nil {
		return err
	}
	armed, ok := reply.(gamelink.PickArmed)
	if !ok {
		return fmt.Errorf("PickArm answered with %T", reply)
	}

	s.mu.Lock()
	if s.pending[importer] == pick {
		if _, completed := pick.completed[armed.CaptureID]; completed {
			delete(s.pending, importer)
		} else {
			pick.captureID = armed.CaptureID
			pick.completed = nil
		}
	}
	s.mu.Unlock()
	return nil
}

func isContract(err error) bool {
	var contract infra.ContractError
	return errors.As(err, &contract)
}

func (s *Service) receive(importer string, pid uint32, m gamelink.Message) {
	var capture Capture
	var captureID uint64
	switch msg := m.(type) {
	case gamelink.PickResult:
		modsRoot := ""
		if s.opts.Link != nil {
			for _, link := range s.opts.Link.States() {
				if link.Importer != importer || link.PID != pid {
					continue
				}
				if link.Info.ImporterFolder != "" {
					modsRoot = filepath.Join(link.Info.ImporterFolder, "Mods")
				}
				break
			}
		}
		capture, captureID = assembleCapture(msg, modsRoot), msg.CaptureID
	case gamelink.PickFailed:
		// A failure does not say what started the pick; only a desktop pick is pending.
		trigger := "key"
		s.mu.Lock()
		if pick, ok := s.pending[importer]; ok && pick.captureID == msg.CaptureID {
			trigger = "desktop"
		}
		s.mu.Unlock()
		capture = Capture{Trigger: trigger, FailureCode: msg.Code, FailureDetail: msg.Detail, Layers: []Layer{}}
		captureID = msg.CaptureID
	default:
		return
	}

	now := s.opts.Now()
	capture.ID = fmt.Sprintf("%d-%d-%d", now.UnixMilli(), pid, captureID)
	capture.Importer = importer
	capture.CapturedAt = now.UTC().Format(time.RFC3339)
	if err := s.storeCapture(importer, captureID, capture); err != nil {
		_ = infra.ReportError(s.opts.Log, err, "HashInspector.Receive", infra.Diagnostic{
			Operation: "store-capture", Stage: "write",
			Fields: map[string]any{"importer": importer, "pid": pid, "captureId": captureID},
		})
	}
	s.emit(EventCapture, importer, capture.ID)
	s.emit(EventState, importer)
}

func (s *Service) linkChanged() {
	s.emit(EventState, "")
}

func (s *Service) Capture(_ context.Context, importer, id string) (Capture, error) {
	importer, err := normalizeImporter(importer)
	if err != nil {
		return Capture{}, err
	}
	captures, err := s.loadCaptures(importer)
	if err != nil {
		return Capture{}, err
	}
	for _, c := range captures {
		if c.ID == id {
			return c, nil
		}
	}
	return Capture{}, infra.ContractError(ErrorNotFound + ":" + id)
}

func (s *Service) DeleteCaptures(_ context.Context, importer string) error {
	importer, err := normalizeImporter(importer)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.captures[importer] = []Capture{}
	err = s.writeCaptures(importer)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.emit(EventState, importer)
	return nil
}

// livePending returns the importer's armed pick, dropping one the DLL never
// answered. The caller holds mu.
func (s *Service) livePending(importer string) (*pendingPick, bool) {
	pick, ok := s.pending[importer]
	if ok && s.opts.Now().Sub(pick.armedAt) > pickTimeout {
		delete(s.pending, importer)
		return nil, false
	}
	return pick, ok
}

func (s *Service) storeCapture(importer string, captureID uint64, capture Capture) error {
	if _, err := s.loadCaptures(importer); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if pick, ok := s.pending[importer]; ok {
		if pick.completed != nil {
			pick.completed[captureID] = struct{}{}
		} else if pick.captureID == captureID {
			delete(s.pending, importer)
		}
	}
	captures := append([]Capture{capture}, s.captures[importer]...)
	s.captures[importer] = captures[:min(len(captures), keptCaptures)]
	return s.writeCaptures(importer)
}

func (s *Service) loadCaptures(importer string) ([]Capture, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if captures, ok := s.captures[importer]; ok {
		return slices.Clone(captures), nil
	}
	captures := []Capture{}
	if s.data != nil {
		data, err := s.data.ReadFile(capturesFile(importer))
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return nil, err
		default:
			// A file this build cannot read is replaced by the next capture rather than blocking the tool.
			if json.Unmarshal(data, &captures) != nil {
				captures = []Capture{}
			}
		}
	}
	s.captures[importer] = captures
	return slices.Clone(captures), nil
}

// writeCaptures stores the importer's captures. The caller holds mu.
func (s *Service) writeCaptures(importer string) error {
	if s.data == nil {
		return nil
	}
	data, err := json.Marshal(s.captures[importer])
	if err != nil {
		return err
	}
	return s.data.WriteFile(capturesFile(importer), data, 0o600)
}

func capturesFile(importer string) string {
	return filepath.Join(appdata.HashInspectorDir, importer+".json")
}

// normalizeImporter checks an importer key from the renderer, which also names the captures file.
func normalizeImporter(importer string) (string, error) {
	key := strings.ToUpper(strings.TrimSpace(importer))
	if key == "" || len(key) > 16 || strings.ContainsFunc(key, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	}) {
		return "", fmt.Errorf("invalid importer %q", importer)
	}
	return key, nil
}

func (s *Service) emit(name string, data ...any) {
	if s.opts.EventEmit != nil {
		s.opts.EventEmit(name, data...)
	}
}
