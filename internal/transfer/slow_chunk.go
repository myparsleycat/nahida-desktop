package transfer

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"
)

const (
	SlowChunkMaxReconnects             = 2
	SlowChunkThresholdRatio            = 0.25
	slowChunkMinObserve                = 3 * time.Second
	slowChunkMinPeers                  = 2
	slowChunkCheckInterval             = time.Second
	slowChunkSpeedWindow               = 2 * time.Second
	slowChunkMinSpeedSampleSpan        = 500 * time.Millisecond
	slowChunkNearCompleteRatio         = 0.85
	slowChunkRequiredRelativeSlowTicks = 2
	slowChunkRequiredAbsoluteSlowTicks = 5
	slowChunkMinAbsoluteBPS            = 64 * 1024
	slowChunkReconnectDelay            = 500 * time.Millisecond
	slowChunkReconnectJitter           = 250 * time.Millisecond
	slowChunkStallTimeout              = 15 * time.Second
	slowChunkNearCompleteStallTimeout  = 45 * time.Second
	slowChunkNearCompleteRemaining     = 256 * 1024
	slowChunkShortRemainingSeconds     = 30
)

type SlowChunkDetect string

const (
	SlowChunkDetectStall    SlowChunkDetect = "stall"
	SlowChunkDetectRelative SlowChunkDetect = "relative"
	SlowChunkDetectAbsolute SlowChunkDetect = "absolute"
)

type SlowChunkPhase string

const (
	SlowChunkPhaseNetwork       SlowChunkPhase = "network"
	SlowChunkPhaseBandwidthWait SlowChunkPhase = "bandwidth-wait"
	SlowChunkPhaseDiskWrite     SlowChunkPhase = "disk-write"
	SlowChunkPhaseProcessing    SlowChunkPhase = "processing"
)

type SlowChunkRegistration struct {
	FileID                  string
	ChunkIndex              int
	ChunkSize               int64
	CohortKey               string
	InitialTransferredBytes int64
	AttemptContext          context.Context
	AttemptCancel           context.CancelFunc
	SlowReconnects          int
	AllowAbsoluteAbort      *bool
}

type SlowChunkSnapshot struct {
	Key              string
	FileID           string
	ChunkIndex       int
	ChunkSize        int64
	CohortKey        string
	StartedAt        time.Time
	LastProgressAt   time.Time
	TransferredBytes int64
	SlowReconnects   int
	SlowTickCount    int
	AbortedSlowChunk bool
	Detect           SlowChunkDetect
	ChunkSpeedBPS    float64
	PeerMedianBPS    float64
	Phase            SlowChunkPhase
	PhaseStartedAt   time.Time
	AllowAbsolute    bool
}

type SlowChunkOptions struct {
	Now           func() time.Time
	DisableTicker bool
}

type byteSample struct {
	at    time.Time
	bytes int64
}

type slowChunkEntry struct {
	SlowChunkSnapshot
	samples        []byteSample
	attemptContext context.Context
	attemptCancel  context.CancelFunc
}

type slowCandidate struct {
	entry      *slowChunkEntry
	speed      *float64
	peerMedian float64
	detect     SlowChunkDetect
}

type SlowChunkMonitor struct {
	mu            sync.Mutex
	entries       map[string]*slowChunkEntry
	sequence      uint64
	now           func() time.Time
	disableTicker bool
	stopTicker    chan struct{}
	tickerDone    chan struct{}
}

func NewSlowChunkMonitor() *SlowChunkMonitor {
	return NewSlowChunkMonitorWithOptions(SlowChunkOptions{})
}

func NewSlowChunkMonitorWithOptions(options SlowChunkOptions) *SlowChunkMonitor {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &SlowChunkMonitor{
		entries:       make(map[string]*slowChunkEntry),
		now:           now,
		disableTicker: options.DisableTicker,
	}
}

func (m *SlowChunkMonitor) Register(input SlowChunkRegistration) SlowChunkSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.sequence++
	cohortKey := input.CohortKey
	if cohortKey == "" {
		cohortKey = input.FileID
	}
	initial := max(int64(0), min(input.ChunkSize, input.InitialTransferredBytes))
	allowAbsolute := true
	if input.AllowAbsoluteAbort != nil {
		allowAbsolute = *input.AllowAbsoluteAbort
	}
	attemptContext := input.AttemptContext
	if attemptContext == nil {
		attemptContext = context.Background()
	}
	key := input.FileID + ":" + strconv.Itoa(input.ChunkIndex) + ":" + strconv.FormatUint(m.sequence, 10)
	entry := &slowChunkEntry{
		SlowChunkSnapshot: SlowChunkSnapshot{
			Key:              key,
			FileID:           input.FileID,
			ChunkIndex:       input.ChunkIndex,
			ChunkSize:        input.ChunkSize,
			CohortKey:        cohortKey,
			StartedAt:        now,
			LastProgressAt:   now,
			TransferredBytes: initial,
			SlowReconnects:   input.SlowReconnects,
			Phase:            SlowChunkPhaseNetwork,
			PhaseStartedAt:   now,
			AllowAbsolute:    allowAbsolute,
		},
		samples:        []byteSample{{at: now, bytes: initial}},
		attemptContext: attemptContext,
		attemptCancel:  input.AttemptCancel,
	}
	m.entries[key] = entry
	m.ensureTickerLocked()
	return entry.SlowChunkSnapshot
}

