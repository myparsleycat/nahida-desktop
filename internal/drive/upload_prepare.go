package drive

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type UploadFile struct {
	FID        string `json:"FID"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ParentPath string `json:"parentPath"`
	FullPath   string `json:"fullPath"`
}

type FinalUploadFile struct {
	UploadFile
	ParentID string `json:"parentId"`
	SHA256   string `json:"sha256"`
}

type UploadDirectory struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	ParentPath string `json:"parentPath"`
}

type UploadPreparation struct {
	PID         string            `json:"pid"`
	Files       []UploadFile      `json:"files"`
	Directories []UploadDirectory `json:"directories"`
	TotalSize   int64             `json:"totalSize"`
	ProcessName string            `json:"processName"`
}

type UploadConflictStrategy string

const (
	UploadConflictSuffix UploadConflictStrategy = "suffix"
	UploadConflictSkip   UploadConflictStrategy = "skip"
)

type GetUploadConflictsParams struct {
	DestID string   `json:"destId"`
	Paths  []string `json:"paths"`
}

type UploadConflictsResult struct {
	SelectedPaths []string `json:"selectedPaths"`
	Conflicts     []string `json:"conflicts"`
}

func (d *Drive) GetUploadConflicts(ctx context.Context, params GetUploadConflictsParams) (result UploadConflictsResult, err error) {
	defer normalizeDriveBoundaryError(&err, "fn:getUploadConflicts")
	if len(params.Paths) == 0 {
		return UploadConflictsResult{}, errors.New("upload paths are required")
	}
	if err := ensureUploadSourceReadable(params.Paths[0]); err != nil {
		return UploadConflictsResult{}, err
	}
	item, err := d.GetItem(ctx, params.DestID)
	if err != nil {
		return UploadConflictsResult{}, err
	}
	existingNames := childNames(item)
	rules, err := d.UploadRules(ctx)
	if err != nil {
		return UploadConflictsResult{}, err
	}
	files, directories, err := collectUploadPaths(params.Paths, rules, nil, false)
	if err != nil {
		return UploadConflictsResult{}, err
	}
	seen := make(map[string]struct{})
	conflicts := make([]string, 0)
	for _, directory := range directories {
		if directory.ParentPath != "" {
			continue
		}
		if _, exists := existingNames[directory.Name]; exists {
			conflicts = append(conflicts, directory.Name)
		} else if _, duplicate := seen[directory.Name]; duplicate {
			conflicts = append(conflicts, directory.Name)
		}
		seen[directory.Name] = struct{}{}
	}
	for _, file := range files {
		if file.ParentPath != "" {
			continue
		}
		if _, exists := existingNames[file.Name]; exists {
			conflicts = append(conflicts, file.Name)
		} else if _, duplicate := seen[file.Name]; duplicate {
			conflicts = append(conflicts, file.Name)
		}
		seen[file.Name] = struct{}{}
	}
	return UploadConflictsResult{SelectedPaths: slices.Clone(params.Paths), Conflicts: conflicts}, nil
}

func ensureUploadSourceReadable(path string) error {
	handle, err := os.Open(path)
	if err != nil {
		return errors.New("Path is not readable") //nolint:staticcheck // Electron contract text.
	}
	if err := handle.Close(); err != nil {
		return errors.New("Path is not readable") //nolint:staticcheck // Electron contract text.
	}
	return nil
}

//wails:ignore
func PrepareUpload(paths []string, existingNames []string, strategy UploadConflictStrategy, rules UploadRules, additionalExtensions []string, allowAllFiles bool) (UploadPreparation, error) {
	if strategy == "" {
		strategy = UploadConflictSuffix
	}
	if strategy != UploadConflictSuffix && strategy != UploadConflictSkip {
		return UploadPreparation{}, fmt.Errorf("unsupported upload conflict strategy %q", strategy)
	}
	files, directories, err := collectUploadPaths(paths, rules, additionalExtensions, allowAllFiles)
	if err != nil {
		return UploadPreparation{}, err
	}
	existing := make(map[string]struct{}, len(existingNames))
	for _, name := range existingNames {
		existing[name] = struct{}{}
	}
	skippedDirectories := make(map[string]struct{})
	skippedFiles := make(map[string]struct{})
	for index := range directories {
		directory := &directories[index]
		if directory.ParentPath != "" {
			continue
		}
		if _, conflict := existing[directory.Name]; !conflict {
			existing[directory.Name] = struct{}{}
			continue
		}
		if strategy == UploadConflictSkip {
			skippedDirectories[directory.Path] = struct{}{}
			continue
		}
		directory.Name = uniqueUploadName(directory.Name, existing)
		existing[directory.Name] = struct{}{}
	}
	for index := range files {
		file := &files[index]
		if file.ParentPath != "" {
			continue
		}
		if _, conflict := existing[file.Name]; !conflict {
			existing[file.Name] = struct{}{}
			continue
		}
		if strategy == UploadConflictSkip {
			skippedFiles[file.Path] = struct{}{}
			continue
		}
		file.Name = uniqueUploadName(file.Name, existing)
		existing[file.Name] = struct{}{}
	}
	if len(skippedDirectories) > 0 {
		directories = slices.DeleteFunc(directories, func(directory UploadDirectory) bool {
			return underSkippedRoot(directory.Path, skippedDirectories)
		})
		files = slices.DeleteFunc(files, func(file UploadFile) bool {
			return underSkippedRoot(file.Path, skippedDirectories)
		})
	}
	if len(skippedFiles) > 0 {
		files = slices.DeleteFunc(files, func(file UploadFile) bool {
			_, skipped := skippedFiles[file.Path]
			return skipped
		})
	}
	processName := ""
	if len(paths) == 1 {
		processName = filepath.Base(paths[0])
	} else if len(paths) > 1 {
		if len(directories) > 0 {
			names := make([]string, len(directories))
			for i := range directories {
				names[i] = directories[i].Name
			}
			slices.Sort(names)
			processName = fmt.Sprintf("%s 외 %d개", names[len(names)-1], len(paths)-1)
		} else {
			processName = fmt.Sprintf("%s 외 %d개", filepath.Base(paths[0]), len(paths)-1)
		}
	}
	totalSize := int64(0)
	for _, file := range files {
		totalSize += file.Size
	}
	return UploadPreparation{
		PID:         newUploadPID(),
		Files:       files,
		Directories: directories,
		TotalSize:   totalSize,
		ProcessName: processName,
	}, nil
}

func collectUploadPaths(paths []string, rules UploadRules, additionalExtensions []string, allowAllFiles bool) ([]UploadFile, []UploadDirectory, error) {
	allowed := extensionMaxSizes(rules, additionalExtensions)
	files := make([]UploadFile, 0)
	directories := make([]UploadDirectory, 0)
	for _, rawPath := range paths {
		absolute, err := filepath.EvalSymlinks(rawPath)
		if err != nil {
			continue
		}
		absolute, err = filepath.Abs(absolute)
		if err != nil {
			continue
		}
		info, err := os.Stat(absolute)
		if err != nil || isSystemFile(filepath.Base(absolute)) {
			continue
		}
		if info.Mode().IsRegular() {
			if uploadFilePermitted(info.Name(), info.Size(), allowed, allowAllFiles, rules.MaxFileSize) {
				files = append(files, UploadFile{
					Path:       info.Name(),
					Name:       info.Name(),
					Size:       info.Size(),
					ParentPath: "",
					FullPath:   filepath.ToSlash(absolute),
				})
			}
			continue
		}
		if !info.IsDir() {
			continue
		}
		rootParent := filepath.Dir(absolute)
		walkErr := filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if path != absolute && isSystemFile(entry.Name()) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			relative, relErr := filepath.Rel(rootParent, path)
			if relErr != nil {
				return relErr
			}
			relative = filepath.ToSlash(relative)
			parent := filepath.ToSlash(filepath.Dir(relative))
			if parent == "." {
				parent = ""
			}
			if entry.IsDir() {
				directories = append(directories, UploadDirectory{Path: relative, Name: entry.Name(), ParentPath: parent})
				return nil
			}
			entryInfo, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if !entryInfo.Mode().IsRegular() || !uploadFilePermitted(entry.Name(), entryInfo.Size(), allowed, allowAllFiles, rules.MaxFileSize) {
				return nil
			}
			files = append(files, UploadFile{
				Path:       relative,
				Name:       entry.Name(),
				Size:       entryInfo.Size(),
				ParentPath: parent,
				FullPath:   filepath.ToSlash(path),
			})
			return nil
		})
		if walkErr != nil {
			return nil, nil, walkErr
		}
	}
	assignStableUploadFileIDs(files)
	return files, directories, nil
}

func assignStableUploadFileIDs(files []UploadFile) {
	occurrences := make(map[string]int)
	for index := range files {
		relative := strings.ToLower(filepath.ToSlash(files[index].Path))
		occurrence := occurrences[relative]
		occurrences[relative] = occurrence + 1
		digest := sha256.Sum256([]byte(relative + "\x00" + fmt.Sprintf("%d", occurrence)))
		files[index].FID = hex.EncodeToString(digest[:])
	}
}

func HashUploadFiles(ctx context.Context, files []UploadFile, concurrency int, onProgress func(int)) ([]FinalUploadFile, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(files) {
		concurrency = len(files)
	}
	if len(files) == 0 {
		return []FinalUploadFile{}, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := make([]FinalUploadFile, len(files))
	jobs := make(chan int)
	var workers sync.WaitGroup
	completed := 0
	var callbackMu sync.Mutex
	var errorMu sync.Mutex
	var firstErr error
	setError := func(err error) {
		errorMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		errorMu.Unlock()
	}
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for index := range jobs {
				hash, err := hashUploadFile(ctx, files[index].FullPath)
				if err != nil {
					setError(fmt.Errorf("failed to hash %s: %w", files[index].Name, err))
					return
				}
				out[index] = FinalUploadFile{UploadFile: files[index], SHA256: hash}
				callbackMu.Lock()
				completed++
				if onProgress != nil {
					onProgress(completed)
				}
				callbackMu.Unlock()
			}
		}()
	}
sendLoop:
	for index := range files {
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- index:
		}
	}
	close(jobs)
	workers.Wait()
	errorMu.Lock()
	err := firstErr
	errorMu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func hashUploadFile(ctx context.Context, path string) (string, error) {
	file, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, contextReader{ctx: ctx, reader: file}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func childNames(item any) map[string]struct{} {
	out := make(map[string]struct{})
	root, ok := item.(map[string]any)
	if !ok {
		return out
	}
	children, ok := root["children"].([]any)
	if !ok {
		return out
	}
	for _, child := range children {
		row, rowOK := child.(map[string]any)
		name, nameOK := row["name"].(string)
		if rowOK && nameOK {
			out[name] = struct{}{}
		}
	}
	return out
}

func isSystemFile(name string) bool {
	lower := strings.ToLower(name)
	return name == ".DS_Store" || strings.HasPrefix(name, "._") || name == ".AppleDouble" ||
		name == ".Spotlight-V100" || name == ".Trashes" || name == ".fseventsd" ||
		name == ".TemporaryItems" || name == ".apdisk" || name == "__MACOSX" ||
		lower == "thumbs.db" || (strings.HasPrefix(lower, "ehthumbs") && strings.HasSuffix(lower, ".db")) ||
		lower == "desktop.ini" || name == "~"
}

func uniqueUploadName(base string, existing map[string]struct{}) string {
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s (%d)", base, index)
		if _, exists := existing[candidate]; !exists {
			return candidate
		}
	}
}

func underSkippedRoot(target string, roots map[string]struct{}) bool {
	for root := range roots {
		if target == root || strings.HasPrefix(target, root+"/") {
			return true
		}
	}
	return false
}

func newUploadPID() string {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%p", &data)))
		return hex.EncodeToString(digest[:10])
	}
	return base64.RawURLEncoding.EncodeToString(data[:])
}
