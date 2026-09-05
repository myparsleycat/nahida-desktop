package infra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mholt/archives"
)

type ExtractOptions struct {
	// nil preserves the Electron extractor default of true.
	FlattenSingleRoot *bool
}

type ExtractProgress func(percent int, message string)

type Archive struct{ log *Log }

func NewArchive() *Archive {
	return &Archive{}
}

//wails:ignore
func (a *Archive) UseLog(log *Log) { a.log = log }

func (a *Archive) IsArchive(ctx context.Context, archivePath string) bool {
	input, err := os.Open(archivePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_ = ReportError(a.log, err, "Archive", Diagnostic{Severity: DiagnosticWarn, Operation: "identify", Fields: map[string]any{"path": archivePath}})
		}
		return false
	}
	defer func() { _ = input.Close() }()
	format, _, err := archives.Identify(ctx, archivePath, input)
	if err != nil {
		return false
	}
	_, ok := format.(archives.Extractor)
	return ok
}

// IsArchiveOf identifies an archive by content and limits accepted formats to
// the supplied extensions. It is used where Electron's file-type sniffing had
// a narrower format contract than the general extractor.
func (a *Archive) IsArchiveOf(ctx context.Context, archivePath string, extensions ...string) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	input, err := os.Open(archivePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_ = ReportError(a.log, err, "Archive", Diagnostic{Severity: DiagnosticWarn, Operation: "identify", Fields: map[string]any{"path": archivePath}})
		}
		return false
	}
	defer func() { _ = input.Close() }()
	format, _, err := archives.Identify(ctx, archivePath, input)
	if err != nil {
		return false
	}
	extension := strings.Trim(strings.ToLower(format.Extension()), ".")
	for _, allowed := range extensions {
		if extension == strings.Trim(strings.ToLower(allowed), ".") {
			return true
		}
	}
	return false
}

func (a *Archive) HasSingleTopLevelDirectory(ctx context.Context, archivePath string) (bool, error) {
	top := make(map[string]bool)
	err := walkArchive(ctx, archivePath, func(_ context.Context, info archives.FileInfo) error {
		name := normalizeArchiveName(info.NameInArchive)
		if name == "" {
			return nil
		}
		segments := strings.Split(name, "/")
		if len(segments) == 1 && ignoredTopLevelEntry(segments[0]) {
			return nil
		}
		isDirectory := len(segments) > 1 || info.IsDir()
		top[segments[0]] = top[segments[0]] || isDirectory
		if len(top) > 1 {
			return errMultipleTopLevels
		}
		return nil
	})
	if errors.Is(err, errMultipleTopLevels) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect archive: %w", err)
	}
	for _, directory := range top {
		return directory, nil
	}
	return false, nil
}

