package tools

import (
	"context"
	"testing"
)

func TestConcurrentFixToolRunReportsThroughLogEvent(t *testing.T) {
	t.Parallel()
	var events []FixToolLogEvent
	service := NewWithOptions(Options{EventEmit: func(name string, data ...any) {
		if name == fixToolLogEvent && len(data) == 1 {
			if event, ok := data[0].(FixToolLogEvent); ok {
				events = append(events, event)
			}
		}
	}})
	run, _, err := service.beginScriptRun(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer service.finishScriptRun(run)
	if err := service.RunScript(context.Background(), "script", t.TempDir()); err != nil {
		t.Fatalf("RunScript should report through ftm:log, got %v", err)
	}
	if len(events) != 1 || events[0].Message != "Error: Another process is running." {
		t.Fatalf("events = %#v", events)
	}
}
