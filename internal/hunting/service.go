package hunting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi"
)

const (
	defaultIdleTimeout     = 5 * time.Minute
	defaultSessionTimeout  = 15 * time.Minute
	defaultWatchInterval   = time.Second
	cleanupTimeout         = 2 * time.Second
	clipboardTimeout       = 2 * time.Second
	initialToggleDelay     = 250 * time.Millisecond
	clipboardPollInterval  = 25 * time.Millisecond
	evidenceImageByteLimit = 3 << 20
)

var resourceHashPattern = regexp.MustCompile(`(?i)^(?:0x)?([0-9a-f]{8,16})$`)

type session struct {
	op sync.Mutex

	id              string
	owner           string
	importerKey     string
	pid             uint32
	windowTitle     string
	target          platform.WindowTarget
	config          huntingConfig
	available       []Category
	initiallyActive bool
	huntingOn       bool
	status          Status
	selected        Category
	steps           map[Category]int
	totalSteps      int
	hash            string
	started         time.Time
	lastActive      time.Time
	cleanup         Cleanup
	cleanupPending  bool
	operationCancel context.CancelFunc
	expireRequested bool
}

type Service struct {
	mu sync.Mutex

	importer  ImporterResolver
	input     WindowInput
	screen    FrameCapturer
	clipboard ClipboardReader
	report    func(error, string, map[string]any)
	now       func() time.Time
	wait      func(context.Context, time.Duration) error

	idleTimeout    time.Duration
	sessionTimeout time.Duration
	watchInterval  time.Duration

	active    *session
	last      *Snapshot
	lastOwner string
	stop      chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
}

func New(options Options) *Service {
	service := &Service{
		importer: options.Importer, input: options.Input, screen: options.Screen, clipboard: options.Clipboard,
		report: options.Report, now: options.Now, wait: options.Wait,
		idleTimeout: options.IdleTimeout, sessionTimeout: options.SessionTimeout,
		watchInterval: options.WatchInterval, stop: make(chan struct{}), done: make(chan struct{}),
	}
	if service.now == nil {
		service.now = time.Now
	}
	if service.wait == nil {
		service.wait = waitContext
	}
	if service.idleTimeout <= 0 {
		service.idleTimeout = defaultIdleTimeout
	}
	if service.sessionTimeout <= 0 {
		service.sessionTimeout = defaultSessionTimeout
	}
	if service.watchInterval <= 0 {
		service.watchInterval = defaultWatchInterval
	}
	go service.watch()
	return service
}

func (s *Service) Inspect(ctx context.Context, importerKey string, pid uint32) (Inspection, error) {
	runtime, config, window, err := s.resolve(ctx, importerKey, pid)
	if err != nil {
		return Inspection{}, err
	}
	captured, err := s.screen.CaptureWindow(ctx, platform.CaptureRequest{
		Target: platform.WindowTarget{PID: window.PID}, ClientOnly: true, MaxBytes: evidenceImageByteLimit,
	})
	if err != nil {
		return Inspection{}, fmt.Errorf("capture hunting inspection: %w", err)
	}
	return Inspection{
		ImporterKey: runtime.ImporterKey, Window: captured.Window,
		AvailableCategories: categories(config), VertexBufferScope: "current_slot",
		MIMEType: "image/png", Width: captured.Width, Height: captured.Height, PNG: captured.PNG,
	}, nil
}

