package fixer4001

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

const (
	minGitDirPrefix       = "mingit-"
	minGitDownloadTimeout = 10 * time.Minute
)

// minGitRelease is a portable Git for Windows build. The build always uses it instead of a Git on PATH so
// that every machine runs the same Git, whatever the user has installed or configured system-wide.
type minGitRelease struct {
	version, url, sha256 string
}

var pinnedMinGit = minGitRelease{
	version: "2.56.0",
	url:     "https://github.com/git-for-windows/git/releases/download/v2.56.0.windows.1/MinGit-2.56.0-64-bit.zip",
	sha256:  "064b440ff870ed5198527e8f3a92cdf5bd2fd0fedf5e718af95e3fdaddeff718",
}

// locateGit returns the MinGit release kept under the app data tools directory, downloading it first when
// it is not there yet.
func (t *Service) locateGit(ctx context.Context, release minGitRelease) (gitPath string, err error) {
	stage := "resolve-tools-directory"
	installRoot, staging := "", ""
	defer func() {
		if err == nil {
			return
		}
		err = infra.AnnotateError(
			fmt.Errorf("set up the portable Git that building XXMI libraries requires: %w", err),
			infra.Diagnostic{
				Operation: "4001Fixer", Stage: stage,
				Fields: map[string]any{
					"url": release.url, "version": release.version,
					"installPath": installRoot, "stagingPath": staging,
				},
			},
		)
	}()
	if t.appData == nil || t.download == nil || t.archive == nil {
		return "", errors.New("portable Git installer is unavailable")
	}
	toolsRoot, err := t.appData.EnsureDir(appdata.ToolsDir)
	if err != nil {
		return "", err
	}
	installRoot = filepath.Join(toolsRoot, minGitDirPrefix+release.version)
	gitPath = filepath.Join(installRoot, "cmd", "git.exe")
	if regularFile(gitPath) {
		return gitPath, nil
	}

	stage = "download"
	t.update4001Progress("XXMI_DOWNLOAD_GIT", "")
	staging, err = os.MkdirTemp(toolsRoot, "."+minGitDirPrefix+"install-")
	if err != nil {
		return "", fmt.Errorf("create portable Git staging directory: %w", err)
	}
	defer func() { t.reportCleanup(os.RemoveAll(staging), "locateGit") }()
	archivePath := filepath.Join(staging, "mingit.zip")
	downloadCtx, cancel := context.WithTimeout(ctx, minGitDownloadTimeout)
	defer cancel()
	if err := t.download.File(downloadCtx, infra.DownloadRequest{
		URL: release.url, Destination: archivePath,
		Header: http.Header{"User-Agent": []string{"Nahida Desktop"}},
	}); err != nil {
		return "", fmt.Errorf("download portable Git: %w", err)
	}

	// The archive holds executables that run during the build, so nothing is extracted before it matches the pin.
	stage = "verify"
	sum, err := hashFile(archivePath)
	if err != nil {
		return "", fmt.Errorf("hash portable Git archive: %w", err)
	}
	if !strings.EqualFold(sum, release.sha256) {
		return "", fmt.Errorf("portable Git archive checksum mismatch: got %s", sum)
	}

	stage = "extract"
	extracted, err := t.archive.Extract(
		ctx,
		archivePath,
		filepath.Join(staging, "extract"),
		infra.ExtractOptions{FlattenSingleRoot: lo.ToPtr(false)},
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("extract portable Git: %w", err)
	}
	if !regularFile(filepath.Join(extracted, "cmd", "git.exe")) {
		return "", errors.New("portable Git archive is missing cmd/git.exe")
	}

	// An existing directory without git.exe is a broken install; a rename onto it would fail.
	stage = "install"
	if err := os.RemoveAll(installRoot); err != nil {
		return "", fmt.Errorf("remove incomplete portable Git: %w", err)
	}
	if err := os.Rename(extracted, installRoot); err != nil {
		return "", fmt.Errorf("install portable Git: %w", err)
	}

	entries, err := os.ReadDir(toolsRoot)
	t.reportCleanup(err, "locateGit")
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), minGitDirPrefix) &&
			entry.Name() != filepath.Base(installRoot) {
			t.reportCleanup(os.RemoveAll(filepath.Join(toolsRoot, entry.Name())), "locateGit")
		}
	}
	if t.log != nil {
		t.log.Info(fmt.Sprintf("Installed portable Git %s at %s", release.version, installRoot), "4001Fixer:locateGit")
	}
	return gitPath, nil
}
