package infra

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	defaultLogLevel        = "warn"
	desktopLogName         = "desktop.log"
	defaultLogMaxSize      = 10 * 1024 * 1024
	defaultLogRotateEvery  = 7 * 24 * time.Hour
	defaultLogRotatedFiles = 3
	userProfileToken       = "%USERPROFILE%"
)

var (
	homeNeedlesOnce      sync.Once
	homeNeedles          []string
	bearerPattern        = regexp.MustCompile(`(?i)\bbearer[ \t]+[a-z0-9._~+/=-]+`)
	logURLPattern        = regexp.MustCompile(`https?://[^\s"<>]+`)
	logJSONStringPattern = regexp.MustCompile(`"(?:\\.|[^"\\])*"`)
	urlUserInfoPattern   = regexp.MustCompile(`(?i)(https?://)[^/@\s"]+@`)
	jsonSecretPattern    = regexp.MustCompile(`(?i)("(?:authorization|proxy-authorization|cookie|set-cookie|rmc|token|access[_-]?token|refresh[_-]?token|password|secret|credentials|api[_-]?key|signature|state|stateResponse|x-amz-signature|x-goog-signature)"[ \t]*:[ \t]*)("(?:\\.|[^"\\])*")`)
	authHeaderPattern    = regexp.MustCompile(`(?im)^([ \t]*(?:authorization|proxy-authorization)[ \t]*:[ \t]*)([^\r\n]*)`)
	cookieHeaderPattern  = regexp.MustCompile(`(?im)^([ \t]*(?:cookie|set-cookie)[ \t]*:[ \t]*)([^\r\n]*)`)
	plainSecretPattern   = regexp.MustCompile(`(?i)\b(authorization|proxy-authorization|cookie|set-cookie|rmc|token|access[_-]?token|refresh[_-]?token|password|secret|credentials|api[_-]?key|signature|state|stateResponse|x-amz-signature|x-goog-signature)([ \t]*[=:][ \t]*)([^&\s,;}"']+)`)
)

var levelPriority = map[string]int{
	"trace": 10,
	"debug": 20,
	"info":  30,
	"warn":  40,
	"error": 50,
	"fatal": 60,
}

// Log is the Wails-facing logger. It is a small stdlib port of the Electron
// logger's level filter, writing one plain-text line per record.
type Log struct {
	mu          sync.Mutex
	level       string
	dest        string
	file        *os.File
	writer      io.Writer
	noFile      bool
	fileErr     bool
	dev         bool
	maxSize     int64
	rotateEvery time.Duration
	rotateAt    time.Time
	maxFiles    int
	now         func() time.Time
}

// LogOptions configure an injectable dest and writer. An empty Dest means
// no desktop.log is created. Dev matches Electron is.dev: every level is
// written to the console writer and the file sink is skipped.
type LogOptions struct {
	Dest        string
	Writer      io.Writer
	DisableFile bool
	Dev         bool
	MaxSize     int64
	RotateEvery time.Duration
	MaxFiles    int
	Now         func() time.Time
}

func NewLog() *Log {
	return NewLogWithOptions(LogOptions{})
}

func NewLogWithOptions(opts LogOptions) *Log {
	l := &Log{level: defaultLogLevel}
	l.applyOptions(opts)
	return l
}

// Configure updates dest/writer without resetting the current level.
//
//wails:ignore
func (l *Log) Configure(opts LogOptions) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.closeFileLocked(); err != nil {
		l.fileFailureLocked(err, "reconfigure-close")
	}
	l.fileErr = false
	l.applyOptions(opts)
}

func (l *Log) applyOptions(opts LogOptions) {
	l.dest = opts.Dest
	l.noFile = opts.DisableFile
	l.dev = opts.Dev
	l.maxSize = opts.MaxSize
	if l.maxSize <= 0 {
		l.maxSize = defaultLogMaxSize
	}
	l.rotateEvery = opts.RotateEvery
	if l.rotateEvery <= 0 {
		l.rotateEvery = defaultLogRotateEvery
	}
	l.rotateAt = time.Time{}
	l.maxFiles = opts.MaxFiles
	if l.maxFiles <= 0 {
		l.maxFiles = defaultLogRotatedFiles
	}
	l.now = opts.Now
	if l.now == nil {
		l.now = time.Now
	}
	if opts.Writer != nil {
		l.writer = opts.Writer
	}
	if l.writer == nil {
		l.writer = os.Stderr
	}
}

