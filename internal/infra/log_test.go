package infra

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogFiltersByLevelAndWritesPlainText(t *testing.T) {
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
	line := string(lines[0])
	if strings.Contains(line, `"level"`) || strings.HasPrefix(strings.TrimSpace(line), "{") {
		t.Fatalf("wrote JSON: %q", line)
	}
	if !strings.Contains(line, " ERROR ") || !strings.Contains(line, "[Boot] boom") {
		t.Fatalf("line = %q", line)
	}
	stamp := line
	if i := strings.Index(line, " ERROR "); i >= 0 {
		stamp = line[:i]
	}
	if _, parseErr := time.Parse(logTimeLayout, stamp); parseErr != nil {
		t.Fatalf("timestamp: %v (%q)", parseErr, line)
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
	debugLine := string(lines[1])
	if !strings.Contains(debugLine, " DEBUG ") || !strings.Contains(debugLine, "[Boot] hello") {
		t.Fatalf("debug line = %q", debugLine)
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
			t.Fatalf("archive %d = %q, want one log line", index, data)
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

func TestRedactUserPathsMasksHomePrefix(t *testing.T) {
	t.Parallel()

	home := `C:\Users\사용자`
	msg := `[TogglePersist] Error updating mod ini C:\Users\사용자\AppData\Roaming\XXMI Launcher\ZZMI\mods\Character\SampleMod\mod.ini: GetFileAttributesEx C:\Users\사용자\AppData\Roaming\XXMI Launcher\ZZMI\mods\Character\SampleMod\mod.ini: The system cannot find the path specified.`
	got := redactUserPaths(msg, prepareHomeNeedles([]string{home}))
	want := `[TogglePersist] Error updating mod ini %USERPROFILE%\AppData\Roaming\XXMI Launcher\ZZMI\mods\Character\SampleMod\mod.ini: GetFileAttributesEx %USERPROFILE%\AppData\Roaming\XXMI Launcher\ZZMI\mods\Character\SampleMod\mod.ini: The system cannot find the path specified.`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRedactUserPathsAcceptsSlashAndCaseVariants(t *testing.T) {
	t.Parallel()

	home := `C:\Users\사용자`
	cases := []struct {
		in, want string
	}{
		{`c:/users/사용자/AppData/Roaming/xxmi.ini`, `%USERPROFILE%/AppData/Roaming/xxmi.ini`},
		{`C:\USERS\사용자\mods\a.ini`, `%USERPROFILE%\mods\a.ini`},
		{`c:\Users\사용자`, `%USERPROFILE%`},
		{`prefix C:\Users\사용자`, `prefix %USERPROFILE%`},
	}
	for _, tc := range cases {
		got := redactUserPaths(tc.in, prepareHomeNeedles([]string{home}))
		if got != tc.want {
			t.Fatalf("in %q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactUserPathsKeepsNonBoundaryMatches(t *testing.T) {
	t.Parallel()

	home := `C:\Users\사용자`
	in := `C:\Users\사용자foo\mod.ini and C:\Users\neighbor\file.ini`
	got := redactUserPaths(in, prepareHomeNeedles([]string{home}))
	if got != in {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestRedactUserPathsLeavesUnrelatedText(t *testing.T) {
	t.Parallel()

	in := "boom without a path"
	got := redactUserPaths(in, prepareHomeNeedles([]string{`C:\Users\사용자`}))
	if got != in {
		t.Fatalf("got %q, want unchanged", got)
	}
}

func TestRedactUserPathsHandlesJSONEscapedSeparators(t *testing.T) {
	t.Parallel()

	home := `C:\Users\사용자`
	in := `{"path":"C:\\Users\\사용자\\AppData\\Roaming\\xxmi.ini"}`
	got := redactUserPaths(in, prepareHomeNeedles([]string{home}))
	want := `{"path":"%USERPROFILE%\\AppData\\Roaming\\xxmi.ini"}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeLogLineRedactsErrorAndStructuredMsg(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	path := filepath.Join(home, "AppData", "Roaming", "xxmi.ini")
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

	line := encodeLogLine(now, "error", errors.New("update "+path), "TogglePersist")
	got := string(bytes.TrimSpace(line))
	if strings.Contains(got, home) {
		t.Fatalf("line still contains home: %q", got)
	}
	if !strings.Contains(got, "%USERPROFILE%") {
		t.Fatalf("line missing token: %q", got)
	}
	if !strings.HasPrefix(got, "2026-08-30 12:00:00.000 ERROR [TogglePersist] update ") {
		t.Fatalf("line = %q", got)
	}

	structured := encodeLogLine(now, "error", map[string]any{"path": path}, "Mod")
	structuredLine := string(bytes.TrimSpace(structured))
	if strings.Contains(structuredLine, home) || strings.Contains(structuredLine, strings.ReplaceAll(home, `\`, `\\`)) {
		t.Fatalf("structured line still contains home: %q", structuredLine)
	}
	if !strings.Contains(structuredLine, "%USERPROFILE%") {
		t.Fatalf("structured line missing token: %q", structuredLine)
	}
	if !strings.HasPrefix(structuredLine, "2026-08-30 12:00:00.000 ERROR [Mod] ") {
		t.Fatalf("structured line = %q", structuredLine)
	}

	console := encodeDevConsoleLine("failed "+path, "Boot")
	if bytes.Contains(console, []byte(home)) {
		t.Fatalf("console still contains home: %s", console)
	}
	if !bytes.Contains(console, []byte("%USERPROFILE%")) {
		t.Fatalf("console missing token: %s", console)
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
