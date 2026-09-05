package infra

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// DiagnosticThrottle belongs to one periodic operation, not the global logger.
//
//wails:ignore
type DiagnosticThrottle struct {
	mu      sync.Mutex
	entries map[string]diagnosticEpisode
}

type diagnosticEpisode struct {
	key        string
	last       time.Time
	suppressed int
}

// Report records a changed failure immediately and repeats every five minutes.
// A successful iteration resets the episode without emitting an extra log.
//
//wails:ignore
func (t *DiagnosticThrottle) Report(log *Log, err error, where string, diagnostic Diagnostic) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err == nil {
		clear(t.entries)
		return
	}
	if log == nil || IsCancellationError(err) {
		return
	}
	now := log.now()
	identity := fmt.Sprintf("%s:%s:%v", diagnostic.Operation, diagnostic.Stage, diagnostic.Fields["path"])
	causes, _, _ := collectDiagnosticCauses(err, Diagnostic{}, false)
	encoded, marshalErr := json.Marshal(causes)
	key := string(encoded)
	if marshalErr != nil {
		key = err.Error()
	}
	if t.entries == nil {
		t.entries = make(map[string]diagnosticEpisode)
	}
	episode := t.entries[identity]
	if key == episode.key && now.Sub(episode.last) < 5*time.Minute {
		episode.suppressed++
		t.entries[identity] = episode
		return
	}
	fields := make(map[string]any, len(diagnostic.Fields)+1)
	for name, value := range diagnostic.Fields {
		fields[name] = value
	}
	fields["suppressedCount"] = episode.suppressed
	diagnostic.Fields = fields
	_ = ReportError(log, err, where, diagnostic)
	if len(t.entries) >= 64 {
		var oldest string
		var oldestTime time.Time
		for name, entry := range t.entries {
			if oldest == "" || entry.last.Before(oldestTime) {
				oldest, oldestTime = name, entry.last
			}
		}
		delete(t.entries, oldest)
	}
	t.entries[identity] = diagnosticEpisode{key: key, last: now}
}