func (m *SlowChunkMonitor) RecordSample(key string, transferredBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil {
		return
	}
	now := m.now()
	normalized := max(entry.TransferredBytes, transferredBytes)
	if normalized > entry.TransferredBytes {
		entry.LastProgressAt = now
	}
	entry.TransferredBytes = normalized
	entry.samples = append(entry.samples, byteSample{at: now, bytes: normalized})
	cutoff := now.Add(-slowChunkSpeedWindow)
	first := 0
	for first < len(entry.samples) && entry.samples[first].at.Before(cutoff) {
		first++
	}
	entry.samples = slices.Clone(entry.samples[first:])
}

func (m *SlowChunkMonitor) ResetProgress(key string, transferredBytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil {
		return
	}
	now := m.now()
	normalized := max(int64(0), min(entry.ChunkSize, transferredBytes))
	entry.TransferredBytes = normalized
	entry.LastProgressAt = now
	entry.samples = []byteSample{{at: now, bytes: normalized}}
	entry.SlowTickCount = 0
}

func (m *SlowChunkMonitor) SetPhase(key string, phase SlowChunkPhase) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil || entry.Phase == phase {
		return
	}
	now := m.now()
	entry.Phase = phase
	entry.PhaseStartedAt = now
	entry.SlowTickCount = 0
	if phase == SlowChunkPhaseNetwork {
		entry.LastProgressAt = now
		entry.samples = []byteSample{{at: now, bytes: entry.TransferredBytes}}
	}
}

func (m *SlowChunkMonitor) Unregister(key string) {
	m.mu.Lock()
	delete(m.entries, key)
	if len(m.entries) == 0 {
		m.stopTickerLocked()
	}
	m.mu.Unlock()
}

func (m *SlowChunkMonitor) Get(key string) (SlowChunkSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[key]
	if !ok {
		return SlowChunkSnapshot{}, false
	}
	return entry.SlowChunkSnapshot, true
}

func (m *SlowChunkMonitor) EvaluateNow() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evaluateLocked(m.now())
}

func (m *SlowChunkMonitor) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.entries = make(map[string]*slowChunkEntry)
	m.stopTickerLocked()
	done := m.tickerDone
	m.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (m *SlowChunkMonitor) ensureTickerLocked() {
	if m.disableTicker || m.stopTicker != nil {
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	m.stopTicker = stop
	m.tickerDone = done
	go func() {
		defer close(done)
		ticker := time.NewTicker(slowChunkCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				m.EvaluateNow()
			}
		}
	}()
}

func (m *SlowChunkMonitor) stopTickerLocked() {
	if m.stopTicker == nil {
		return
	}
	close(m.stopTicker)
	m.stopTicker = nil
}

