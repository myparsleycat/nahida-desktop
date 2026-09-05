package infra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticThrottleAndBatch(t *testing.T) {
	var output bytes.Buffer
	now := time.Unix(1000, 0)
	log := NewLogWithOptions(LogOptions{Writer: &output, DisableFile: true, Now: func() time.Time { return now }})
	var throttle DiagnosticThrottle
	diagnostic := Diagnostic{Operation: "watch", Stage: "read", Severity: DiagnosticWarn}
	throttle.Report(log, errors.New("denied"), "Test", diagnostic)
	throttle.Report(log, errors.New("denied"), "Test", diagnostic)
	if strings.Count(output.String(), "[Test]") != 1 {
		t.Fatal(output.String())
	}
	now = now.Add(5 * time.Minute)
	throttle.Report(log, errors.New("denied"), "Test", diagnostic)
	if !strings.Contains(output.String(), `"suppressedCount":1`) {
		t.Fatal(output.String())
	}
	throttle.Report(log, errors.New("disconnected"), "Test", diagnostic)
	if strings.Count(output.String(), "[Test]") != 3 {
		t.Fatal(output.String())
	}
	throttle.Report(log, nil, "Test", diagnostic)
	throttle.Report(log, errors.New("disconnected"), "Test", diagnostic)
	if strings.Count(output.String(), "[Test]") != 4 {
		t.Fatal(output.String())
	}
	output.Reset()
	var batch DiagnosticBatch
	batch.Add(context.Canceled)
	for index := range 13 {
		batch.Add(fmt.Errorf("failure-%02d", index))
	}
	batch.Report(log, "Test", "scan")
	for _, want := range []string{`"failureCount":13`, `"omittedCount":3`, "failure-09"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %s: %s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "failure-10") {
		t.Fatal("batch retained too many causes")
	}
}

func TestLogSinkFailureKeepsRecordAndReportsOnce(t *testing.T) {
	for _, stage := range []string{"open", "rotate", "write"} {
		t.Run(stage, func(t *testing.T) {
			var output bytes.Buffer
			dest := filepath.Join(t.TempDir(), "desktop.log")
			log := NewLogWithOptions(LogOptions{Dest: dest, Writer: &output})
			t.Cleanup(func() { _ = log.Close() })
			switch stage {
			case "open":
				if err := os.Mkdir(dest, 0o700); err != nil {
					t.Fatal(err)
				}
			case "write":
				if err := os.WriteFile(dest, []byte("existing\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				file, err := os.Open(dest)
				if err != nil {
					t.Fatal(err)
				}
				log.file, log.rotateAt = file, time.Now().Add(time.Hour)
			default:
				log.Error("first", "Test")
				log.maxSize = 1
				// Rotation cannot remove a nonempty directory as the archive target.
				blocked := rotatedLogPath(dest, 3)
				if err := os.Mkdir(blocked, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(blocked, "child"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			log.Error("original failure", "Test")
			log.Error("second failure", "Test")
			if strings.Count(output.String(), `"operation":"log-file"`) != 1 {
				t.Fatal(output.String())
			}
			for _, want := range []string{`"stage":"` + stage + `"`, "original failure", "second failure"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %q: %s", want, output.String())
				}
			}
		})
	}
}