func (s *Service) Begin(
	ctx context.Context,
	owner, importerKey string,
	pid uint32,
	initiallyActive bool,
) (ImageResult, error) {
	if strings.TrimSpace(owner) == "" {
		return ImageResult{}, fmt.Errorf("%w: missing Agent session owner", ErrSessionOwnerMismatch)
	}
	runtime, config, window, err := s.resolve(ctx, importerKey, pid)
	if err != nil {
		return ImageResult{}, err
	}
	// A cancelled begin must not register a session. The Agent cancels the
	// approved action before restoring the hunting state on session deletion, so
	// registering here would enable hunting mode for a conversation already gone.
	if err := ctx.Err(); err != nil {
		return ImageResult{}, err
	}

	id, err := newSessionID()
	if err != nil {
		return ImageResult{}, err
	}
	now := s.now()
	current := &session{
		id: id, owner: owner, importerKey: runtime.ImporterKey, pid: window.PID,
		windowTitle: window.Title, target: platform.WindowTarget{PID: window.PID}, config: config,
		available: categories(config), initiallyActive: initiallyActive, huntingOn: initiallyActive,
		status: StatusManual, steps: make(map[Category]int), started: now, lastActive: now,
	}
	s.mu.Lock()
	if s.active != nil {
		active := s.active
		id, status := active.id, active.status
		pending := active.cleanupPending
		s.mu.Unlock()
		if pending {
			return ImageResult{}, fmt.Errorf(
				"%w: session %s still needs its hunting state restored", ErrAlreadyActive, id,
			)
		}
		return ImageResult{}, fmt.Errorf("%w: session %s is still %s", ErrAlreadyActive, id, status)
	}
	s.active = current
	s.mu.Unlock()

	current.op.Lock()
	defer current.op.Unlock()
	if err := s.requireActive(current); err != nil {
		return ImageResult{}, err
	}
	operationCtx, cancel := context.WithCancel(ctx)
	s.setOperationCancel(current, cancel)
	defer s.clearOperationCancel(current, cancel)

	lease, err := s.input.AcquireForeground(operationCtx, current.target)
	if err != nil {
		return ImageResult{}, s.failSession(current, err)
	}
	if initiallyActive {
		if err := s.sendBinding(operationCtx, lease, config.toggle); err != nil {
			_ = lease.Close()
			return ImageResult{}, s.failSession(current, err)
		}
		current.huntingOn = false
		if err := s.wait(operationCtx, initialToggleDelay); err != nil {
			_ = lease.Close()
			return ImageResult{}, s.failOrCancel(current, err)
		}
	}

	captured, captureErr := s.screen.CaptureWindow(operationCtx, platform.CaptureRequest{
		Target: current.target, ClientOnly: true, MaxBytes: evidenceImageByteLimit,
	})
	if captureErr == nil {
		captureErr = s.sendBinding(operationCtx, lease, config.toggle)
		if captureErr == nil {
			current.huntingOn = true
			captureErr = s.wait(operationCtx, initialToggleDelay)
		}
	}
	closeErr := lease.Close()
	if err := errors.Join(captureErr, closeErr); err != nil {
		return ImageResult{}, s.failOrCancel(current, err)
	}
	s.touch(current, StatusManual)
	return ImageResult{
		Snapshot: s.snapshot(current), Window: captured.Window, Width: captured.Width, Height: captured.Height,
		MIMEType: "image/png", PNG: captured.PNG,
	}, nil
}

// Next advances exactly one resource in the category selected by the user.
func (s *Service) Next(ctx context.Context, owner string, category Category) (Snapshot, error) {
	current, err := s.ownerSession(owner)
	if err != nil {
		return Snapshot{}, err
	}
	current.op.Lock()
	defer current.op.Unlock()
	if err := s.requireActive(current); err != nil {
		return s.snapshot(current), err
	}

	bindings, ok := current.config.categories[category]
	if !ok {
		return s.snapshot(current), fmt.Errorf("%w: category %q is unavailable", ErrConfigUnsupported, category)
	}
	operationCtx, cancel := context.WithCancel(ctx)
	s.setOperationCancel(current, cancel)
	defer s.clearOperationCancel(current, cancel)

	lease, err := s.input.AcquireForeground(operationCtx, current.target)
	if err != nil {
		return s.snapshot(current), err
	}
	sendErr := s.sendBinding(operationCtx, lease, bindings.next)
	closeErr := lease.Close()
	if sendErr != nil {
		return s.snapshot(current), errors.Join(sendErr, closeErr)
	}

	s.mu.Lock()
	if s.active == current {
		current.selected = category
		current.steps[category]++
		current.totalSteps++
		current.lastActive = s.now()
	}
	s.mu.Unlock()
	if closeErr != nil {
		return s.snapshot(current), closeErr
	}
	return s.snapshot(current), nil
}

