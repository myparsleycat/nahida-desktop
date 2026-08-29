package mod

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"nahida.live/desktop/internal/infra"
)

const (
	defaultNteSigBypasserURL = "https://github.com/rm-NoobInCoding/UniversalSigBypasser/releases/download/v1.2/SigBypasser_v1.2.zip"
	defaultNteASILoaderURL   = "https://github.com/ThirteenAG/Ultimate-ASI-Loader/releases/download/x64-latest/winhttp-x64.zip"
	nteSigBypasserArchive    = "SigBypasser_v1.2.zip"
	nteASILoaderArchive      = "winhttp-x64.zip"
	nteBootstrapEvent        = "mod:nte-bootstrap-progress"
)

var nteBootstrapRequiredFiles = []string{"dsound.dll", "UniversalSigBypasser.asi", "winhttp.dll"}

type NteBootstrapProgress struct {
	Phase       string   `json:"phase"`
	ArchiveName string   `json:"archiveName,omitempty"`
	Progress    *float64 `json:"progress"`
	Message     string   `json:"message,omitempty"`
}

type nteBootstrapFileCopy struct {
	sourcePath string
	targetPath string
}

type nteBootstrapSnapshot struct {
	targetPath string
	backupPath string
	existed    bool
}

type nteBootstrapInstall struct {
	rollbackDir string
	snapshots   []nteBootstrapSnapshot
}

func (m *Mod) resolveNteBootstrapExecutablePath(
	ctx context.Context,
	modFolderPath string,
	linkedModFolderPath, gameInstallPath *string,
) (string, error) {
	installPath := ""
	if gameInstallPath != nil {
		installPath = *gameInstallPath
	} else {
		modsPath := modFolderPath
		if linkedModFolderPath != nil {
			modsPath = *linkedModFolderPath
		}
		installPath = filepath.Clean(filepath.Join(modsPath, "..", "..", "..", "..", ".."))
	}
	resolution, err := m.ResolveNteInstallPath(ctx, installPath)
	if err != nil {
		return "", err
	}
	if resolution == nil {
		return "", errors.New("NTE_EXECUTABLE_PATH_NOT_FOUND")
	}
	return resolution.ExecutablePath, nil
}

func (m *Mod) ensureNteBootstrapFiles(ctx context.Context, executablePath string) (install *nteBootstrapInstall, returnErr error) {
	targetDir := filepath.Dir(executablePath)
	if info, err := os.Stat(targetDir); err != nil || !info.IsDir() {
		return nil, errors.New("NTE_BOOTSTRAP_INVALID_TARGET_DIR")
	}
	if runtime.GOARCH != "amd64" {
		err := fmt.Errorf("NTE_BOOTSTRAP_UNSUPPORTED_ARCH: %s", runtime.GOARCH)
		m.emitNteBootstrapProgress("failed", nil, "", err.Error())
		return nil, err
	}
	if nteBootstrapFilesInstalled(targetDir) {
		m.emitNteBootstrapProgress("completed", float64Pointer(100), "", "")
		return nil, nil
	}
	if m == nil || m.archive == nil || m.http == nil {
		err := errors.New("NTE_BOOTSTRAP_SERVICE_NOT_CONFIGURED")
		m.emitNteBootstrapProgress("failed", nil, "", err.Error())
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "nte-bootstrap-*")
	if err != nil {
		m.emitNteBootstrapProgress("failed", nil, "", err.Error())
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	defer func() {
		if returnErr == nil {
			return
		}
		if install != nil {
			returnErr = errors.Join(returnErr, install.Rollback())
			install = nil
		}
		m.emitNteBootstrapProgress("failed", nil, "", returnErr.Error())
	}()

	m.emitNteBootstrapProgress("fetching-release", nil, nteSigBypasserArchive, "")
	if err := m.downloadAndExtractNteBootstrap(ctx, m.nteSigBypasserURL, nteSigBypasserArchive, tempDir); err != nil {
		return nil, err
	}
	m.emitNteBootstrapProgress("fetching-release", float64Pointer(93), nteASILoaderArchive, "")
	if err := m.downloadAndExtractNteBootstrap(ctx, m.nteASILoaderURL, nteASILoaderArchive, tempDir); err != nil {
		return nil, err
	}

	files, err := collectNteBootstrapFiles(tempDir)
	if err != nil {
		return nil, err
	}
	copies := make([]nteBootstrapFileCopy, 0, len(nteBootstrapRequiredFiles)+len(files))
	for _, name := range nteBootstrapRequiredFiles {
		source := findNteBootstrapFile(files, name)
		if source == "" {
			return nil, fmt.Errorf("NTE_BOOTSTRAP_FILE_MISSING:%s", name)
		}
		copies = append(copies, nteBootstrapFileCopy{sourcePath: source, targetPath: filepath.Join(targetDir, name)})
	}
	for _, path := range files {
		if strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".sha512") {
			copies = append(copies, nteBootstrapFileCopy{sourcePath: path, targetPath: filepath.Join(targetDir, filepath.Base(path))})
		}
	}

	install, err = prepareNteBootstrapInstall(copies)
	if err != nil {
		return nil, err
	}
	m.emitNteBootstrapProgress("installing", float64Pointer(96), "", "")
	if err := installNteBootstrapCopies(copies, !directoryWritableOrCreatable(targetDir)); err != nil {
		return install, err
	}
	m.emitNteBootstrapProgress("completed", float64Pointer(100), "", "")
	return install, nil
}