func (a *Archive) Extract(ctx context.Context, archivePath, targetDir string, options ExtractOptions, onProgress ExtractProgress) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("create extraction target: %w", err)
	}
	emitExtractProgress(onProgress, 0, "Preparing extraction")

	files := 0
	if err := walkArchive(ctx, archivePath, func(_ context.Context, info archives.FileInfo) error {
		if !info.IsDir() && !shouldSkipArchiveEntry(info.NameInArchive) {
			files++
		}
		return nil
	}); err != nil {
		return "", fmt.Errorf("inspect archive: %w", err)
	}

	temporaryDir, err := os.MkdirTemp(targetDir, ".extract_temp_")
	if err != nil {
		return "", fmt.Errorf("create extraction temp directory: %w", err)
	}
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = ReportError(a.log, os.RemoveAll(temporaryDir), "Archive", Diagnostic{Operation: "extract", Stage: "cleanup", Fields: map[string]any{"path": temporaryDir, "archivePath": archivePath}})
		}
	}()
	emitExtractProgress(onProgress, 1, "Starting extraction")

	extracted := 0
	err = walkArchive(ctx, archivePath, func(ctx context.Context, info archives.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if shouldSkipArchiveEntry(info.NameInArchive) {
			return nil
		}
		relative, err := safeArchivePath(info.NameInArchive)
		if err != nil {
			return err
		}
		destination := filepath.Join(temporaryDir, relative)
		if info.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("create archive entry parent: %w", err)
		}
		input, err := info.Open()
		if err != nil {
			return fmt.Errorf("open archive entry %q: %w", info.NameInArchive, err)
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			_ = input.Close()
			return fmt.Errorf("create archive entry %q: %w", info.NameInArchive, err)
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return fmt.Errorf("extract archive entry %q: %w", info.NameInArchive, copyErr)
		}
		if inputCloseErr != nil {
			return fmt.Errorf("close archive entry %q: %w", info.NameInArchive, inputCloseErr)
		}
		if outputCloseErr != nil {
			return fmt.Errorf("finalize archive entry %q: %w", info.NameInArchive, outputCloseErr)
		}
		extracted++
		if files > 0 {
			percent := max(1, min(90, extracted*90/files))
			emitExtractProgress(onProgress, percent, fmt.Sprintf("Extracting... %d%%", percent))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("extract archive: %w", err)
	}
	emitExtractProgress(onProgress, 92, "Finalizing extracted files")

	flatten := true
	if options.FlattenSingleRoot != nil {
		flatten = *options.FlattenSingleRoot
	}
	currentPath := temporaryDir
	targetName := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
	if targetName == "" || targetName == "." {
		targetName = "extracted"
	}
	for flatten {
		entries, readErr := visibleDirectoryEntries(currentPath)
		if readErr != nil {
			return "", fmt.Errorf("inspect extracted directory: %w", readErr)
		}
		if len(entries) != 1 || !entries[0].IsDir() {
			break
		}
		currentPath = filepath.Join(currentPath, entries[0].Name())
		targetName = entries[0].Name()
		emitExtractProgress(onProgress, 95, "Resolving extracted folder: "+targetName)
	}

	targetPath := uniqueFolderPath(targetDir, targetName)
	emitExtractProgress(onProgress, 97, "Moving extracted contents")
	if err := os.Rename(currentPath, targetPath); err != nil {
		return "", fmt.Errorf("move extracted folder: %w", err)
	}
	if currentPath == temporaryDir {
		keepTemporary = true
	}
	emitExtractProgress(onProgress, 100, "Extraction complete")
	return targetPath, nil
}

var errMultipleTopLevels = errors.New("archive has multiple top-level entries")

func walkArchive(ctx context.Context, archivePath string, handler archives.FileHandler) error {
	input, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	format, stream, err := archives.Identify(ctx, archivePath, input)
	if err != nil {
		return err
	}
	extractor, ok := format.(archives.Extractor)
	if !ok {
		return fmt.Errorf("format %T is not an archive", format)
	}
	return extractor.Extract(ctx, stream, handler)
}

func normalizeArchiveName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	return strings.Trim(name, "/")
}

func ignoredTopLevelEntry(name string) bool {
	return strings.EqualFold(name, "desktop.ini") || strings.EqualFold(name, "thumbs.db")
}

func shouldSkipArchiveEntry(name string) bool {
	normalized := normalizeArchiveName(name)
	return normalized == "" || (!strings.Contains(normalized, "/") && ignoredTopLevelEntry(normalized))
}

func safeArchivePath(name string) (string, error) {
	normalized := normalizeArchiveName(name)
	if normalized == "" {
		return "", errors.New("archive entry has an empty path")
	}
	parts := strings.Split(normalized, "/")
	safe := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("archive entry %q uses an unsafe relative path", name)
		default:
			if strings.ContainsRune(part, ':') {
				return "", fmt.Errorf("archive entry %q uses an unsafe volume path", name)
			}
			safe = append(safe, part)
		}
	}
	if len(safe) == 0 {
		return "", fmt.Errorf("archive entry %q resolves to an empty path", name)
	}
	return filepath.Join(safe...), nil
}

func visibleDirectoryEntries(directory string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	visible := entries[:0]
	for _, entry := range entries {
		if !ignoredTopLevelEntry(entry.Name()) {
			visible = append(visible, entry)
		}
	}
	return visible, nil
}

func uniqueFolderPath(parent, name string) string {
	target := filepath.Join(parent, name)
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return target
	}
	for index := 2; ; index++ {
		target = filepath.Join(parent, fmt.Sprintf("%s (%d)", name, index))
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			return target
		}
	}
}

func emitExtractProgress(callback ExtractProgress, percent int, message string) {
	if callback != nil {
		callback(percent, message)
	}
}