func (m *SlowChunkMonitor) evaluateLocked(now time.Time) {
	scored := make(map[*slowChunkEntry]*float64, len(m.entries))
	for _, entry := range m.entries {
		scored[entry] = speedFromSamples(entry.samples, now)
	}
	stallCandidates := make([]slowCandidate, 0)
	relativeCandidates := make([]slowCandidate, 0)
	absoluteCandidates := make([]slowCandidate, 0)
	for entry, speed := range scored {
		if entry.SlowReconnects >= SlowChunkMaxReconnects {
			entry.SlowTickCount = 0
			continue
		}
		if entry.AbortedSlowChunk || entry.attemptContext.Err() != nil {
			continue
		}
		if entry.Phase != SlowChunkPhaseNetwork {
			entry.SlowTickCount = 0
			continue
		}
		if now.Sub(entry.StartedAt) < slowChunkMinObserve {
			entry.SlowTickCount = 0
			continue
		}
		remaining := max(int64(0), entry.ChunkSize-entry.TransferredBytes)
		completion := float64(0)
		if entry.ChunkSize > 0 {
			completion = float64(entry.TransferredBytes) / float64(entry.ChunkSize)
		}
		nearComplete := entry.ChunkSize > 0 && (completion >= slowChunkNearCompleteRatio || (completion >= 0.5 && remaining <= slowChunkNearCompleteRemaining))
		stallTimeout := slowChunkStallTimeout
		if nearComplete {
			stallTimeout = slowChunkNearCompleteStallTimeout
		}
		if now.Sub(entry.LastProgressAt) >= stallTimeout {
			entry.SlowTickCount = 0
			stallCandidates = append(stallCandidates, slowCandidate{entry: entry, speed: speed, detect: SlowChunkDetectStall})
			continue
		}
		estimatedRemaining := math.Inf(1)
		if speed != nil && *speed > 0 {
			estimatedRemaining = float64(remaining) / *speed
		}
		if estimatedRemaining <= slowChunkShortRemainingSeconds {
			entry.SlowTickCount = 0
			continue
		}
		if speed == nil || *speed >= slowChunkMinAbsoluteBPS {
			entry.SlowTickCount = 0
			continue
		}
		peerMedian := peerMedianBPS(scored, entry)
		entry.SlowTickCount++
		if peerMedian > 0 && *speed < peerMedian*SlowChunkThresholdRatio && entry.SlowTickCount >= slowChunkRequiredRelativeSlowTicks {
			relativeCandidates = append(relativeCandidates, slowCandidate{entry: entry, speed: speed, peerMedian: peerMedian, detect: SlowChunkDetectRelative})
			continue
		}
		if entry.AllowAbsolute && entry.SlowTickCount >= slowChunkRequiredAbsoluteSlowTicks {
			absoluteCandidates = append(absoluteCandidates, slowCandidate{entry: entry, speed: speed, peerMedian: peerMedian, detect: SlowChunkDetectAbsolute})
		}
	}
	pick := oldestCandidate(stallCandidates)
	if pick == nil {
		pick = slowestCandidate(relativeCandidates)
	}
	if pick == nil {
		pick = slowestCandidate(absoluteCandidates)
	}
	if pick == nil {
		return
	}
	pick.entry.AbortedSlowChunk = true
	pick.entry.Detect = pick.detect
	if pick.speed != nil {
		pick.entry.ChunkSpeedBPS = *pick.speed
	}
	pick.entry.PeerMedianBPS = pick.peerMedian
	if pick.entry.attemptCancel != nil {
		pick.entry.attemptCancel()
	}
}

func speedFromSamples(samples []byteSample, now time.Time) *float64 {
	window := make([]byteSample, 0, len(samples))
	for _, sample := range samples {
		if now.Sub(sample.at) <= slowChunkSpeedWindow {
			window = append(window, sample)
		}
	}
	if len(window) < 2 {
		return nil
	}
	first := window[0]
	last := window[len(window)-1]
	elapsed := last.at.Sub(first.at)
	if elapsed < slowChunkMinSpeedSampleSpan {
		return nil
	}
	speed := max(float64(0), float64(last.bytes-first.bytes)/elapsed.Seconds())
	return &speed
}

func peerMedianBPS(scored map[*slowChunkEntry]*float64, candidate *slowChunkEntry) float64 {
	positive := func(sameFile bool) []float64 {
		values := make([]float64, 0)
		for entry, speed := range scored {
			if entry == candidate || entry.CohortKey != candidate.CohortKey || entry.Phase != SlowChunkPhaseNetwork || entry.AbortedSlowChunk || speed == nil || *speed <= 0 {
				continue
			}
			if sameFile && entry.FileID != candidate.FileID {
				continue
			}
			values = append(values, *speed)
		}
		return values
	}
	sameFile := positive(true)
	if len(sameFile) >= slowChunkMinPeers {
		return median(sameFile)
	}
	global := positive(false)
	if len(global) < slowChunkMinPeers {
		return 0
	}
	return median(global)
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	slices.Sort(values)
	mid := len(values) / 2
	if len(values)%2 == 0 {
		return (values[mid-1] + values[mid]) / 2
	}
	return values[mid]
}

func oldestCandidate(candidates []slowCandidate) *slowCandidate {
	var best *slowCandidate
	for i := range candidates {
		if best == nil || candidates[i].entry.StartedAt.Before(best.entry.StartedAt) {
			best = &candidates[i]
		}
	}
	return best
}

func slowestCandidate(candidates []slowCandidate) *slowCandidate {
	var best *slowCandidate
	for i := range candidates {
		if best == nil || candidateSpeed(candidates[i]) < candidateSpeed(*best) {
			best = &candidates[i]
		}
	}
	return best
}

func candidateSpeed(candidate slowCandidate) float64 {
	if candidate.speed == nil {
		return math.Inf(1)
	}
	return *candidate.speed
}

func SlowReconnectDelay() time.Duration {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return slowChunkReconnectDelay
	}
	jitterMilliseconds := binary.LittleEndian.Uint64(data[:]) % uint64(slowChunkReconnectJitter/time.Millisecond+1)
	jitter := time.Duration(jitterMilliseconds) * time.Millisecond
	return slowChunkReconnectDelay + jitter
}

func SleepWithAbort(ctx context.Context, duration time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