//wails:ignore
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.closeFileLocked()
	if err != nil {
		l.fileFailureLocked(err, "close")
	}
	return err
}

func (l *Log) closeFileLocked() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// SetLevel updates the current level. Empty and unknown values become "warn"
// so a corrupted setting cannot silence every log entry.
//
//wails:ignore
func (l *Log) SetLevel(level string) {
	if l == nil {
		return
	}
	if _, ok := levelPriority[level]; !ok {
		level = defaultLogLevel
	}
	l.mu.Lock()
	l.level = level
	l.mu.Unlock()
}

// Level returns the current filter level.
//
//wails:ignore
func (l *Log) Level() string {
	if l == nil {
		return defaultLogLevel
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.level
}

func (l *Log) Log(level string, msg any, where string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dev {
		if l.writer != nil {
			_, _ = l.writer.Write(encodeDevConsoleLine(msg, where))
		}
		return
	}
	if !shouldWrite(level, l.level) {
		return
	}
	line := encodeLogLine(l.now(), level, msg, where)
	if l.dest != "" && !l.noFile && !l.fileErr {
		if l.file == nil {
			if err := l.openDestLocked(); err != nil {
				l.fileFailureLocked(err, "open")
			}
		}
		if l.file != nil {
			if err := l.rotateIfNeededLocked(int64(len(line))); err != nil {
				l.fileFailureLocked(errors.Join(err, l.closeFileLocked()), "rotate")
			} else if l.file != nil {
				written, err := l.file.Write(line)
				if err == nil && written == len(line) {
					return
				}
				if err == nil {
					err = io.ErrShortWrite
				}
				l.fileFailureLocked(errors.Join(err, l.closeFileLocked()), "write")
			}
		}
	}
	if l.writer != nil {
		_, _ = l.writer.Write(line)
	}
}

func (l *Log) fileFailureLocked(err error, stage string) {
	if l.fileErr {
		return
	}
	l.fileErr = true
	if l.writer != nil {
		record := map[string]any{"operation": "log-file", "stage": stage, "path": l.dest, "error": limitDiagnosticText(err.Error(), 4<<10)}
		causes, truncated, _ := collectDiagnosticCauses(err, Diagnostic{}, false)
		if len(causes) > 1 {
			record["causes"] = causes
		}
		if truncated {
			record["causesTruncated"] = true
		}
		_, _ = l.writer.Write(encodeLogLine(l.now(), "error", record, "Log"))
	}
}

func (l *Log) rotateIfNeededLocked(incoming int64) error {
	if l.file == nil || l.dest == "" {
		return nil
	}
	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	tooLarge := info.Size() > 0 && info.Size()+incoming > l.maxSize
	tooOld := info.Size() > 0 && !l.now().Before(l.rotateAt)
	if !tooLarge && !tooOld {
		return nil
	}
	if err := l.closeFileLocked(); err != nil {
		return err
	}
	if err := rotateCompressedLog(l.dest, l.maxFiles); err != nil {
		return err
	}
	return l.openDestLocked()
}

func rotateCompressedLog(path string, maxFiles int) error {
	if maxFiles <= 0 {
		return os.Remove(path)
	}
	oldest := rotatedLogPath(path, maxFiles)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return err
	}
	for index := maxFiles - 1; index >= 1; index-- {
		src := rotatedLogPath(path, index)
		dst := rotatedLogPath(path, index+1)
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	archive := rotatedLogPath(path, 1)
	temp := archive + ".tmp"
	if err := compressLogFile(path, temp); err != nil {
		return WithCause(err, os.Remove(temp))
	}
	if err := os.Remove(archive); err != nil && !os.IsNotExist(err) {
		return WithCause(err, os.Remove(temp))
	}
	if err := os.Rename(temp, archive); err != nil {
		return WithCause(err, os.Remove(temp))
	}
	return os.Remove(path)
}

func rotatedLogPath(path string, index int) string {
	return fmt.Sprintf("%s.%d.gz", path, index)
}

func compressLogFile(src, dst string) (resultErr error) {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, input.Close()) }()
	output, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, output.Close()) }()
	compressed := gzip.NewWriter(output)
	if _, err := io.Copy(compressed, input); err != nil {
		_ = compressed.Close()
		return err
	}
	return compressed.Close()
}