// Found marks the currently selected resource and completes the session once
// 3DMigoto publishes its hash to the clipboard.
func (s *Service) Found(ctx context.Context, owner string) (Snapshot, error) {
	current, err := s.ownerSession(owner)
	if err != nil {
		return Snapshot{}, err
	}
	current.op.Lock()
	defer current.op.Unlock()
	if err := s.requireActive(current); err != nil {
		return s.snapshot(current), err
	}

	if current.selected == "" {
		return s.snapshot(current), ErrCategoryRequired
	}
	if s.clipboard == nil {
		return s.snapshot(current), fmt.Errorf("%w: text clipboard access is unavailable", ErrConfigUnsupported)
	}
	operationCtx, cancel := context.WithCancel(ctx)
	s.setOperationCancel(current, cancel)
	defer s.clearOperationCancel(current, cancel)

	before, readErr := s.clipboard.ReadClipboardText()
	// A non-text clipboard can still provide the sequence needed to reject stale hashes.
	if readErr != nil && before.Sequence == 0 {
		return s.snapshot(current), fmt.Errorf("read clipboard before marking: %w", readErr)
	}
	lease, err := s.input.AcquireForeground(operationCtx, current.target)
	if err != nil {
		return s.snapshot(current), err
	}
	sendErr := s.sendBinding(operationCtx, lease, current.config.categories[current.selected].mark)
	closeErr := lease.Close()
	if sendErr != nil {
		return s.snapshot(current), errors.Join(sendErr, closeErr)
	}

	deadline := s.now().Add(clipboardTimeout)
	for s.now().Before(deadline) {
		if err := operationCtx.Err(); err != nil {
			return s.snapshot(current), err
		}
		observed, readErr := s.clipboard.ReadClipboardText()
		if readErr == nil && observed.Sequence != before.Sequence {
			match := resourceHashPattern.FindStringSubmatch(strings.TrimSpace(observed.Text))
			if len(match) == 2 {
				s.mu.Lock()
				current.hash = strings.ToLower(match[1])
				s.mu.Unlock()
				snapshot, finishErr := s.finishSession(current, StatusCompleted, nil)
				return snapshot, errors.Join(closeErr, finishErr)
			}
		}
		if err := s.wait(operationCtx, clipboardPollInterval); err != nil {
			return s.snapshot(current), err
		}
	}
	s.touch(current, StatusManual)
	return s.snapshot(current), errors.Join(
		closeErr,
		fmt.Errorf("%w: no new 8-16 digit hexadecimal hash arrived", ErrClipboardTimeout),
	)
}

func (s *Service) CancelOwner(ctx context.Context, owner string) (Snapshot, error) {
	current, err := s.ownerSession(owner)
	if err != nil {
		return Snapshot{}, err
	}
	return s.cancel(ctx, current)
}

func (s *Service) Cancel(ctx context.Context, owner, id string) (Snapshot, error) {
	current, err := s.session(owner, id)
	if err != nil {
		if snapshot, ok := s.terminalSnapshot(owner, id); ok {
			return snapshot, nil
		}
		return Snapshot{}, err
	}
	return s.cancel(ctx, current)
}

func (s *Service) cancel(ctx context.Context, current *session) (Snapshot, error) {
	current.op.Lock()
	defer current.op.Unlock()
	if err := ctx.Err(); err != nil {
		return s.snapshot(current), err
	}
	return s.finishSession(current, StatusCancelled, nil)
}

