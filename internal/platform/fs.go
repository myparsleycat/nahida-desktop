package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
)

// Sentinel errors match Electron message strings that the renderer already parses.
var (
	ErrInvalidGroupName       = errors.New("INVALID_GROUP_NAME")
	ErrInvalidWindowsFilename = errors.New("INVALID_WINDOWS_FILENAME")
)

var (
	windowsInvalidChars  = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	windowsReservedNames = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$`)
	onlyDots             = regexp.MustCompile(`^\.+$`)
	trailingDots         = regexp.MustCompile(`\.+$`)
	windowsDriveLetter   = regexp.MustCompile(`^[a-zA-Z]:$`)
)

// ProcessInfo is the lock-holder shape from Electron native/fs.
type ProcessInfo struct {
	Name string `json:"name"`
	PID  uint32 `json:"pid"`
}

// LockedPathError is Electron FS.isLockedPathError.
type LockedPathError struct {
	IsLocked          bool          `json:"isLocked"`
	IsPermissionError bool          `json:"isPermissionError"`
	Processes         []ProcessInfo `json:"processes"`
}

// FileWriteAccessResult is the detailed writable check from Electron FS.
type FileWriteAccessResult struct {
	Writable  bool          `json:"writable"`
	Exists    bool          `json:"exists"`
	Locked    bool          `json:"locked"`
	Processes []ProcessInfo `json:"processes"`
}

// PathMetadata is the Electron util:fs:metadata payload.
type PathMetadata struct {
	IsDirectory bool      `json:"isDirectory"`
	IsFile      bool      `json:"isFile"`
	Size        int64     `json:"size"`
	Mtime       time.Time `json:"mtime"`
	Ctime       time.Time `json:"ctime"`
	Birthtime   time.Time `json:"birthtime"`
}

type FS struct{}

func NewFS() *FS {
	return &FS{}
}

// Mkdir creates parentPath/name. It is mkdir, not MkdirAll.
func (f *FS) Mkdir(parentPath, name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrInvalidGroupName
	}
	if err := f.AssertValidWindowsFilename(trimmed); err != nil {
		return "", err
	}
	next := filepath.Join(parentPath, trimmed)
	if err := os.Mkdir(next, 0o755); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("ALREADY_EXISTS:%s", trimmed)
		}
		return "", err
	}
	return next, nil
}

func (f *FS) IsValidWindowsFilename(name string) bool {
	if name == "" || len(utf16.Encode([]rune(name))) > 255 {
		return false
	}
	if windowsInvalidChars.MatchString(name) {
		return false
	}
	if onlyDots.MatchString(name) {
		return false
	}
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return false
	}
	if windowsReservedNames.MatchString(name) {
		return false
	}
	return true
}

func (f *FS) AssertValidWindowsFilename(name string) error {
	if !f.IsValidWindowsFilename(name) {
		return ErrInvalidWindowsFilename
	}
	return nil
}

func (f *FS) SanitizeWindowsFilename(input, sanitizeString string) string {
	if sanitizeString == "" {
		sanitizeString = " "
	}
	sanitized := windowsInvalidChars.ReplaceAllString(input, sanitizeString)
	sanitized = strings.TrimSpace(sanitized)
	sanitized = trailingDots.ReplaceAllString(sanitized, "")
	if sanitized == "" {
		return "Untitled"
	}
	return sanitized
}

func (f *FS) SanitizePath(input string) string {
	sep := string(os.PathSeparator)
	parts := strings.Split(input, sep)
	for i, part := range parts {
		if i == 0 && windowsDriveLetter.MatchString(part) {
			continue
		}
		parts[i] = f.SanitizeWindowsFilename(part, " ")
	}
	return strings.Join(parts, sep)
}

func (f *FS) GetUniqueName(name string, existingNames []string) string {
	lower := make(map[string]struct{}, len(existingNames))
	for _, existing := range existingNames {
		lower[strings.ToLower(existing)] = struct{}{}
	}
	unique := name
	counter := 1
	for {
		if _, exists := lower[strings.ToLower(unique)]; !exists {
			return unique
		}
		counter++
		unique = fmt.Sprintf("%s (%d)", name, counter)
	}
}

func (f *FS) IsPathWritable(pathStr string) bool {
	info, err := os.Stat(pathStr)
	if err != nil {
		return false
	}
	if info.IsDir() {
		tmp, err := os.CreateTemp(pathStr, ".nhd-w-*")
		if err != nil {
			return false
		}
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
		return true
	}
	if err := fileWritableError(pathStr); err != nil {
		return false
	}
	return true
}

func fileWritableError(pathStr string) error {
	file, err := os.OpenFile(pathStr, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_ = file.Close()
	return nil
}

// GetFileWriteAccess is the detailed writable path from Electron FS.
func (f *FS) GetFileWriteAccess(filePath, parentPath string) FileWriteAccessResult {
	result := FileWriteAccessResult{Processes: []ProcessInfo{}}
	if parentPath == "" {
		parentPath = filepath.Dir(filePath)
	}
	if !f.IsPathWritable(parentPath) {
		return result
	}
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		result.Writable = true
		return result
	}
	if err != nil {
		return result
	}
	result.Exists = true
	if err := fileWritableError(filePath); err != nil {
		processes := getLockingProcesses(filePath)
		return inaccessibleFileWriteAccess(processes)
	}
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		lock := f.IsLockedPathError(err, filePath)
		result.Locked = lock.IsLocked
		result.Processes = lock.Processes
		return result
	}
	_ = file.Close()
	result.Writable = true
	return result
}

func inaccessibleFileWriteAccess(processes []ProcessInfo) FileWriteAccessResult {
	if processes == nil {
		processes = []ProcessInfo{}
	}
	return FileWriteAccessResult{
		Exists:    true,
		Locked:    len(processes) > 0,
		Processes: processes,
	}
}

// IsLockedPathError matches Electron FS.isLockedPathError.
//
//wails:ignore
func (f *FS) IsLockedPathError(err error, pathStr string) LockedPathError {
	empty := LockedPathError{Processes: []ProcessInfo{}}
	if err == nil || !isLockCandidate(err) {
		return empty
	}
	processes := getLockingProcesses(pathStr)
	if processes == nil {
		processes = []ProcessInfo{}
	}
	if len(processes) > 0 || isBusy(err) {
		return LockedPathError{IsLocked: true, Processes: processes}
	}
	return LockedPathError{IsPermissionError: isPermissionError(err), Processes: []ProcessInfo{}}
}

func (f *FS) FormatProcessList(processes []ProcessInfo) string {
	parts := make([]string, len(processes))
	for i, proc := range processes {
		parts[i] = fmt.Sprintf("%s (%d)", proc.Name, proc.PID)
	}
	return strings.Join(parts, ", ")
}

func (f *FS) GetPathMetadata(pathStr string) (PathMetadata, error) {
	info, err := os.Stat(pathStr)
	if err != nil {
		return PathMetadata{}, err
	}
	mtime, ctime, birth := fileTimes(info)
	return PathMetadata{
		IsDirectory: info.IsDir(),
		IsFile:      info.Mode().IsRegular(),
		Size:        info.Size(),
		Mtime:       mtime,
		Ctime:       ctime,
		Birthtime:   birth,
	}, nil
}

func fileTimes(info os.FileInfo) (mtime, ctime, birth time.Time) {
	mtime = info.ModTime()
	ctime, birth = fileTimesFromSys(info)
	if ctime.IsZero() {
		ctime = mtime
	}
	if birth.IsZero() {
		birth = mtime
	}
	return
}