func (m *Mod) downloadAndExtractNteBootstrap(ctx context.Context, rawURL, archiveName, tempDir string) error {
	archivePath := filepath.Join(tempDir, archiveName)
	extractDir, err := os.MkdirTemp(tempDir, "extract-*")
	if err != nil {
		return err
	}
	m.emitNteBootstrapProgress("downloading", nil, archiveName, "")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	response, err := m.http.HTTPClient().Do(request)
	if err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_DOWNLOAD_FAILED:%s: %w", rawURL, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("NTE_BOOTSTRAP_DOWNLOAD_FAILED:%s (HTTP %d)", rawURL, response.StatusCode)
	}
	output, err := os.OpenFile(archivePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, response.Body)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}

	m.emitNteBootstrapProgress("extracting", nil, archiveName, "")
	flatten := false
	if _, err := m.archive.Extract(ctx, archivePath, extractDir, infra.ExtractOptions{FlattenSingleRoot: &flatten}, nil); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_EXTRACT_FAILED:%s: %w", archiveName, err)
	}
	return nil
}

func (m *Mod) emitNteBootstrapProgress(phase string, progress *float64, archiveName, message string) {
	if m != nil && m.emit != nil {
		m.emit(nteBootstrapEvent, NteBootstrapProgress{
			Phase: phase, Progress: progress, ArchiveName: archiveName, Message: message,
		})
	}
}

func nteBootstrapFilesInstalled(targetDir string) bool {
	for _, name := range nteBootstrapRequiredFiles {
		if !fileExists(filepath.Join(targetDir, name)) {
			return false
		}
	}
	return true
}

func collectNteBootstrapFiles(root string) ([]string, error) {
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func findNteBootstrapFile(files []string, name string) string {
	for _, path := range files {
		if strings.EqualFold(filepath.Base(path), name) {
			return path
		}
	}
	return ""
}

func prepareNteBootstrapInstall(copies []nteBootstrapFileCopy) (*nteBootstrapInstall, error) {
	rollbackDir, err := os.MkdirTemp("", "nte-bootstrap-rollback-*")
	if err != nil {
		return nil, err
	}
	install := &nteBootstrapInstall{rollbackDir: rollbackDir, snapshots: make([]nteBootstrapSnapshot, 0, len(copies))}
	for index, file := range copies {
		snapshot := nteBootstrapSnapshot{
			targetPath: file.targetPath,
			backupPath: filepath.Join(rollbackDir, fmt.Sprintf("%d-%s", index, filepath.Base(file.targetPath))),
		}
		if info, err := os.Stat(file.targetPath); err == nil && info.Mode().IsRegular() {
			snapshot.existed = true
			if err := copyNteBootstrapFile(file.targetPath, snapshot.backupPath); err != nil {
				_ = os.RemoveAll(rollbackDir)
				return nil, err
			}
		} else if err != nil && !os.IsNotExist(err) {
			_ = os.RemoveAll(rollbackDir)
			return nil, err
		}
		install.snapshots = append(install.snapshots, snapshot)
	}
	return install, nil
}

func installNteBootstrapCopies(copies []nteBootstrapFileCopy, elevated bool) error {
	if elevated {
		return elevatedCopyNteBootstrapFiles(copies)
	}
	for _, file := range copies {
		if err := copyNteBootstrapFile(file.sourcePath, file.targetPath); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return elevatedCopyNteBootstrapFiles(copies)
			}
			return err
		}
	}
	return nil
}

func copyNteBootstrapFile(sourcePath, targetPath string) error {
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (i *nteBootstrapInstall) Rollback() error {
	if i == nil {
		return nil
	}
	var rollbackErr error
	for index := len(i.snapshots) - 1; index >= 0; index-- {
		snapshot := i.snapshots[index]
		if snapshot.existed {
			rollbackErr = errors.Join(rollbackErr, copyNteBootstrapFile(snapshot.backupPath, snapshot.targetPath))
		} else if err := os.Remove(snapshot.targetPath); err != nil && !os.IsNotExist(err) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if rollbackErr != nil && errors.Is(rollbackErr, os.ErrPermission) {
		rollbackErr = elevatedRollbackNteBootstrapFiles(i.snapshots)
	}
	removeErr := os.RemoveAll(i.rollbackDir)
	return errors.Join(rollbackErr, removeErr)
}

func (i *nteBootstrapInstall) Commit() error {
	if i == nil {
		return nil
	}
	return os.RemoveAll(i.rollbackDir)
}

func float64Pointer(value float64) *float64 { return &value }