func (s *Service) Status(owner, id string) (Snapshot, error) {
	current, err := s.session(owner, id)
	if err == nil {
		return s.snapshot(current), nil
	}
	if snapshot, ok := s.terminalSnapshot(owner, id); ok {
		return snapshot, nil
	}
	return Snapshot{}, err
}

// Current returns the active or most recent hunting state owned by the Agent
// conversation without extending its idle deadline.
func (s *Service) Current(owner string) *Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.owner == owner {
		snapshot := s.snapshotLocked(s.active)
		return &snapshot
	}
	if s.last != nil && s.lastOwner == owner {
		snapshot := *s.last
		return &snapshot
	}
	return nil
}

func (s *Service) Shutdown(ctx context.Context) error {
	s.stopOnce.Do(func() { close(s.stop) })
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	s.mu.Lock()
	current := s.active
	if current != nil && current.operationCancel != nil {
		current.operationCancel()
	}
	s.mu.Unlock()
	if current == nil {
		return nil
	}
	current.op.Lock()
	defer current.op.Unlock()
	_, err := s.finishSession(current, StatusCancelled, nil)
	return err
}

func (s *Service) resolve(
	ctx context.Context,
	importerKey string,
	pid uint32,
) (xxmi.HuntingRuntime, huntingConfig, platform.WindowInfo, error) {
	if s.importer == nil || s.input == nil || s.screen == nil {
		return xxmi.HuntingRuntime{}, huntingConfig{}, platform.WindowInfo{},
			fmt.Errorf("%w: hunting dependencies are unavailable", ErrConfigUnsupported)
	}
	runtime, err := s.importer.ResolveHuntingRuntime(ctx, importerKey)
	if err != nil {
		return xxmi.HuntingRuntime{}, huntingConfig{}, platform.WindowInfo{}, fmt.Errorf("resolve importer: %w", err)
	}
	config, err := readHuntingConfig(runtime.ImporterFolder, runtime.INIPath)
	if err != nil {
		return xxmi.HuntingRuntime{}, huntingConfig{}, platform.WindowInfo{}, err
	}
	if err := validateConfigBindings(s.input, config); err != nil {
		return xxmi.HuntingRuntime{}, huntingConfig{}, platform.WindowInfo{}, err
	}

	byPID := make(map[uint32]platform.WindowInfo)
	for _, executable := range runtime.GameEXENames {
		windows, listErr := s.input.ListWindows(ctx, platform.WindowFilter{
			Process: executable, PID: pid, Limit: 20,
		})
		if listErr != nil {
			return xxmi.HuntingRuntime{}, huntingConfig{}, platform.WindowInfo{}, listErr
		}
		for _, candidate := range windows {
			if _, exists := byPID[candidate.PID]; !exists || candidate.IsForeground {
				byPID[candidate.PID] = candidate
			}
		}
	}
	if len(byPID) == 0 {
		return xxmi.HuntingRuntime{}, huntingConfig{}, platform.WindowInfo{}, fmt.Errorf(
			"%w: no visible window matches %s", ErrWindowNotFound, strings.Join(runtime.GameEXENames, ", "),
		)
	}
	if len(byPID) > 1 {
		pids := make([]string, 0, len(byPID))
		for candidatePID := range byPID {
			pids = append(pids, fmt.Sprint(candidatePID))
		}
		slices.Sort(pids)
		return xxmi.HuntingRuntime{}, huntingConfig{}, platform.WindowInfo{}, fmt.Errorf(
			"%w: choose one of pids %s", ErrWindowAmbiguous, strings.Join(pids, ", "),
		)
	}
	for _, candidate := range byPID {
		return runtime, config, candidate, nil
	}
	panic("unreachable")
}

func (s *Service) sendBinding(ctx context.Context, lease platform.ForegroundController, value binding) error {
	pressed, err := lease.PressedKeys(value.forbidden)
	if err != nil {
		return fmt.Errorf("check hunting key conditions: %w", err)
	}
	if len(pressed) > 0 {
		return fmt.Errorf("%w: release %s", ErrKeyConflict, strings.Join(pressed, ", "))
	}
	if _, err := lease.SendKeys(ctx, []string{value.chord}); err != nil {
		return fmt.Errorf("send hunting key %q: %w", value.chord, err)
	}
	return nil
}

