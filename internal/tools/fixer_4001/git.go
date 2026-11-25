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

	fixer4001GitSourceKey = "mod_tools:4001-fixer:git-source"
)

// Git sources the DLL build can run.
const (
	GitSourceSystem  = "system"
	GitSourceBundled = "bundled"
)

// Fixer4001GitSource is the stored Git source and the git.exe that PATH resolves to, "" when there is none.
type Fixer4001GitSource struct {
	Source     string `json:"source"`
	SystemPath string `json:"systemPath"`
}

// minGitRelease is a portable Git for Windows build, used when the user chooses the bundled Git or PATH has
// none.
type minGitRelease struct {
	version, url, sha256 string
}

var pinnedMinGit = minGitRelease{
	version: "2.56.0",
	url:     "https://github.com/git-for-windows/git/releases/download/v2.56.0.windows.1/MinGit-2.56.0-64-bit.zip",
	sha256:  "064b440ff870ed5198527e8f3a92cdf5bd2fd0fedf5e718af95e3fdaddeff718",
}

func (t *Service) FourThousandOneFixerGetGitSource(ctx context.Context) (Fixer4001GitSource, error) {
	source, err := t.gitSource(ctx)
	if err != nil {
		return Fixer4001GitSource{}, err
	}
	return Fixer4001GitSource{Source: source, SystemPath: t.systemGitPath()}, nil
}

func (t *Service) FourThousandOneFixerSetGitSource(ctx context.Context, source string) error {
	if source != GitSourceSystem && source != GitSourceBundled {
		return fmt.Errorf("unknown Git source %q", source)
	}
	return t.setAppState(ctx, fixer4001GitSourceKey, source)
}

func (t *Service) gitSource(ctx context.Context) (string, error) {
	value, err := t.getAppState(ctx, fixer4001GitSourceKey)
	if err != nil {
		return "", err
	}
	if value != nil && *value == GitSourceBundled {
		return GitSourceBundled, nil
	}
	return GitSourceSystem, nil
}

func (t *Service) systemGitPath() string {
	path, err := t.lookPath("git.exe")
	if err != nil {
		return ""
	}
	path, err = filepath.Abs(path)
	if err != nil || !isLocalFilesystemPath(path) || !regularFile(path) {
		return ""
	}
	return path
}

// resolveBuildGit returns the git.exe the build runs and whether it is the bundled release. The system source
// falls back to the bundled release when PATH has no Git, which is also what the UI shows as selected then.
func (t *Service) resolveBuildGit(ctx context.Context, release minGitRelease) (path string, bundled bool, err error) {
	source, err := t.gitSource(ctx)
	if err != nil {
		return "", false, err
	}
	if source == GitSourceSystem {
		if system := t.systemGitPath(); system != "" {
			t.log.Info("Building with the Git on PATH at "+system, "4001Fixer:resolveBuildGit")
			return system, false, nil
		}
		t.log.Info("No Git on PATH; building with the bundled Git", "4001Fixer:resolveBuildGit")
	}
	path, err = t.locateGit(ctx, release)
	return path, err == nil, err
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
	t.log.Info(fmt.Sprintf("Installed portable Git %s at %s", release.version, installRoot), "4001Fixer:locateGit")
	return gitPath, nil
}
