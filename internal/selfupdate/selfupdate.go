// Package selfupdate replaces the running executable with a newer GitHub release of the application. A
// verified release is staged beside the executable, and a copy of the executable started in helper mode
// swaps it in once the application has exited.
package selfupdate

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
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// The asset names are the release contract of build/windows/publish-updater.ps1.
const (
	assetName          = "nahida-desktop-windows-amd64.exe"
	checksumAsset      = "SHA256SUMS"
	stagingPrefix      = "nahida-update-"
	readyFileName      = "helper.ready"
	maxChecksumBytes   = 64 << 10
	maxExecutableBytes = 1 << 30
	helperReadyTimeout = 30 * time.Second
)

var (
	ErrNoPendingRelease = errors.New("no update release has been checked")
	ErrNotReady         = errors.New("no update is staged for installation")
	// ErrRestartPending reports that the staged update belongs to a swap helper waiting for this process to exit.
	ErrRestartPending   = errors.New("an update restart is already in progress")
	ErrHelperNotReady   = errors.New("update helper did not signal readiness")
	ErrChecksumMismatch = errors.New("update checksum mismatch")
)

var releaseRepo = github.Repo{Owner: "myparsleycat", Name: "nahida-desktop"}

type Options struct {
	// Version is the running version; empty uses platform.AppVersion.
	Version string
	GitHub  *github.Client
	Log     *infra.Log
	// Quit starts the application's normal shutdown once the swap helper waits for this process to exit.
	Quit func()
	// HelperLog is the file the swap helper appends its progress to; empty writes no helper log.
	HelperLog string
}

// Engine implements infra.UpdateEngine.
type Engine struct {
	version   string
	helperLog string
	github    *github.Client
	log       *infra.Log
	quit      func()

	executable   func() (string, error)
	spawn        func(executable string, env []string) (helperProcess, error)
	readyTimeout time.Duration

	downloadMu sync.Mutex

	mu         sync.Mutex
	pending    *pendingRelease
	staged     string
	stagingDir string
	// restarting records that Restart handed the staging directory to a swap helper, which renames the staged
	// executable into place after this process exits. Until then nothing here may delete or replace it, and it
	// is released only when the helper is known not to be waiting.
	restarting bool
}

type pendingRelease struct {
	tag    string
	digest []byte
}

type helperProcess interface {
	Abort()
	Detach()
}

func New(opts Options) (*Engine, error) {
	if opts.GitHub == nil {
		return nil, errors.New("self-update GitHub client is not configured")
	}
	if opts.Quit == nil {
		return nil, errors.New("self-update quit is not configured")
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = platform.AppVersion
	}
	return &Engine{
		version:      version,
		helperLog:    opts.HelperLog,
		github:       opts.GitHub,
		log:          opts.Log,
		quit:         opts.Quit,
		executable:   currentExecutable,
		spawn:        spawnHelper,
		readyTimeout: helperReadyTimeout,
	}, nil
}

// Check returns the newest release above the running version that carries the executable and its checksum,
// or nil when none does. The release list goes through the shared GitHub client, so the caller's
// infra.WithGitHub* context values decide its cache use and refresh.
func (e *Engine) Check(ctx context.Context, includePrerelease bool) (*infra.UpdateRelease, error) {
	releases, err := e.github.AllReleases(ctx, releaseRepo)
	if err != nil {
		return nil, err
	}
	release, found := selectRelease(releases, e.version, includePrerelease)
	if !found {
		e.mu.Lock()
		e.pending = nil
		e.mu.Unlock()
		return nil, nil
	}

	tag := release.TagName
	sumsURL := github.ReleaseFileURL(releaseRepo, tag, checksumAsset)
	sums, err := e.github.FetchFile(ctx, releaseRepo, sumsURL, maxChecksumBytes)
	if err != nil {
		return nil, infra.AnnotateError(fmt.Errorf("read %s of %s: %w", checksumAsset, tag, err), infra.Diagnostic{
			Operation: "self-update", Stage: "checksum",
			Fields: map[string]any{"tag": tag, "url": infra.SanitizeLogURL(sumsURL)},
		})
	}
	digest, err := checksumFor(sums, assetName)
	if err != nil {
		return nil, infra.AnnotateError(fmt.Errorf("release %s: %w", tag, err), infra.Diagnostic{
			Operation: "self-update", Stage: "checksum", Fields: map[string]any{"tag": tag, "asset": assetName},
		})
	}

	e.mu.Lock()
	e.pending = &pendingRelease{tag: tag, digest: digest}
	e.mu.Unlock()
	channel := ""
	if release.Prerelease {
		channel = infra.UpdateChannelPrerelease
	}
	return &infra.UpdateRelease{Version: strings.TrimPrefix(tag, "v"), Channel: channel, Notes: release.Body}, nil
}

