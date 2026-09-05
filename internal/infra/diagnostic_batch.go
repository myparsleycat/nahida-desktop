package infra

import (
	"errors"
	"sync"
)

// DiagnosticBatch aggregates partial failures without retaining every file.
//
//wails:ignore
type DiagnosticBatch struct {
	mu     sync.Mutex
	count  int
	causes []error
}

//wails:ignore
func (b *DiagnosticBatch) Add(err error) {
	if err == nil || IsCancellationError(err) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	if len(b.causes) < 10 {
		b.causes = append(b.causes, err)
	}
}

//wails:ignore
func (b *DiagnosticBatch) Report(log *Log, where, operation string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count == 0 {
		return
	}
	_ = ReportError(log, errors.Join(b.causes...), where, Diagnostic{Severity: DiagnosticWarn, Operation: operation, Stage: "partial-result", Fields: map[string]any{"failureCount": b.count, "omittedCount": b.count - len(b.causes)}})
}
