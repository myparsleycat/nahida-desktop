package menumaker

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"nahida.live/desktop/internal/infra"
)

const maxSourceBytes = 32 << 20

var (
	ErrSourceChanged = errors.New("MENU_MAKER_SOURCE_CHANGED")
	ErrNoKeySections = errors.New("MENU_MAKER_NO_KEY_SECTIONS")
)

type Options struct {
	Log *infra.Log
}

type MenuMaker struct {
	log *infra.Log
}

type textEncoding struct {
	name    string
	bom     bool
	newline string
}

type promotion struct {
	target   string
	rollback string
	existed  bool
}

func New() *MenuMaker { return NewWithOptions(Options{}) }

func NewWithOptions(opts Options) *MenuMaker {
	return &MenuMaker{log: opts.Log}
}

func (m *MenuMaker) ScanFolder(ctx context.Context, rootPath string, includeTXT bool) (MenuMakerScanResult, error) {
	root, err := requireDirectory(rootPath)
	if err != nil {
		return MenuMakerScanResult{}, err
	}
	result := MenuMakerScanResult{RootPath: root, Files: []MenuMakerScanFile{}}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if walkErr != nil {
			result.Stats.Errors++
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root && strings.HasPrefix(strings.ToLower(entry.Name()), "disabled") {
			result.Stats.Disabled++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root && (entry.Type()&os.ModeSymlink != 0 || isReparsePoint(path)) {
			result.Stats.Disabled++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != root {
				result.Stats.Directories++
			}
			return nil
		}
		result.Stats.Files++
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		switch ext {
		case ".ini":
			result.Stats.INI++
		case ".txt":
			result.Stats.TXT++
			if !includeTXT {
				return nil
			}
		default:
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			result.Stats.Errors++
			return relErr
		}
		result.Files = append(result.Files, MenuMakerScanFile{
			Name: entry.Name(), Path: path, RelativePath: relative, Kind: strings.TrimPrefix(ext, "."),
		})
		result.Stats.Listed++
		return nil
	})
	if err != nil {
		return MenuMakerScanResult{}, err
	}
	sort.Slice(result.Files, func(i, j int) bool {
		return strings.ToLower(result.Files[i].RelativePath) < strings.ToLower(result.Files[j].RelativePath)
	})
	return result, nil
}

func (m *MenuMaker) LoadSource(ctx context.Context, filePath string) (MenuMakerSource, error) {
	path, err := requireSourceFile(filePath)
	if err != nil {
		return MenuMakerSource{}, err
	}
	if err := ctx.Err(); err != nil {
		return MenuMakerSource{}, err
	}
	raw, err := readLimited(path, maxSourceBytes)
	if err != nil {
		return MenuMakerSource{}, err
	}
	text, encoding, err := decodeText(raw)
	if err != nil {
		return MenuMakerSource{}, err
	}
	document := parseDocument(text)
	if len(document.Slots) == 0 {
		return MenuMakerSource{}, ErrNoKeySections
	}
	return MenuMakerSource{
		Path: path, FileName: filepath.Base(path), Text: text, SHA256: sha256Hex(raw),
		Encoding: encoding.name, HasBOM: encoding.bom, Newline: encoding.newline,
		Document: document,
	}, nil
}

func (m *MenuMaker) Parse(_ context.Context, text string) (MenuMakerDocument, error) {
	document := parseDocument(text)
	if len(document.Slots) == 0 {
		return MenuMakerDocument{}, ErrNoKeySections
	}
	return document, nil
}

func (m *MenuMaker) Generate(_ context.Context, req MenuMakerGenerateRequest) (MenuMakerGenerateResult, error) {
	return generatePreview(req.SourceText, req.Slots, req.Settings), nil
}

func (m *MenuMaker) ApplyBundle(ctx context.Context, req MenuMakerApplyRequest) (result MenuMakerWriteResult, err error) {
	stage := "validate"
	cleanupState := "not-started"
	writtenPaths := []string{}
	rollbackError := ""
	defer func() {
		if err != nil && m != nil && m.log != nil {
			m.log.Error(fmt.Sprintf("menu maker apply failed: stage=%s source=%q output=%q written=%q rollback=%t rollback_error=%q cleanup=%s error=%v", stage, req.SourcePath, req.OutputININame, writtenPaths, result.RolledBack, rollbackError, cleanupState, err), "MenuMaker.ApplyBundle")
		}
	}()
	sourcePath, err := requireSourceFile(req.SourcePath)
	if err != nil {
		return result, err
	}
	original, err := readLimited(sourcePath, maxSourceBytes)
	if err != nil {
		return result, err
	}
	if !strings.EqualFold(strings.TrimSpace(req.SourceSHA256), sha256Hex(original)) {
		return result, ErrSourceChanged
	}
	text, _, err := decodeText(original)
	if err != nil {
		return result, err
	}
	generated := generatePreview(text, req.Slots, req.Settings)
	return m.applyGenerated(ctx, applyGeneratedRequest{
		sourcePath:         sourcePath,
		original:           original,
		outputININame:      req.OutputININame,
		iniText:            generated.INIText,
		encoding:           req.Encoding,
		hasBOM:             req.HasBOM,
		newline:            req.Newline,
		assets:             req.Assets,
		useOriginalININame: req.UseOriginalININame,
	}, &stage, &cleanupState, &writtenPaths, &rollbackError)
}