func selectRelease(releases []github.Release, current string, includePrerelease bool) (github.Release, bool) {
	current = canonicalVersion(current)
	var best github.Release
	found := false
	for _, release := range releases {
		if release.Draft || release.Prerelease && !includePrerelease {
			continue
		}
		tag := canonicalVersion(release.TagName)
		if !semver.IsValid(tag) || semver.Compare(tag, current) <= 0 {
			continue
		}
		if !hasAsset(release, assetName) || !hasAsset(release, checksumAsset) {
			continue
		}
		if !found || semver.Compare(tag, canonicalVersion(best.TagName)) > 0 {
			best, found = release, true
		}
	}
	return best, found
}

func canonicalVersion(version string) string {
	return "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func hasAsset(release github.Release, name string) bool {
	for _, asset := range release.Assets {
		if asset.Name == name {
			return true
		}
	}
	return false
}

// checksumFor reads the digest of name from sha256sum output, whose lines are "<hex>  <name>" with an
// optional "*" binary-mode marker before the name.
func checksumFor(sums []byte, name string) ([]byte, error) {
	for line := range strings.Lines(string(sums)) {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("malformed %s entry for %s", checksumAsset, name)
		}
		return digest, nil
	}
	return nil, fmt.Errorf("%s has no entry for %s", checksumAsset, name)
}

// DownloadAndInstall stages the release found by the last Check beside the executable, on its volume, so the
// helper's swap is a rename. A previously staged release stays in place until the new one is verified.
func (e *Engine) DownloadAndInstall(ctx context.Context, progress func(written, total int64)) error {
	e.downloadMu.Lock()
	defer e.downloadMu.Unlock()

	e.mu.Lock()
	pending, restarting, current := e.pending, e.restarting, e.stagingDir
	e.mu.Unlock()
	if restarting {
		return ErrRestartPending
	}
	if pending == nil {
		return ErrNoPendingRelease
	}
	target, err := e.executable()
	if err != nil {
		return infra.AnnotateError(fmt.Errorf("resolve executable: %w", err), infra.Diagnostic{
			Operation: "self-update", Stage: "resolve-executable",
		})
	}
	e.sweepStaging(filepath.Dir(target), current)

	dir, err := os.MkdirTemp(filepath.Dir(target), stagingPrefix+"*")
	if err != nil {
		return infra.AnnotateError(fmt.Errorf("create update staging directory: %w", err), infra.Diagnostic{
			Operation: "self-update", Stage: "stage", Fields: map[string]any{"executable": target},
		})
	}
	staged := filepath.Join(dir, assetName)
	assetURL := github.ReleaseFileURL(releaseRepo, pending.tag, assetName)
	fields := map[string]any{"tag": pending.tag, "url": infra.SanitizeLogURL(assetURL), "staged": staged}
	err = e.github.DownloadFile(ctx, github.FileRequest{
		Repo: releaseRepo, URL: assetURL, Destination: staged, MaxSize: maxExecutableBytes, Progress: progress,
	})
	if err != nil {
		e.removeStaging(dir)
		return infra.AnnotateError(fmt.Errorf("download update %s: %w", pending.tag, err), infra.Diagnostic{
			Operation: "self-update", Stage: "download", Fields: fields,
		})
	}
	if err := verifyDigest(ctx, staged, pending.digest); err != nil {
		e.removeStaging(dir)
		return infra.AnnotateError(err, infra.Diagnostic{Operation: "self-update", Stage: "verify", Fields: fields})
	}

	e.mu.Lock()
	if e.restarting {
		e.mu.Unlock()
		e.removeStaging(dir)
		return ErrRestartPending
	}
	previous := e.stagingDir
	e.staged, e.stagingDir = staged, dir
	e.mu.Unlock()
	if previous != "" {
		e.removeStaging(previous)
	}
	return nil
}