func (s *Service) ownerSession(owner string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return nil, ErrSessionNotFound
	}
	if s.active.owner != owner {
		return nil, ErrSessionOwnerMismatch
	}
	if s.expiredLocked(s.active) {
		return nil, ErrSessionExpired
	}
	return s.active, nil
}

func (s *Service) requireActive(current *session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != current {
		return ErrSessionNotFound
	}
	if current.cleanupPending {
		return fmt.Errorf("%w: restore the previous hunting state first", ErrCleanupIncomplete)
	}
	if s.expiredLocked(current) {
		return ErrSessionExpired
	}
	return nil
}

func (s *Service) session(owner, id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.id != id {
		if s.last != nil && s.last.ID == id && s.last.Status == StatusExpired {
			return nil, ErrSessionExpired
		}
		return nil, ErrSessionNotFound
	}
	if s.active.owner != owner {
		return nil, ErrSessionOwnerMismatch
	}
	if s.expiredLocked(s.active) {
		return nil, ErrSessionExpired
	}
	return s.active, nil
}

func (s *Service) expiredLocked(current *session) bool {
	// A session whose cleanup failed stays available for a retry, so it never
	// counts as expired until its hunting state is restored.
	if current.cleanupPending {
		return false
	}
	return s.now().Sub(current.started) >= s.sessionTimeout || s.now().Sub(current.lastActive) >= s.idleTimeout
}

func (s *Service) touch(current *session, status Status) {
	s.mu.Lock()
	if s.active == current {
		current.status = status
		current.lastActive = s.now()
	}
	s.mu.Unlock()
}

func (s *Service) setOperationCancel(current *session, cancel context.CancelFunc) {
	s.mu.Lock()
	if s.active == current {
		current.operationCancel = cancel
	}
	s.mu.Unlock()
}

func (s *Service) clearOperationCancel(current *session, cancel context.CancelFunc) {
	cancel()
	s.mu.Lock()
	if s.active == current {
		current.operationCancel = nil
	}
	s.mu.Unlock()
}

func (s *Service) snapshot(current *session) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(current)
}

func (s *Service) snapshotLocked(current *session) Snapshot {
	states := make([]CategoryState, 0, len(current.available))
	for _, category := range current.available {
		states = append(states, CategoryState{Category: category, Steps: current.steps[category]})
	}
	return Snapshot{
		ID: current.id, Status: current.status, ImporterKey: current.importerKey,
		PID: current.pid, WindowTitle: current.windowTitle,
		AvailableCategories: append([]Category(nil), current.available...),
		SelectedCategory:    current.selected, Categories: states, TotalSteps: current.totalSteps, Hash: current.hash,
		StartedAt: current.started.UTC().Format(time.RFC3339Nano),
		ElapsedMs: s.now().Sub(current.started).Milliseconds(), Cleanup: current.cleanup,
	}
}

func (s *Service) terminalSnapshot(owner, id string) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil || s.last.ID != id || (owner != "" && s.lastOwner != owner) {
		return Snapshot{}, false
	}
	return *s.last, true
}

func (s *Service) failOrCancel(current *session, cause error) error {
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		_, err := s.finishSession(current, StatusCancelled, cause)
		return err
	}
	return s.failSession(current, cause)
}

func (s *Service) failSession(current *session, cause error) error {
	_, err := s.finishSession(current, StatusFailed, cause)
	return err
}