func (m *MenuMaker) writeGenerated(ctx context.Context, req applyGeneratedRequest) (MenuMakerWriteResult, error) {
	stage := "validate"
	cleanupState := "not-started"
	writtenPaths := []string{}
	rollbackError := ""
	return m.applyGenerated(ctx, req, &stage, &cleanupState, &writtenPaths, &rollbackError)
}

type applyGeneratedRequest struct {
	sourcePath         string
	original           []byte
	outputININame      string
	iniText            string
	encoding           string
	hasBOM             bool
	newline            string
	assets             []MenuMakerGeneratedAsset
	useOriginalININame bool
}

func (m *MenuMaker) applyGenerated(
	ctx context.Context,
	req applyGeneratedRequest,
	stage *string,
	cleanupState *string,
	writtenPaths *[]string,
	rollbackError *string,
) (result MenuMakerWriteResult, err error) {
	outputName, err := expectedOutputName(req.sourcePath, req.useOriginalININame)
	if err != nil {
		return result, err
	}
	if !strings.EqualFold(outputName, req.outputININame) {
		return result, fmt.Errorf("invalid menu maker output name %q, expected %q", req.outputININame, outputName)
	}
	assets, err := validateAssets(req.assets)
	if err != nil {
		return result, err
	}
	encoding := textEncoding{name: req.encoding, bom: req.hasBOM, newline: req.newline}
	iniBytes, err := encodeText(req.iniText, encoding)
	if err != nil {
		return result, err
	}
	dir := filepath.Dir(req.sourcePath)
	outputPath := filepath.Join(dir, outputName)
	stageDir, err := os.MkdirTemp(dir, ".menu-maker-stage-*")
	if err != nil {
		return result, fmt.Errorf("create menu maker staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	*stage = "stage-output"
	staged := make(map[string]string, len(assets)+1)
	allData := append([]MenuMakerGeneratedAsset{{RelativePath: outputName, Data: iniBytes}}, assets...)
	for index, file := range allData {
		stagedPath := filepath.Join(stageDir, fmt.Sprintf("new-%04d", index))
		if writeErr := os.WriteFile(stagedPath, file.Data, 0o600); writeErr != nil {
			return result, fmt.Errorf("stage %s: %w", file.RelativePath, writeErr)
		}
		staged[file.RelativePath] = stagedPath
	}

	backupPath := ""
	if strings.EqualFold(filepath.Ext(req.sourcePath), ".ini") {
		*stage = "backup-source"
		backupPath, err = nextBackupPath(req.sourcePath)
		if err != nil {
			return result, err
		}
		if err = writeExclusive(backupPath, req.original); err != nil {
			return result, fmt.Errorf("write menu maker source backup: %w", err)
		}
		if backup, readErr := os.ReadFile(backupPath); readErr != nil || sha256Hex(backup) != sha256Hex(req.original) {
			_ = os.Remove(backupPath)
			return result, errors.New("menu maker source backup verification failed")
		}
		result.BackupPath = backupPath
	}

	promotions := make([]promotion, 0, len(allData))
	rollback := func() error {
		*cleanupState = "rolling-back"
		rollbackErrors := []error{}
		for index := len(promotions) - 1; index >= 0; index-- {
			entry := promotions[index]
			if removeErr := os.Remove(entry.target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove partially applied target %s: %w", entry.target, removeErr))
			}
			if entry.existed {
				if restoreErr := os.Rename(entry.rollback, entry.target); restoreErr != nil {
					rollbackErrors = append(rollbackErrors, fmt.Errorf("restore menu maker target %s: %w", entry.target, restoreErr))
				}
			}
		}
		if len(rollbackErrors) > 0 {
			*cleanupState = "rollback-failed-backup-preserved"
			joined := errors.Join(rollbackErrors...)
			*rollbackError = joined.Error()
			return joined
		}
		result.RolledBack = true
		*cleanupState = "rolled-back"
		if backupPath != "" {
			if removeErr := os.Remove(backupPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				*cleanupState = "rolled-back-backup-cleanup-failed"
				*rollbackError = removeErr.Error()
				return fmt.Errorf("remove menu maker rollback backup %s: %w", backupPath, removeErr)
			}
			result.BackupPath = ""
		}
		return nil
	}

	*stage = "promote-output"
	for index, file := range allData {
		if err = ctx.Err(); err != nil {
			return result, errors.Join(err, rollback())
		}
		target := filepath.Join(dir, filepath.FromSlash(file.RelativePath))
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return result, errors.Join(err, rollback())
		}
		entry := promotion{target: target, rollback: filepath.Join(stageDir, fmt.Sprintf("old-%04d", index))}
		if _, statErr := os.Stat(target); statErr == nil {
			entry.existed = true
			if err = os.Rename(target, entry.rollback); err != nil {
				return result, errors.Join(fmt.Errorf("stage existing target %s: %w", target, err), rollback())
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return result, errors.Join(statErr, rollback())
		}
		promotions = append(promotions, entry)
		if err = os.Rename(staged[file.RelativePath], target); err != nil {
			return result, errors.Join(fmt.Errorf("promote menu maker target %s: %w", target, err), rollback())
		}
		*writtenPaths = append(*writtenPaths, target)
	}

	if strings.EqualFold(filepath.Ext(req.sourcePath), ".ini") && !samePath(req.sourcePath, outputPath) {
		*stage = "disable-source"
		held := filepath.Join(stageDir, "disabled-source.ini")
		if err = os.Rename(req.sourcePath, held); err != nil {
			return result, errors.Join(fmt.Errorf("disable original menu maker source: %w", err), rollback())
		}
	}

	*cleanupState = "complete"
	result.OutputINIPath = outputPath
	result.ResourcePaths = make([]string, 0, len(assets))
	for _, asset := range assets {
		result.ResourcePaths = append(result.ResourcePaths, filepath.Join(dir, filepath.FromSlash(asset.RelativePath)))
	}
	return result, nil
}

func (m *MenuMaker) SaveINI(_ context.Context, req MenuMakerSaveINIRequest) (MenuMakerWriteResult, error) {
	destination, err := requireSavePath(req.DestinationPath, ".ini")
	if err != nil {
		return MenuMakerWriteResult{}, err
	}
	generated := generatePreview(req.SourceText, req.Slots, req.Settings)
	return saveINIBytes(destination, generated.INIText, textEncoding{name: req.Encoding, bom: req.HasBOM, newline: req.Newline})
}

func (m *MenuMaker) SaveZIP(_ context.Context, req MenuMakerSaveZIPRequest) (MenuMakerWriteResult, error) {
	return saveZIPBytes(req.DestinationPath, req.OutputININame, generatePreview(req.SourceText, req.Slots, req.Settings).INIText, textEncoding{name: req.Encoding, bom: req.HasBOM, newline: req.Newline}, req.Assets)
}

func saveINIBytes(destination, iniText string, encoding textEncoding) (MenuMakerWriteResult, error) {
	data, err := encodeText(iniText, encoding)
	if err != nil {
		return MenuMakerWriteResult{}, err
	}
	if err := writeAtomic(destination, data); err != nil {
		return MenuMakerWriteResult{}, err
	}
	return MenuMakerWriteResult{OutputINIPath: destination, ResourcePaths: []string{}}, nil
}

func saveZIPBytes(destination, outputININame, iniText string, encoding textEncoding, assets []MenuMakerGeneratedAsset) (MenuMakerWriteResult, error) {
	resolved, err := requireSavePath(destination, ".zip")
	if err != nil {
		return MenuMakerWriteResult{}, err
	}
	if filepath.Base(outputININame) != outputININame || !strings.EqualFold(filepath.Ext(outputININame), ".ini") {
		return MenuMakerWriteResult{}, errors.New("invalid menu maker ZIP INI name")
	}
	validated, err := validateAssets(assets)
	if err != nil {
		return MenuMakerWriteResult{}, err
	}
	iniData, err := encodeText(iniText, encoding)
	if err != nil {
		return MenuMakerWriteResult{}, err
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	files := append([]MenuMakerGeneratedAsset{{RelativePath: outputININame, Data: iniData}}, validated...)
	for _, file := range files {
		header := &zip.FileHeader{Name: filepath.ToSlash(file.RelativePath), Method: zip.Deflate}
		header.Modified = time.Now()
		entry, createErr := writer.CreateHeader(header)
		if createErr != nil {
			_ = writer.Close()
			return MenuMakerWriteResult{}, createErr
		}
		if _, writeErr := entry.Write(file.Data); writeErr != nil {
			_ = writer.Close()
			return MenuMakerWriteResult{}, writeErr
		}
	}
	if err := writer.Close(); err != nil {
		return MenuMakerWriteResult{}, err
	}
	if err := writeAtomic(resolved, buffer.Bytes()); err != nil {
		return MenuMakerWriteResult{}, err
	}
	return MenuMakerWriteResult{ArchivePath: resolved, ResourcePaths: []string{}}, nil
}

func requireDirectory(path string) (string, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("menu maker directory is unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("menu maker directory is unavailable: path is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func requireSourceFile(path string) (string, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || isReparsePoint(resolved) {
		return "", errors.New("menu maker source must be a regular file")
	}
	ext := strings.ToLower(filepath.Ext(resolved))
	if ext != ".ini" && ext != ".txt" {
		return "", errors.New("menu maker source must be an INI or TXT file")
	}
	return filepath.Clean(resolved), nil
}

func requireSavePath(path, extension string) (string, error) {
	resolved, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(resolved), extension) {
		return "", fmt.Errorf("menu maker destination must use %s", extension)
	}
	if info, statErr := os.Lstat(resolved); statErr == nil && (info.Mode()&os.ModeSymlink != 0 || isReparsePoint(resolved)) {
		return "", errors.New("menu maker destination cannot be a link")
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	return filepath.Clean(resolved), nil
}

func validateAssets(input []MenuMakerGeneratedAsset) ([]MenuMakerGeneratedAsset, error) {
	seen := make(map[string]bool, len(input))
	out := make([]MenuMakerGeneratedAsset, 0, len(input))
	for _, asset := range input {
		name := filepath.ToSlash(strings.TrimSpace(asset.RelativePath))
		base := filepath.Base(name)
		validName := base == "draw_2d.hlsl" || base == "bg.png" || base == "title.png" ||
			(strings.HasPrefix(base, "slot_") && strings.HasSuffix(base, ".png"))
		if filepath.IsAbs(name) || strings.Contains(name, "..") || filepath.Dir(name) != "res_gui" || !validName {
			return nil, fmt.Errorf("invalid menu maker asset path %q", asset.RelativePath)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("duplicate menu maker asset path %q", asset.RelativePath)
		}
		seen[key] = true
		out = append(out, MenuMakerGeneratedAsset{RelativePath: name, Data: append([]byte(nil), asset.Data...)})
	}
	return out, nil
}

func expectedOutputName(source string, original bool) (string, error) {
	name := filepath.Base(source)
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		return "", errors.New("invalid menu maker source name")
	}
	if original {
		return base + ".ini", nil
	}
	return base + "_gui.ini", nil
}

func nextBackupPath(source string) (string, error) {
	dir := filepath.Dir(source)
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	primary := filepath.Join(dir, base+".txt")
	if _, err := os.Stat(primary); errors.Is(err, os.ErrNotExist) {
		return primary, nil
	} else if err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	for index := range 100 {
		suffix := ""
		if index > 0 {
			suffix = fmt.Sprintf("-%02d", index)
		}
		candidate := filepath.Join(dir, base+".backup-"+stamp+suffix+".txt")
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not allocate menu maker backup name")
}

func readLimited(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("menu maker source is too large")
	}
	return data, nil
}

func decodeText(data []byte) (string, textEncoding, error) {
	encoding := textEncoding{name: "utf8", newline: "lf"}
	if bytes.Contains(data, []byte("\r\n")) {
		encoding.newline = "crlf"
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		encoding.bom = true
		data = data[3:]
	}
	if utf8.Valid(data) {
		return string(data), encoding, nil
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return "", encoding, errors.New("menu maker source is neither UTF-8 nor GBK")
	}
	encoding.name = "gbk"
	encoding.bom = false
	return string(decoded), encoding, nil
}

func encodeText(content string, encoding textEncoding) ([]byte, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if encoding.newline == "crlf" {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	if encoding.name == "gbk" {
		return io.ReadAll(transform.NewReader(strings.NewReader(content), simplifiedchinese.GBK.NewEncoder()))
	}
	data := []byte(content)
	if encoding.bom {
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	}
	return data, nil
}

func writeExclusive(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".menu-maker-write-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err = temp.Write(data); err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	rollback := tempPath + ".old"
	existed := false
	if _, statErr := os.Stat(path); statErr == nil {
		existed = true
		if err = os.Rename(path, rollback); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if err = os.Rename(tempPath, path); err != nil {
		if existed {
			_ = os.Rename(rollback, path)
		}
		return err
	}
	if existed {
		_ = os.Remove(rollback)
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
