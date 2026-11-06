package mod

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const nteBootstrapEvent = "mod:nte-bootstrap-progress"

// nteBootstrapArchive is a pinned release file installed into the NTE game folder. The files run inside
// the game process, so each archive is checked against its SHA-256 before it is extracted.
type nteBootstrapArchive struct {
	repo   github.Repo
	name   string
	url    string
	sha256 string
	// renames maps an archive entry to the name it is installed under.
	renames map[string]string
}

func defaultNteBootstrapArchives() []nteBootstrapArchive {
	sigBypasser := github.Repo{Owner: "rm-NoobInCoding", Name: "UniversalSigBypasser"}
	asiLoader := github.Repo{Owner: "ThirteenAG", Name: "Ultimate-ASI-Loader"}
	return []nteBootstrapArchive{
		{
			repo:   sigBypasser,
			name:   "SigBypasser_v1.2.zip",
			url:    github.ReleaseFileURL(sigBypasser, "v1.2", "SigBypasser_v1.2.zip"),
			sha256: "c2a1f2d72aa2b7c5c47b1074d589c358d5a1bc5812cc48980af6ba57bbbfa344",
		},
		{
			// The loader picks the DLL it proxies from its own file name.
			repo:    asiLoader,
			name:    "Ultimate-ASI-Loader_x64.zip",
			url:     github.ReleaseFileURL(asiLoader, "v10.0.2", "Ultimate-ASI-Loader_x64.zip"),
			sha256:  "a99e188e1f40d7aac9db9c0339990332152d372e0359faa06a52fa339e8b218f",
			renames: map[string]string{"dinput8.dll": "winhttp.dll"},
		},
	}
}

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
	if m.archive == nil || m.github == nil {
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

	for index, archive := range m.nteBootstrapArchives {
		var progress *float64
		if index > 0 {
			progress = lo.ToPtr(float64(90 + 3*index))
		}
		m.emitNteBootstrapProgress("fetching-release", progress, archive.name, "")
		if err := m.downloadAndExtractNteBootstrap(ctx, archive, tempDir); err != nil {
			return nil, err
		}
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

func (m *Mod) downloadAndExtractNteBootstrap(ctx context.Context, archive nteBootstrapArchive, tempDir string) error {
	archiveName := archive.name
	archivePath := filepath.Join(tempDir, archiveName)
	extractDir, err := os.MkdirTemp(tempDir, "extract-*")
	if err != nil {
		return err
	}
	m.emitNteBootstrapProgress("downloading", nil, archiveName, "")
	if err := m.github.DownloadFile(ctx, github.FileRequest{
		Repo: archive.repo, URL: archive.url, Destination: archivePath,
	}); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_DOWNLOAD_FAILED:%s: %w", infra.SanitizeLogURL(archive.url), err)
	}
	if err := verifyNteBootstrapArchive(archivePath, archive.sha256); err != nil {
		return fmt.Errorf("NTE_BOOTSTRAP_CHECKSUM_MISMATCH:%s: %w", archiveName, err)
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
	if len(archive.renames) == 0 {
		return nil
	}
	files, err := collectNteBootstrapFiles(extractDir)
	if err != nil {
		return err
	}
	for from, to := range archive.renames {
		source := findNteBootstrapFile(files, from)
		if source == "" {
			return fmt.Errorf("NTE_BOOTSTRAP_FILE_MISSING:%s", from)
		}
		if err := os.Rename(source, filepath.Join(filepath.Dir(source), to)); err != nil {
			return err
		}
	}
	return nil
}

func verifyNteBootstrapArchive(path, want string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 %s, want %s", got, want)
	}
	return nil
}

func (m *Mod) emitNteBootstrapProgress(phase string, progress *float64, archiveName, message string) {
	if m.emit != nil {
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
	// The DLLs are loaded by the game, so a copy cut short must never take the place of a working file;
	// whether the install is complete is later judged by the files existing.
	output, err := os.CreateTemp(filepath.Dir(targetPath), ".nhd-write-*")
	if err != nil {
		return err
	}
	temporary := output.Name()
	_, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return infra.WithCause(err, os.Remove(temporary))
	}
	if err := platform.ReplaceAtomic(temporary, targetPath); err != nil {
		return infra.WithCause(err, os.Remove(temporary))
	}
	return nil
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
