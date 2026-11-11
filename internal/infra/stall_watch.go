package infra

import (
	"cmp"
	"maps"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	stallWarnAfter   = 30 * time.Second
	stallDumpAfter   = 2 * time.Minute
	stallDumpLimit   = 64
	stallFrameLimit  = 16
	stallStackLimit  = 16 << 20
	stallModulePath  = "nahida.live/desktop/"
	stallPackagePath = stallModulePath + "internal/"
)

// lastStallDump keeps watches nested in one operation from logging the same goroutines twice.
var lastStallDump atomic.Int64

// StallWatch logs a stage of an operation that runs past a threshold. A wait that never fails, such as a lock
// that is never released, otherwise leaves nothing in desktop.log. A stage still running after two minutes also
// logs the stack of the goroutine that runs the stage, which names the lock or channel it waits on, and the
// other parked goroutines in application code, among which is usually the one holding it.
type StallWatch struct {
	log       *Log
	where     string
	operation string
	fields    map[string]any
	warnAfter time.Duration
	dumpAfter time.Duration

	mu         sync.Mutex
	stage      string
	goroutine  string
	started    time.Time
	generation int
	timers     [2]*time.Timer
	stalled    bool
}

func NewStallWatch(log *Log, where, operation string, fields map[string]any) *StallWatch {
	return &StallWatch{
		log: log, where: where, operation: operation, fields: fields,
		warnAfter: stallWarnAfter, dumpAfter: stallDumpAfter,
	}
}

// Stage ends the current stage and starts timing the next one on the calling goroutine.
func (w *StallWatch) Stage(stage string) {
	w.start(stage, true)
}

// ExternalStage is Stage for a stage that waits on the user or another program, such as a UAC prompt or a game
// starting, and may take minutes when nothing is wrong. Its stall is logged without the goroutine dump.
func (w *StallWatch) ExternalStage(stage string) {
	w.start(stage, false)
}

func (w *StallWatch) start(stage string, dump bool) {
	goroutine := currentGoroutineID()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finishLocked()

	w.stage, w.goroutine, w.started = stage, goroutine, time.Now()
	generation := w.generation
	w.timers[0] = time.AfterFunc(w.warnAfter, func() { w.fire(generation, false) })
	if dump {
		w.timers[1] = time.AfterFunc(w.dumpAfter, func() { w.fire(generation, true) })
	}
}

// Stop ends the current stage without starting another. A later Stage starts timing again.
func (w *StallWatch) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finishLocked()
}

// finishLocked also logs how long a stalled stage took, so a slow stage can be told apart from a hung one.
// The generation moves on so a timer that already fired and waits for the lock logs nothing.
func (w *StallWatch) finishLocked() {
	for i, timer := range w.timers {
		if timer != nil {
			timer.Stop()
			w.timers[i] = nil
		}
	}
	w.generation++
	if w.stalled {
		w.log.Warn(w.record("finished"), w.where)
		w.stalled = false
	}
}

func (w *StallWatch) fire(generation int, dump bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if generation != w.generation {
		return
	}
	w.stalled = true
	fields := w.record("stalled")
	if dump && claimStallDump(w.dumpAfter) {
		fields["stageGoroutine"], fields["goroutines"] = summarizeGoroutines(allGoroutineStacks(), w.goroutine)
	}
	w.log.Warn(fields, w.where)
}

func (w *StallWatch) record(status string) map[string]any {
	fields := make(map[string]any, len(w.fields)+4)
	maps.Copy(fields, w.fields)
	fields["operation"] = w.operation
	fields["stage"] = w.stage
	fields["status"] = status
	fields["elapsedMs"] = time.Since(w.started).Milliseconds()
	return fields
}

func claimStallDump(interval time.Duration) bool {
	now := time.Now().UnixNano()
	last := lastStallDump.Load()
	if last != 0 && time.Duration(now-last) < interval {
		return false
	}
	return lastStallDump.CompareAndSwap(last, now)
}

func currentGoroutineID() string {
	var buf [64]byte
	header := string(buf[:runtime.Stack(buf[:], false)])
	fields := strings.Fields(header)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func allGoroutineStacks() string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= stallStackLimit {
			return string(buf[:n])
		}
		buf = make([]byte, 2*len(buf))
	}
}

// summarizeGoroutines returns the stack of the goroutine with the given id and the other parked goroutines
// that run application code, one line each with the innermost frame first. The others in the packages the stage
// waits in come first, innermost package first, since the one holding its lock is usually among them. The
// runtime cannot read the stack of a goroutine running on another thread, so such a stage goroutine is reported
// by its "[running]" header alone.
func summarizeGoroutines(dump, id string) (string, []string) {
	type goroutine struct {
		summary  string
		packages []string
	}
	var stage goroutine
	var others []goroutine
	for block := range strings.SplitSeq(dump, "\n\n") {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		header := strings.Fields(lines[0])
		if len(header) < 3 {
			continue
		}
		summary, packages := summarizeStack(lines)
		switch {
		case header[1] == id:
			stage = goroutine{summary, packages}
		case len(lines) >= 3 && !strings.HasPrefix(header[2], "[running") && len(packages) > 0:
			others = append(others, goroutine{summary, packages})
		}
	}

	rank := func(g goroutine) int {
		for i, pkg := range stage.packages {
			if slices.Contains(g.packages, pkg) {
				return i
			}
		}
		return len(stage.packages)
	}
	slices.SortStableFunc(others, func(a, b goroutine) int { return cmp.Compare(rank(a), rank(b)) })
	summaries := []string{}
	for _, g := range others[:min(len(others), stallDumpLimit)] {
		summaries = append(summaries, g.summary)
	}
	return stage.summary, summaries
}

// summarizeStack returns one goroutine's stack on one line and the application packages it runs in, innermost
// first.
func summarizeStack(lines []string) (string, []string) {
	frames := []string{strings.TrimSuffix(lines[0], ":")}
	var packages []string
	for i := 1; i+1 < len(lines); i += 2 {
		if _, function, ok := strings.Cut(lines[i], stallModulePath); ok {
			if pkg, _, _ := strings.Cut(function, "."); !slices.Contains(packages, pkg) {
				packages = append(packages, pkg)
			}
		}
		if len(frames) > stallFrameLimit {
			continue
		}

		function := strings.Replace(lines[i], stallPackagePath, "", 1)
		if open := strings.LastIndexByte(function, '('); open > 0 && strings.HasSuffix(function, ")") {
			function = function[:open]
		}
		location, _, _ := strings.Cut(strings.TrimSpace(lines[i+1]), " +0x")
		location = location[strings.LastIndexAny(location, `/\`)+1:]
		frames = append(frames, function+" "+location)
	}
	return strings.Join(frames, " | "), packages
}
