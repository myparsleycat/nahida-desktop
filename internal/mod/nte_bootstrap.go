package mod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/elevated"
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
	// lease is held from the first protected write until Commit or Rollback, so installing the
	// files and rolling them back share one UAC prompt.
	lease *elevated.FileLease
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

func (m *Mod) ensureNteBootstrapFiles(
	ctx context.Context,
	executablePath string,
) (install *nteBootstrapInstall, returnErr error) {
	targetDir := filepath.Dir(executablePath)
	if info, err := os.Stat(targetDir); err != nil || !info.IsDir() {
		return nil, infra.WithCause(errors.New("NTE_BOOTSTRAP_INVALID_TARGET_DIR"), err)
	}
	if runtime.GOARCH != "amd64" {
		err := fmt.Errorf("NTE_BOOTSTRAP_UNSUPPORTED_ARCH: %s", runtime.GOARCH)
		m.emitNteBootstrapProgress("failed", nil, "", err.Error())
		return nil, err
	}
	if nteBootstrapFilesInstalled(targetDir) {
		m.emitNteBootstrapProgress("completed", lo.ToPtr[float64](100), "", "")
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
	defer func() { m.reportCleanup(os.RemoveAll(tempDir), "ensureNteBootstrapFiles") }()

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
	m.emitNteBootstrapProgress("fetching-release", lo.ToPtr[float64](93), nteASILoaderArchive, "")
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
			copies = append(
				copies,
				nteBootstrapFileCopy{sourcePath: path, targetPath: filepath.Join(targetDir, filepath.Base(path))},
			)
		}
	}

	install, err = prepareNteBootstrapInstall(copies, m.elevated)
	if err != nil {
		return nil, err
	}
	m.emitNteBootstrapProgress("installing", lo.ToPtr[float64](96), "", "")
	if err := install.copyFiles(ctx, copies, !directoryWritableOrCreatable(targetDir)); err != nil {
		return install, err
	}
	m.emitNteBootstrapProgress("completed", lo.ToPtr[float64](100), "", "")
	return install, nil
}

func (m *Mod) downloadAndExtractNteBootstrap(ctx context.Context, rawURL, archiveName, tempDir string) error {
	archivePath := filepath.Join(tempDir, archiveName)
	extractDir, err := os.MkdirTemp(tempDir, "extract-*")
	if err != nil {
		return err
	}
	m.emitNteBootstrapProgress("downloading", nil, archiveName, "")
	if err := m.download.File(ctx, infra.DownloadRequest{URL: rawURL, Destination: archivePath}); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_DOWNLOAD_FAILED:%s: %w", rawURL, err)
	}

	m.emitNteBootstrapProgress("extracting", nil, archiveName, "")
	flatten := false
	if _, err := m.archive.Extract(
		ctx,
		archivePath,
		extractDir,
		infra.ExtractOptions{FlattenSingleRoot: &flatten},
		nil,
	); err != nil {
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

func prepareNteBootstrapInstall(
	copies []nteBootstrapFileCopy,
	gateway elevated.FileGateway,
) (*nteBootstrapInstall, error) {
	rollbackDir, err := os.MkdirTemp("", "nte-bootstrap-rollback-*")
	if err != nil {
		return nil, err
	}
	install := &nteBootstrapInstall{
		rollbackDir: rollbackDir, snapshots: make([]nteBootstrapSnapshot, 0, len(copies)),
		lease: elevated.NewFileLease(gateway),
	}
	for index, file := range copies {
		snapshot := nteBootstrapSnapshot{
			targetPath: file.targetPath,
			backupPath: filepath.Join(rollbackDir, fmt.Sprintf("%d-%s", index, filepath.Base(file.targetPath))),
		}
		if info, err := os.Stat(file.targetPath); err == nil && info.Mode().IsRegular() {
			snapshot.existed = true
			if err := copyNteBootstrapFile(file.targetPath, snapshot.backupPath); err != nil {
				return nil, infra.WithCause(err, os.RemoveAll(rollbackDir))
			}
		} else if err != nil && !os.IsNotExist(err) {
			return nil, infra.WithCause(err, os.RemoveAll(rollbackDir))
		}
		install.snapshots = append(install.snapshots, snapshot)
	}
	return install, nil
}

func (i *nteBootstrapInstall) copyFiles(ctx context.Context, copies []nteBootstrapFileCopy, useElevated bool) error {
	if useElevated {
		return elevatedCopyNteBootstrapFiles(ctx, i.lease, copies)
	}
	for _, file := range copies {
		if err := copyNteBootstrapFile(file.sourcePath, file.targetPath); err != nil {
			if errors.Is(err, os.ErrPermission) {
				return elevatedCopyNteBootstrapFiles(ctx, i.lease, copies)
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
	defer i.lease.Release()

	// A file the install never got to replace is left alone: restoring it would ask for rights
	// the install was refused and report a rollback failure for a folder that is unchanged.
	pending := lo.Filter(i.snapshots, func(snapshot nteBootstrapSnapshot, _ int) bool {
		return !snapshot.existed || snapshot.replaced()
	})
	var rollbackErr error
	for _, snapshot := range slices.Backward(pending) {
		if snapshot.existed {
			rollbackErr = errors.Join(rollbackErr, copyNteBootstrapFile(snapshot.backupPath, snapshot.targetPath))
		} else if err := os.Remove(snapshot.targetPath); err != nil && !os.IsNotExist(err) {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if rollbackErr != nil && errors.Is(rollbackErr, os.ErrPermission) {
		// A rollback also follows a cancelled action, so it does not run under that action's context.
		rollbackErr = elevatedRollbackNteBootstrapFiles(context.Background(), i.lease, pending)
	}

	// After a failed rollback the backups are the only copy of the files the install replaced.
	if rollbackErr != nil && i.backupsStillNeeded() {
		return infra.AnnotateError(rollbackErr, infra.Diagnostic{
			Stage: "rollback", Fields: map[string]any{"backupDir": i.rollbackDir, "backupsKept": true},
		})
	}
	return errors.Join(rollbackErr, os.RemoveAll(i.rollbackDir))
}

func (i *nteBootstrapInstall) backupsStillNeeded() bool {
	return slices.ContainsFunc(i.snapshots, func(snapshot nteBootstrapSnapshot) bool {
		return snapshot.existed && snapshot.replaced()
	})
}

// replaced reports whether the target no longer holds what its backup does. A file that cannot
// be compared counts as replaced.
func (s nteBootstrapSnapshot) replaced() bool {
	backup, backupErr := os.ReadFile(s.backupPath)
	target, targetErr := os.ReadFile(s.targetPath)
	return backupErr != nil || targetErr != nil || !bytes.Equal(backup, target)
}

func (i *nteBootstrapInstall) Commit() error {
	if i == nil {
		return nil
	}
	i.lease.Release()
	return os.RemoveAll(i.rollbackDir)
}