func (l *Log) openDestLocked() error {
	if err := os.MkdirAll(filepath.Dir(l.dest), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	l.file = f
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		l.file = nil
		return err
	}
	base := l.now()
	if info.Size() > 0 {
		base = info.ModTime()
	}
	l.rotateAt = base.Add(l.rotateEvery)
	return nil
}

//wails:ignore
func (l *Log) Info(msg any, where string) { l.Log("info", msg, where) }

//wails:ignore
func (l *Log) Debug(msg any, where string) { l.Log("debug", msg, where) }

//wails:ignore
func (l *Log) Warn(msg any, where string) { l.Log("warn", msg, where) }

//wails:ignore
func (l *Log) Error(msg any, where string) { l.Log("error", msg, where) }

//wails:ignore
func (l *Log) Trace(msg any, where string) { l.Log("trace", msg, where) }

//wails:ignore
func (l *Log) Fatal(msg any, where string) { l.Log("fatal", msg, where) }

// DesktopLogPath is the packaged file sink under a Logs directory.
func DesktopLogPath(logsDir string) string {
	return filepath.Join(logsDir, desktopLogName)
}

func shouldWrite(msgLevel, current string) bool {
	mp, ok1 := levelPriority[msgLevel]
	cp, ok2 := levelPriority[current]
	if !ok1 || !ok2 {
		return false
	}
	return mp >= cp
}

const logTimeLayout = "2006-01-02 15:04:05.000"

func encodeDevConsoleLine(msg any, where string) []byte {
	return []byte(formatLogContent(msg, where) + "\n")
}

func encodeLogLine(now time.Time, level string, msg any, where string) []byte {
	content := flattenLogLine(formatLogContent(msg, where))
	stamp := now.Format(logTimeLayout)
	level = strings.ToUpper(level)
	if content == "" {
		return []byte(stamp + " " + level + "\n")
	}
	return []byte(stamp + " " + level + " " + content + "\n")
}

func formatLogContent(msg any, where string) string {
	content := redactSecrets(redactUserPaths(formatLogMsg(msg), currentHomeNeedles()))
	if where == "" {
		return content
	}
	if content == "" {
		return "[" + where + "]"
	}
	return "[" + where + "] " + content
}

func redactSecrets(value string) string {
	if value == "" {
		return ""
	}
	value = redactLogURLs(value)
	value = bearerPattern.ReplaceAllString(value, "Bearer %REDACTED%")
	value = urlUserInfoPattern.ReplaceAllString(value, `${1}%REDACTED%@`)
	value = authHeaderPattern.ReplaceAllString(value, `${1}%REDACTED%`)
	value = cookieHeaderPattern.ReplaceAllString(value, `${1}%REDACTED%`)
	value = jsonSecretPattern.ReplaceAllString(value, `${1}"%REDACTED%"`)
	return plainSecretPattern.ReplaceAllString(value, `${1}${2}%REDACTED%`)
}

// Structured logs must be decoded at the string boundary: a URL inside an
// error may end just before an escaped quote, which is not part of the URL.
func redactLogURLs(value string) string {
	if !logURLPattern.MatchString(value) {
		return value
	}
	if !json.Valid([]byte(value)) {
		return logURLPattern.ReplaceAllStringFunc(value, SanitizeLogURL)
	}
	return logJSONStringPattern.ReplaceAllStringFunc(value, func(encoded string) string {
		if !logURLPattern.MatchString(encoded) {
			return encoded
		}
		var decoded string
		if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
			return encoded
		}
		redacted := logURLPattern.ReplaceAllStringFunc(decoded, SanitizeLogURL)
		// A string is always JSON-marshalable; preserve all quote/backslash escapes.
		raw, _ := json.Marshal(redacted)
		return string(raw)
	})
}