func verifyDigest(ctx context.Context, path string, want []byte) error {
	release, err := diskio.Acquire(ctx, path)
	if err != nil {
		return err
	}
	defer release()

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open staged update: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash staged update: %w", err)
	}
	if !bytes.Equal(hash.Sum(nil), want) {
		return fmt.Errorf("%w: %s", ErrChecksumMismatch, filepath.Base(path))
	}
	return nil
}

// sweepStaging removes staging directories an earlier process left beside the executable, keeping the one
// this engine still holds.
func (e *Engine) sweepStaging(parent, keep string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		e.warn(err, "sweep-staging", map[string]any{"directory": parent})
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), stagingPrefix) {
			continue
		}
		dir := filepath.Join(parent, entry.Name())
		if keep != "" && platform.SamePathFold(dir, keep) {
			continue
		}
		e.removeStaging(dir)
	}
}

func (e *Engine) removeStaging(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		e.warn(err, "remove-staging", map[string]any{"directory": dir})
	}
}

func (e *Engine) warn(err error, stage string, fields map[string]any) {
	_ = infra.ReportError(e.log, err, "SelfUpdate", infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "self-update", Stage: stage, Fields: fields,
	})
}

// Restart starts the swap helper and, once it reports that it is waiting for this process, asks the
// application to quit. The application keeps running when the helper cannot be started.
func (e *Engine) Restart(ctx context.Context) error {
	target, err := e.executable()
	if err != nil {
		return infra.AnnotateError(fmt.Errorf("resolve executable: %w", err), infra.Diagnostic{
			Operation: "self-update", Stage: "resolve-executable",
		})
	}

	// The staged path, its directory and the claim are taken together, so a download that starts meanwhile
	// either finishes before the claim or sees it and leaves the directory alone.
	e.mu.Lock()
	if e.restarting {
		e.mu.Unlock()
		return ErrRestartPending
	}
	staged, dir := e.staged, e.stagingDir
	if staged == "" {
		e.mu.Unlock()
		return ErrNotReady
	}
	e.stagingDir, e.restarting = "", true
	e.mu.Unlock()

	fields := map[string]any{"executable": target, "staged": staged}
	readyPath := filepath.Join(dir, readyFileName)
	_ = os.Remove(readyPath)
	defer func() { _ = os.Remove(readyPath) }()
	env := append(os.Environ(),
		envHelper+"=1",
		envTarget+"="+target,
		envStaged+"="+staged,
		envParentPID+"="+strconv.Itoa(os.Getpid()),
		envReady+"="+readyPath,
		envLog+"="+e.helperLog,
	)
	process, err := e.spawn(target, env)
	if err != nil {
		e.reclaim(dir)
		return infra.AnnotateError(fmt.Errorf("start update helper: %w", err), infra.Diagnostic{
			Operation: "self-update", Stage: "spawn-helper", Fields: fields,
		})
	}
	if err := waitForReady(ctx, readyPath, e.readyTimeout); err != nil {
		process.Abort()
		e.reclaim(dir)
		return infra.AnnotateError(err, infra.Diagnostic{
			Operation: "self-update", Stage: "helper-ready", Fields: fields,
		})
	}
	process.Detach()

	// The helper keeps its claim even if the shutdown is vetoed: it still swaps the executable when this
	// process exits within its wait.
	e.quit()
	return nil
}

func (e *Engine) reclaim(dir string) {
	e.mu.Lock()
	e.stagingDir, e.restarting = dir, false
	e.mu.Unlock()
}

func waitForReady(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		payload, err := os.ReadFile(path)
		switch {
		case err == nil:
			status := strings.TrimSpace(string(payload))
			if status == helperReady {
				return nil
			}
			if detail, failed := strings.CutPrefix(status, helperErrorPrefix); failed {
				return fmt.Errorf("%w: %s", ErrHelperNotReady, strings.TrimSpace(detail))
			}
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("%w: read readiness signal: %w", ErrHelperNotReady, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w within %s", ErrHelperNotReady, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