func (s *Service) finishSession(current *session, status Status, cause error) (Snapshot, error) {
	s.mu.Lock()
	if s.active != current {
		// Another control already ended this session; its cleanup must not run twice.
		if s.last != nil && s.last.ID == current.id {
			snapshot := *s.last
			s.mu.Unlock()
			return snapshot, cause
		}
		s.mu.Unlock()
		return s.snapshot(current), ErrSessionNotFound
	}
	if current.cleanupPending {
		// A retry only finishes restoring the game state, so it keeps the reason
		// the session ended, such as the hash found before the failed cleanup.
		status = current.status
	}
	s.mu.Unlock()

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	cleanupErr := s.cleanup(cleanupCtx, current)
	cleanup := Cleanup{Complete: cleanupErr == nil, Pending: cleanupErr != nil}
	if cleanupErr != nil {
		cleanup.Error = cleanupErr.Error()
		cleanupErr = fmt.Errorf("%w: %w", ErrCleanupIncomplete, cleanupErr)
		s.reportError(cleanupErr, "cleanup", current)
		if s.targetGone(current) {
			// The game window is gone, so no retry can restore anything and the
			// session slot must be released for the next hunting session.
			cleanup.Pending = false
		}
	}

	s.mu.Lock()
	current.cleanup = cleanup
	current.status = status
	current.lastActive = s.now()
	snapshot := s.snapshotLocked(current)
	if cleanup.Pending {
		// Keep the session available so Cancel can retry restoring the game state.
		current.cleanupPending = true
	} else {
		current.cleanupPending = false
		if s.active == current {
			s.active = nil
		}
		s.last = &snapshot
		s.lastOwner = current.owner
	}
	s.mu.Unlock()
	return snapshot, errors.Join(cause, cleanupErr)
}

// targetGone reports whether the session's game is gone. A failed cleanup can
// never restore the hunting state then, so the session is released instead of
// blocking new sessions on a retry that cannot succeed.
func (s *Service) targetGone(current *session) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	windows, err := s.input.ListWindows(ctx, platform.WindowFilter{PID: current.pid, Limit: 20})
	if err != nil || len(windows) > 0 {
		return false
	}
	// ListWindows only reports visible windows, so a game that temporarily hid
	// its window still looks gone here. Only release the session when the game
	// process itself exited.
	return !s.input.ProcessAlive(current.pid)
}

func (s *Service) cleanup(ctx context.Context, current *session) error {
	if current == nil || (!current.huntingOn && !current.initiallyActive) {
		return nil
	}
	lease, err := s.input.AcquireForeground(ctx, current.target)
	if err != nil {
		return err
	}
	var cleanupErr error
	if current.huntingOn {
		if err := s.sendBinding(ctx, lease, current.config.toggle); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			current.huntingOn = false
			cleanupErr = errors.Join(cleanupErr, s.wait(ctx, 100*time.Millisecond))
		}
	}
	if current.initiallyActive && !current.huntingOn {
		if err := s.sendBinding(ctx, lease, current.config.toggle); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		} else {
			current.huntingOn = true
		}
	}
	return errors.Join(cleanupErr, lease.Close())
}

func (s *Service) watch() {
	ticker := time.NewTicker(s.watchInterval)
	defer func() {
		ticker.Stop()
		close(s.done)
	}()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}

		s.mu.Lock()
		current := s.active
		if current == nil {
			s.mu.Unlock()
			continue
		}
		expired := s.expiredLocked(current)
		if expired {
			current.expireRequested = true
			if current.operationCancel != nil {
				current.operationCancel()
			}
		}
		s.mu.Unlock()
		if !expired {
			continue
		}

		current.op.Lock()
		if _, ok := s.terminalSnapshot("", current.id); !ok {
			_, err := s.finishSession(current, StatusExpired, ErrSessionExpired)
			s.reportError(err, "expire", current)
		}
		current.op.Unlock()
	}
}

func (s *Service) reportError(err error, stage string, current *session) {
	if err == nil || s.report == nil {
		return
	}
	s.report(err, stage, map[string]any{
		"sessionId": current.id, "importer": current.importerKey, "pid": current.pid,
		"category": current.selected, "steps": current.totalSteps,
	})
}

func newSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("read hunting session entropy: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func waitContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