// SanitizeLogURL removes credentials, query parameters, and fragments from a
// URL before it is placed in diagnostic context.
//
//wails:ignore
func SanitizeLogURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

func flattenLogLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "\r", " ")
}

func formatLogMsg(msg any) string {
	if msg == nil {
		return ""
	}
	switch v := msg.(type) {
	case string:
		return v
	case error:
		return v.Error()
	case fmt.Stringer:
		return v.String()
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return flattenJSONNewlines(string(raw))
	}
}

// flattenJSONNewlines replaces encoded CR/LF characters inside JSON strings
// without mistaking an escaped backslash followed by n or r for a newline.
func flattenJSONNewlines(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' || index+1 >= len(value) {
			result.WriteByte(value[index])
			continue
		}
		next := value[index+1]
		if next == '\\' {
			result.WriteString(`\\`)
			index++
			continue
		}
		if next != 'r' && next != 'n' {
			result.WriteByte(value[index])
			continue
		}
		result.WriteByte(' ')
		index++
		if next == 'r' && index+2 < len(value) && value[index+1] == '\\' && value[index+2] == 'n' {
			index += 2
		}
	}
	return result.String()
}

func currentHomeNeedles() []string {
	homeNeedlesOnce.Do(func() {
		homes := make([]string, 0, 3)
		if home, err := os.UserHomeDir(); err == nil {
			homes = append(homes, home)
		}
		if profile := os.Getenv("USERPROFILE"); profile != "" {
			homes = append(homes, profile)
		}
		drive := os.Getenv("HOMEDRIVE")
		path := os.Getenv("HOMEPATH")
		if drive != "" && path != "" {
			homes = append(homes, drive+path)
		}
		homeNeedles = prepareHomeNeedles(homes)
	})
	return homeNeedles
}

func prepareHomeNeedles(homes []string) []string {
	seen := make(map[string]struct{}, len(homes)*3)
	out := make([]string, 0, len(homes)*3)
	for _, home := range homes {
		base := normalizeHomePrefix(home)
		if base == "" {
			continue
		}
		for _, needle := range []string{
			base,
			strings.ReplaceAll(base, `\`, `/`),
			strings.ReplaceAll(base, `\`, `\\`),
		} {
			if _, ok := seen[needle]; ok {
				continue
			}
			seen[needle] = struct{}{}
			out = append(out, needle)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		return len(b) - len(a)
	})
	return out
}

func normalizeHomePrefix(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	home = filepath.Clean(home)
	home = strings.ReplaceAll(home, "/", "\\")
	home = strings.TrimRight(home, "\\")
	if home == "" || home == "." {
		return ""
	}
	home = strings.ToLower(home)
	if len(home) < 3 {
		return ""
	}
	return home
}

func redactUserPaths(s string, needles []string) string {
	if s == "" || len(needles) == 0 {
		return s
	}
	var b strings.Builder
	start := 0
	changed := false
	for i := 0; i < len(s); {
		n := 0
		for _, needle := range needles {
			n = matchNeedleAt(s, i, needle)
			if n > 0 {
				break
			}
		}
		if n > 0 {
			if !changed {
				b.Grow(len(s))
				changed = true
			}
			b.WriteString(s[start:i])
			b.WriteString(userProfileToken)
			i += n
			start = i
			continue
		}
		i++
	}
	if !changed {
		return s
	}
	b.WriteString(s[start:])
	return b.String()
}

func matchNeedleAt(s string, i int, needle string) int {
	n := len(needle)
	if n == 0 || i+n > len(s) {
		return 0
	}
	if !strings.EqualFold(s[i:i+n], needle) {
		return 0
	}
	rest := s[i+n:]
	if rest == "" || rest[0] == '\\' || rest[0] == '/' {
		return n
	}
	return 0
}
