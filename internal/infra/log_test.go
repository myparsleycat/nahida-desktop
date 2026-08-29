package infra

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogFiltersByLevelAndWritesJSON(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "desktop.log")
	l := NewLogWithOptions(LogOptions{Dest: dest})
	defer func() { _ = l.Close() }()

	l.SetLevel("error")
	l.Info("quiet", "Boot")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		data, _ := os.ReadFile(dest)
		if len(bytes.TrimSpace(data)) != 0 {
			t.Fatalf("Info wrote %q", data)
		}
	}

	l.Error("boom", "Boot")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	lines := nonEmptyLines(data)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(lines), data)
	}
	var rec map[string]any
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatalf("json: %v (%s)", err, lines[0])
	}
	msg, _ := rec["msg"].(string)
	if !strings.Contains(msg, "boom") || !strings.Contains(msg, "[Boot]") {
		t.Fatalf("msg = %q", msg)
	}
	if rec["level"] != "error" {
		t.Fatalf("level = %#v", rec["level"])
	}
	if timestamp, ok := rec["time"].(float64); !ok || timestamp <= 0 {
		t.Fatalf("time = %#v, want epoch milliseconds", rec["time"])
	}

	l.SetLevel("debug")
	l.Debug("hello", "Boot")
	data, err = os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest after debug: %v", err)
	}
	lines = nonEmptyLines(data)
	if len(lines) != 2 {
		t.Fatalf("got %d lines after Debug, want 2: %q", len(lines), data)
	}
	var debugRec map[string]any
	if err := json.Unmarshal(lines[1], &debugRec); err != nil {
		t.Fatalf("debug json: %v", err)
	}
	debugMsg, _ := debugRec["msg"].(string)
	if !strings.Contains(debugMsg, "hello") {
		t.Fatalf("debug msg = %q", debugMsg)
	}
}

func TestLogRotatesBySizeCompressesAndRetainsThreeArchives(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "desktop.log")
	l := NewLogWithOptions(LogOptions{Dest: dest, MaxSize: 180, MaxFiles: 3})
	l.SetLevel("debug")
	for index := range 6 {
		l.Debug(strings.Repeat(string(rune('a'+index)), 80), "Rotate")
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("active log: %v", err)
	}
	for index := 1; index <= 3; index++ {
		archive := rotatedLogPath(dest, index)
		data := readGzipFile(t, archive)
		if len(nonEmptyLines(data)) != 1 {
			t.Fatalf("archive %d = %q, want one JSON line", index, data)
		}
	}
	if _, err := os.Stat(rotatedLogPath(dest, 4)); !os.IsNotExist(err) {
		t.Fatalf("fourth archive retained: %v", err)
	}
}

func TestLogRotatesAfterSevenDays(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	dest := filepath.Join(t.TempDir(), "desktop.log")
	l := NewLogWithOptions(LogOptions{Dest: dest, MaxSize: 1 << 20, Now: func() time.Time { return now }})
	l.SetLevel("info")
	l.Info("before", "Rotate")
	now = now.Add(8 * 24 * time.Hour)
	l.Info("after", "Rotate")
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	archive := readGzipFile(t, rotatedLogPath(dest, 1))
	if !bytes.Contains(archive, []byte("before")) {
		t.Fatalf("archive = %q, want pre-rotation line", archive)
	}
	active, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read active log: %v", err)
	}
	if !bytes.Contains(active, []byte("after")) {
		t.Fatalf("active = %q, want post-rotation line", active)
	}
}

func readGzipFile(t *testing.T, path string) []byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("gzip %s: %v", path, err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestLogSetLevelInvalidIsError(t *testing.T) {
	t.Parallel()
	for _, level := range []string{"", "verbose", "ERROR"} {
		l := NewLog()
		l.SetLevel(level)
		if l.Level() != "error" {
			t.Fatalf("SetLevel(%q) = %q, want error", level, l.Level())
		}
	}
}

func TestLogDevWritesEveryLevelToConsoleAndSkipsFile(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "desktop.log")
	var buf bytes.Buffer
	l := NewLogWithOptions(LogOptions{Dest: dest, Writer: &buf, Dev: true})
	defer func() { _ = l.Close() }()

	l.SetLevel("error")
	l.Info("starting", "StaticGlb.loadForViewer")
	l.Debug("detail", "Boot")
	l.Error("boom", "Boot")

	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		data, _ := os.ReadFile(dest)
		if len(bytes.TrimSpace(data)) != 0 {
			t.Fatalf("dev mode wrote file %q", data)
		}
	}
	got := buf.String()
	if !strings.Contains(got, "[StaticGlb.loadForViewer] starting") {
		t.Fatalf("missing info console line: %q", got)
	}
	if !strings.Contains(got, "[Boot] detail") || !strings.Contains(got, "[Boot] boom") {
		t.Fatalf("missing unfiltered console lines: %q", got)
	}
	if strings.Contains(got, `"level"`) {
		t.Fatalf("dev console wrote JSON: %q", got)
	}
}

func TestLogNoDestDoesNotCreateDesktopLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var buf bytes.Buffer
	l := NewLogWithOptions(LogOptions{Writer: &buf})
	l.SetLevel("error")
	l.Error("x", "")
	if _, err := os.Stat(filepath.Join(dir, "desktop.log")); !os.IsNotExist(err) {
		t.Fatalf("created desktop.log: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected stderr/writer fallback")
	}
}

func nonEmptyLines(data []byte) [][]byte {
	raw := bytes.Split(data, []byte("\n"))
	out := make([][]byte, 0, len(raw))
	for _, line := range raw {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}
