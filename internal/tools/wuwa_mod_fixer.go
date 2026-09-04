package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

const (
	wuwaReleasesLatestURL = "https://api.github.com/repos/Moonholder/Wuwa_Mod_Fixer/releases/latest"
	wuwaConfigURL         = "https://raw.githubusercontent.com/Moonholder/Wuwa_Mod_Fixer/refs/heads/main/config.json"
	wuwaFixerDirName      = "wuwa-mod-fixer"
	wuwaCheckCooldown     = 2 * time.Minute
	wuwaAutoUpdateEvery   = time.Hour

	wuwaLastCheckKey        = "github:wuwa-mod-fixer:last-check"
	wuwaInstalledVersionKey = "mod_tools:wuwa-mod-fixer:installed-version"
	wuwaBinaryPathKey       = "mod_tools:wuwa-mod-fixer:binary-path"
	wuwaLatestReleaseKey    = "mod_tools:wuwa-mod-fixer:latest-release"
	githubCoreRateKey       = "github:core-rate"
	wuwaMaxDownloadSize     = 512 << 20
)

var (
	wuwaBackupRE        = regexp.MustCompile(`(?i)^(.*)_(\d{4}-\d{2}-\d{2} \d{2}-\d{2}-\d{2}(?:\.\d{3})?)\.BAK$`)
	wuwaBinaryRE        = regexp.MustCompile(`(?i)^Wuwa_Mod_Fixer_v.+\.exe$`)
	wuwaBinaryVersionRE = regexp.MustCompile(`(?i)^Wuwa_Mod_Fixer_(v[\d.]+)\.exe$`)
)

type GitHubRateState struct {
	Limit     int64  `json:"limit"`
	Remaining int64  `json:"remaining"`
	Reset     int64  `json:"reset"`
	Used      int64  `json:"used"`
	Resource  string `json:"resource"`
	UpdatedAt string `json:"updatedAt"`
}

type WuwaFixerOptions struct {
	DerivedHashes bool   `json:"derivedHashes"`
	StableTexture bool   `json:"stableTexture"`
	AemeathMech   bool   `json:"aemeathMech"`
	Rendering33   bool   `json:"rendering33"`
	AeroFix       string `json:"aeroFix"`
}

type WuwaBackupFile struct {
	CurrentPath  string `json:"currentPath"`
	OriginalPath string `json:"originalPath"`
	Timestamp    string `json:"timestamp"`
	GroupKey     string `json:"groupKey"`
}

type WuwaBackupGroup struct {
	GroupKey string           `json:"groupKey"`
	Files    []WuwaBackupFile `json:"files"`
}

type WuwaBackupSize struct {
	Bytes int64 `json:"bytes"`
	Count int   `json:"count"`
}

type WuwaFixerStatus struct {
	Supported        bool             `json:"supported"`
	Installed        bool             `json:"installed"`
	InstalledVersion *string          `json:"installedVersion"`
	LatestVersion    *string          `json:"latestVersion"`
	BinaryPath       *string          `json:"binaryPath"`
	UpdateAvailable  bool             `json:"updateAvailable"`
	RateState        *GitHubRateState `json:"rateState"`
	RateLimited      bool             `json:"rateLimited"`
	NextCheckAt      *string          `json:"nextCheckAt"`
}

type WuwaFixerPrepareResult struct {
	WuwaFixerStatus
	NeedsInstall    bool `json:"needsInstall"`
	CheckedRemotely bool `json:"checkedRemotely"`
}

type wuwaLatestAsset struct {
	Name               string  `json:"name"`
	BrowserDownloadURL string  `json:"browserDownloadUrl"`
	Digest             *string `json:"digest"`
}

type wuwaLatestReleaseCache struct {
	Version   string          `json:"version"`
	Asset     wuwaLatestAsset `json:"asset"`
	CheckedAt string          `json:"checkedAt"`
}

type wuwaReleaseResponse struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string  `json:"name"`
		BrowserDownloadURL string  `json:"browser_download_url"`
		Digest             *string `json:"digest"`
	} `json:"assets"`
}

type wuwaRefreshResult struct {
	LatestRelease   *wuwaLatestReleaseCache
	RateState       *GitHubRateState
	RateLimited     bool
	CheckedRemotely bool
	NextCheckAt     *string
}

type wuwaInstalledInfo struct {
	Exists     bool
	BinaryPath *string
	Version    *string
}

func (t *Tools) WuwaFixerGetRateStatus(ctx context.Context) (*GitHubRateState, error) {
	return t.wuwaGetRateState(ctx)
}

func (t *Tools) WuwaFixerGetStatus(ctx context.Context, importer *string) (WuwaFixerStatus, error) {
	installed, err := t.wuwaGetInstalledBinaryInfo(ctx)
	if err != nil {
		return WuwaFixerStatus{}, err
	}
	rate, err := t.wuwaGetRateState(ctx)
	if err != nil {
		return WuwaFixerStatus{}, err
	}
	latest, err := t.wuwaGetCachedLatestRelease(ctx)
	if err != nil {
		return WuwaFixerStatus{}, err
	}
	status := WuwaFixerStatus{
		Supported: t.wuwaSupportedImporter(importer), Installed: installed.Exists,
		InstalledVersion: installed.Version, BinaryPath: installed.BinaryPath,
		RateState: rate, RateLimited: wuwaRateLimited(rate),
	}
	if latest != nil {
		status.LatestVersion = &latest.Version
		next := mustRFC3339(parseRFC3339(latest.CheckedAt).Add(wuwaCheckCooldown))
		status.NextCheckAt = &next
	}
	status.UpdateAvailable = installed.Version != nil && status.LatestVersion != nil && compareToolVersions(*status.LatestVersion, *installed.Version) > 0
	return status, nil
}

func (t *Tools) WuwaFixerPrepareRun(ctx context.Context, importer *string) (WuwaFixerPrepareResult, error) {
	base, err := t.WuwaFixerGetStatus(ctx, importer)
	if err != nil {
		return WuwaFixerPrepareResult{}, err
	}
	result := WuwaFixerPrepareResult{WuwaFixerStatus: base, NeedsInstall: !base.Installed}
	if !base.Supported {
		return result, nil
	}
	refresh, err := t.wuwaRefreshLatestRelease(ctx, false)
	if err != nil {
		if !base.Installed {
			return WuwaFixerPrepareResult{}, err
		}
		t.logError(err, "WuwaModFixer:prepareRun")
		return result, nil
	}
	if refresh.LatestRelease != nil {
		result.LatestVersion = &refresh.LatestRelease.Version
	}
	result.RateState = refresh.RateState
	result.RateLimited = refresh.RateLimited
	result.NextCheckAt = refresh.NextCheckAt
	result.CheckedRemotely = refresh.CheckedRemotely
	result.UpdateAvailable = result.InstalledVersion != nil && result.LatestVersion != nil && compareToolVersions(*result.LatestVersion, *result.InstalledVersion) > 0
	return result, nil
}

func (t *Tools) WuwaFixerInstallOrUpdate(ctx context.Context) (WuwaFixerStatus, error) {
	t.wuwaInstallMu.Lock()
	defer t.wuwaInstallMu.Unlock()

	release, err := t.wuwaLatestReleaseForInstall(ctx)
	if err != nil {
		return WuwaFixerStatus{}, err
	}
	toolDir, err := t.wuwaToolDir()
	if err != nil {
		return WuwaFixerStatus{}, err
	}
	if err := os.MkdirAll(toolDir, 0o700); err != nil {
		return WuwaFixerStatus{}, fmt.Errorf("create Wuwa Mod Fixer directory: %w", err)
	}
	if filepath.Base(release.Asset.Name) != release.Asset.Name || !wuwaBinaryRE.MatchString(release.Asset.Name) {
		return WuwaFixerStatus{}, errors.New("invalid Wuwa Mod Fixer asset name")
	}
	tempPath := filepath.Join(toolDir, release.Asset.Name+".download")
	finalPath := filepath.Join(toolDir, release.Asset.Name)
	defer func() { _ = os.Remove(tempPath) }()

	body, responseHeader, err := t.wuwaFetchBytes(ctx, release.Asset.BrowserDownloadURL, nil, wuwaMaxDownloadSize)
	if responseHeader != nil {
		_, _ = t.wuwaCaptureRate(ctx, responseHeader)
	}
	if err != nil {
		return WuwaFixerStatus{}, fmt.Errorf("Failed to download Wuwa Mod Fixer: %w", err) //nolint:staticcheck // Electron contract text.
	}
	if err := verifyWuwaDigest(body, release.Asset.Digest); err != nil {
		return WuwaFixerStatus{}, err
	}
	if err := os.WriteFile(tempPath, body, 0o700); err != nil {
		return WuwaFixerStatus{}, fmt.Errorf("write Wuwa Mod Fixer: %w", err)
	}
	if err := replaceAtomic(tempPath, finalPath); err != nil {
		return WuwaFixerStatus{}, fmt.Errorf("install Wuwa Mod Fixer: %w", err)
	}
	if err := t.wuwaCleanupOldBinaries(finalPath); err != nil {
		return WuwaFixerStatus{}, err
	}
	if err := t.setAppState(ctx, wuwaInstalledVersionKey, release.Version); err != nil {
		return WuwaFixerStatus{}, err
	}
	if err := t.setAppState(ctx, wuwaBinaryPathKey, finalPath); err != nil {
		return WuwaFixerStatus{}, err
	}
	wwmi := "WWMI"
	return t.WuwaFixerGetStatus(ctx, &wwmi)
}

func (t *Tools) WuwaFixerRun(ctx context.Context, modPath string, options WuwaFixerOptions) error {
	installed, err := t.wuwaGetInstalledBinaryInfo(ctx)
	if err != nil {
		return err
	}
	if !installed.Exists || installed.BinaryPath == nil {
		return contractError("Wuwa Mod Fixer is not installed")
	}
	if _, err := os.Stat(modPath); err != nil {
		return contractError("Destination path does not exist")
	}
	configPath, err := t.wuwaEnsureLatestConfig(ctx)
	if err != nil {
		return err
	}
	args, err := buildWuwaCLIArgs(modPath, configPath, options)
	if err != nil {
		return err
	}
	run, runCtx, err := t.beginScriptRun(ctx)
	if err != nil {
		return err
	}
	defer t.finishScriptRun(run)
	t.emitFixToolLog("Running Wuwa Mod Fixer...", false)
	if err := run.executor.execute(runCtx, *installed.BinaryPath, db.ScriptTypeExec, modPath, args); err != nil {
		return t.reportRunError(err)
	}
	t.emitFixToolLog("Completed Wuwa Mod Fixer", false)
	return nil
}

func (t *Tools) WuwaFixerScanBackups(ctx context.Context, modPath string) ([]WuwaBackupGroup, error) {
	if err := t.wuwaRequireModPath(ctx, modPath); err != nil {
		t.logError(err, "wuwaFixer:scanBackups:"+modPath)
		return nil, err
	}
	return collectWuwaBackupGroups(modPath)
}

func (t *Tools) WuwaFixerGetBackupSize(ctx context.Context, modPath string) (WuwaBackupSize, error) {
	if err := t.wuwaRequireModPath(ctx, modPath); err != nil {
		t.logError(err, "wuwaFixer:getBackupSize:"+modPath)
		return WuwaBackupSize{}, err
	}
	paths, err := listWuwaBackupFiles(modPath)
	if err != nil {
		return WuwaBackupSize{}, err
	}
	result := WuwaBackupSize{Count: len(paths)}
	for _, bakPath := range paths {
		info, statErr := os.Stat(bakPath)
		if statErr != nil {
			continue
		}
		if info.Size() > 0 {
			result.Bytes += info.Size()
			continue
		}
		match := wuwaBackupRE.FindStringSubmatch(filepath.Base(bakPath))
		if len(match) != 3 {
			continue
		}
		if original, originalErr := os.Stat(filepath.Join(filepath.Dir(bakPath), match[1])); originalErr == nil {
			result.Bytes += original.Size()
		}
	}
	return result, nil
}

func (t *Tools) WuwaFixerRollbackToGroup(ctx context.Context, modPath, groupKey string) error {
	if err := t.wuwaRequireModPath(ctx, modPath); err != nil {
		return err
	}
	groups, err := collectWuwaBackupGroups(modPath)
	if err != nil {
		return err
	}
	found := false
	earliest := make(map[string]WuwaBackupFile)
	var deletePaths []string
	for _, group := range groups {
		if group.GroupKey == groupKey {
			found = true
		}
		if group.GroupKey < groupKey {
			continue
		}
		for _, file := range group.Files {
			deletePaths = append(deletePaths, file.CurrentPath)
			previous, ok := earliest[file.OriginalPath]
			if !ok || file.Timestamp < previous.Timestamp {
				earliest[file.OriginalPath] = file
			}
		}
	}
	if !found {
		return contractError(fmt.Sprintf("Backup group not found: %s", groupKey))
	}
	for originalPath, backup := range earliest {
		info, statErr := os.Stat(backup.CurrentPath)
		if statErr != nil {
			return statErr
		}
		if info.Size() == 0 {
			if err := os.Remove(originalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := copyRegularFile(backup.CurrentPath, originalPath); err != nil {
			return err
		}
	}
	for _, bakPath := range deletePaths {
		if err := os.Remove(bakPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.logError(err, "WuwaModFixer:rollbackCleanup")
		}
	}
	return nil
}

func (t *Tools) WuwaFixerCleanBackups(ctx context.Context, modPath string) error {
	if err := t.wuwaRequireModPath(ctx, modPath); err != nil {
		return err
	}
	paths, err := listWuwaBackupFiles(modPath)
	if err != nil {
		return err
	}
	for _, bakPath := range paths {
		if err := os.Remove(bakPath); err != nil {
			return err
		}
	}
	return nil
}

// StartWuwaAutoUpdateCheck starts the Electron-compatible hourly background check.
//
//wails:ignore
func (t *Tools) StartWuwaAutoUpdateCheck() {
	t.wuwaMu.Lock()
	if t.wuwaAutoCancel != nil {
		t.wuwaMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.wuwaAutoCancel, t.wuwaAutoDone = cancel, done
	t.wuwaMu.Unlock()
	go func() {
		defer close(done)
		t.runWuwaAutomaticUpdateCheck(ctx)
		ticker := time.NewTicker(wuwaAutoUpdateEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.runWuwaAutomaticUpdateCheck(ctx)
			}
		}
	}()
}

func (t *Tools) stopWuwaAutoUpdateCheck() error {
	t.wuwaMu.Lock()
	cancel, done := t.wuwaAutoCancel, t.wuwaAutoDone
	t.wuwaAutoCancel, t.wuwaAutoDone = nil, nil
	t.wuwaMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("timed out waiting for Wuwa Mod Fixer update check to stop")
	}
}

func (t *Tools) runWuwaAutomaticUpdateCheck(ctx context.Context) {
	installed, err := t.wuwaGetInstalledBinaryInfo(ctx)
	if err != nil || !installed.Exists {
		return
	}
	refresh, err := t.wuwaRefreshLatestRelease(ctx, true)
	if err != nil || refresh.LatestRelease == nil || installed.Version == nil || compareToolVersions(refresh.LatestRelease.Version, *installed.Version) <= 0 {
		if err != nil && !errors.Is(err, context.Canceled) {
			t.logError(err, "WuwaModFixer:autoUpdate")
		}
		return
	}
	oldVersion, newVersion := *installed.Version, refresh.LatestRelease.Version
	if _, err := t.WuwaFixerInstallOrUpdate(ctx); err != nil {
		t.logError(err, "WuwaModFixer:autoUpdate")
		return
	}
	t.notifyWuwaAutomaticUpdate(ctx, oldVersion, newVersion)
}

func (t *Tools) notifyWuwaAutomaticUpdate(ctx context.Context, oldVersion, newVersion string) {
	if provider, ok := t.settings.(interface {
		Get(context.Context, string) (any, error)
	}); ok {
		value, getErr := provider.Get(ctx, "tools.wuwaFixerUpdateNotification")
		if getErr != nil || value == false {
			return
		}
	}
	if t.emit != nil {
		t.emit(
			"fn:toast",
			"Wuwa Mod Fixer updated",
			map[string]any{"description": fmt.Sprintf("v%s → v%s", oldVersion, newVersion)},
		)
	}
	if t.notify != nil {
		if err := t.notify("Wuwa Mod Fixer updated", fmt.Sprintf("v%s → v%s", oldVersion, newVersion)); err != nil {
			t.logError(err, "WuwaModFixer:autoUpdateNotification")
		}
	}
}

func (t *Tools) wuwaRefreshLatestRelease(ctx context.Context, force bool) (wuwaRefreshResult, error) {
	cached, err := t.wuwaGetCachedLatestRelease(ctx)
	if err != nil {
		return wuwaRefreshResult{}, err
	}
	lastCheck, err := t.getAppState(ctx, wuwaLastCheckKey)
	if err != nil {
		return wuwaRefreshResult{}, err
	}
	rate, err := t.wuwaGetRateState(ctx)
	if err != nil {
		return wuwaRefreshResult{}, err
	}
	if !force && cached != nil && lastCheck != nil && time.Since(parseRFC3339(*lastCheck)) < wuwaCheckCooldown {
		next := mustRFC3339(parseRFC3339(*lastCheck).Add(wuwaCheckCooldown))
		return wuwaRefreshResult{LatestRelease: cached, RateState: rate, RateLimited: wuwaRateLimited(rate), NextCheckAt: &next}, nil
	}
	if rate == nil {
		rate = t.wuwaRefreshRateState(ctx)
	}
	if wuwaRateLimited(rate) {
		next := time.Unix(rate.Reset, 0).UTC().Format(time.RFC3339Nano)
		return wuwaRefreshResult{LatestRelease: cached, RateState: rate, RateLimited: true, NextCheckAt: &next}, nil
	}
	header := make(http.Header)
	header.Set("Accept", "application/vnd.github+json")
	body, responseHeader, err := t.wuwaFetchBytes(ctx, wuwaReleasesLatestURL, header, 8<<20)
	if responseHeader != nil {
		rate, _ = t.wuwaCaptureRate(ctx, responseHeader)
	}
	if err != nil {
		return wuwaRefreshResult{}, fmt.Errorf("Failed to fetch Wuwa Mod Fixer release: %w", err) //nolint:staticcheck // Electron contract text.
	}
	var payload wuwaReleaseResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return wuwaRefreshResult{}, fmt.Errorf("decode Wuwa Mod Fixer release: %w", err)
	}
	latest, err := parseWuwaLatestRelease(payload)
	if err != nil {
		return wuwaRefreshResult{}, err
	}
	now := time.Now().UTC()
	latest.CheckedAt = now.Format(time.RFC3339Nano)
	raw, _ := json.Marshal(latest)
	if err := t.setAppState(ctx, wuwaLastCheckKey, now.Format(time.RFC3339Nano)); err != nil {
		return wuwaRefreshResult{}, err
	}
	if err := t.setAppState(ctx, wuwaLatestReleaseKey, string(raw)); err != nil {
		return wuwaRefreshResult{}, err
	}
	next := now.Add(wuwaCheckCooldown).Format(time.RFC3339Nano)
	return wuwaRefreshResult{LatestRelease: &latest, RateState: rate, RateLimited: wuwaRateLimited(rate), CheckedRemotely: true, NextCheckAt: &next}, nil
}

func (t *Tools) wuwaLatestReleaseForInstall(ctx context.Context) (*wuwaLatestReleaseCache, error) {
	refresh, err := t.wuwaRefreshLatestRelease(ctx, true)
	if err == nil && refresh.LatestRelease != nil {
		return refresh.LatestRelease, nil
	}
	cached, cacheErr := t.wuwaGetCachedLatestRelease(ctx)
	if cacheErr != nil {
		return nil, cacheErr
	}
	if cached != nil {
		return cached, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, contractError("Unable to fetch the latest Wuwa Mod Fixer release")
}

func (t *Tools) wuwaEnsureLatestConfig(ctx context.Context) (string, error) {
	toolDir, err := t.wuwaToolDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(toolDir, 0o700); err != nil {
		return "", err
	}
	body, _, err := t.wuwaFetchBytes(ctx, wuwaConfigURL, nil, 8<<20)
	if err != nil {
		return "", fmt.Errorf("Failed to download Wuwa Mod Fixer config: %w", err) //nolint:staticcheck // Electron contract text.
	}
	configPath := filepath.Join(toolDir, "config.json")
	tempPath := configPath + ".download"
	defer func() { _ = os.Remove(tempPath) }()
	if err := os.WriteFile(tempPath, body, 0o600); err != nil {
		return "", err
	}
	if err := replaceAtomic(tempPath, configPath); err != nil {
		return "", err
	}
	return configPath, nil
}

func (t *Tools) wuwaGetInstalledBinaryInfo(ctx context.Context) (wuwaInstalledInfo, error) {
	pathValue, err := t.getAppState(ctx, wuwaBinaryPathKey)
	if err != nil {
		return wuwaInstalledInfo{}, err
	}
	version, err := t.getAppState(ctx, wuwaInstalledVersionKey)
	if err != nil {
		return wuwaInstalledInfo{}, err
	}
	if pathValue != nil {
		if _, statErr := os.Stat(*pathValue); statErr == nil {
			if version == nil {
				version = extractWuwaVersion(filepath.Base(*pathValue))
			}
			return wuwaInstalledInfo{Exists: true, BinaryPath: pathValue, Version: version}, nil
		}
	}
	toolDir, err := t.wuwaToolDir()
	if err != nil {
		return wuwaInstalledInfo{}, err
	}
	dir, err := os.Open(toolDir)
	if errors.Is(err, os.ErrNotExist) {
		return wuwaInstalledInfo{}, nil
	}
	if err != nil {
		return wuwaInstalledInfo{}, err
	}
	defer func() { _ = dir.Close() }()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return wuwaInstalledInfo{}, err
	}
	var match string
	for _, name := range names {
		if wuwaBinaryRE.MatchString(name) {
			match = name
			break
		}
	}
	if match == "" {
		return wuwaInstalledInfo{}, nil
	}
	resolved := filepath.Join(toolDir, match)
	version = extractWuwaVersion(match)
	if err := t.setAppState(ctx, wuwaBinaryPathKey, resolved); err != nil {
		return wuwaInstalledInfo{}, err
	}
	if version != nil {
		if err := t.setAppState(ctx, wuwaInstalledVersionKey, *version); err != nil {
			return wuwaInstalledInfo{}, err
		}
	}
	return wuwaInstalledInfo{Exists: true, BinaryPath: &resolved, Version: version}, nil
}

func (t *Tools) wuwaCleanupOldBinaries(current string) error {
	toolDir := filepath.Dir(current)
	entries, err := os.ReadDir(toolDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(toolDir, entry.Name())
		if filepath.Clean(path) != filepath.Clean(current) && wuwaBinaryRE.MatchString(entry.Name()) {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Tools) wuwaRequireModPath(ctx context.Context, modPath string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	games, err := client.GamePaths.List(ctx)
	if err != nil {
		return err
	}
	resolvedTarget, err := filepath.Abs(modPath)
	if err != nil {
		return contractError("Path is outside the managed mod folder")
	}
	_, statErr := os.Stat(modPath)
	if statErr == nil {
		resolvedTarget, err = filepath.EvalSymlinks(modPath)
		if err != nil {
			return contractError("Path is outside the managed mod folder")
		}
	}
	for _, game := range games {
		logicalRoot, resolveErr := filepath.Abs(game.ModFolderPath)
		if statErr != nil && resolveErr == nil && sameOrChildPath(logicalRoot, resolvedTarget) {
			return contractError("Destination path does not exist")
		}
		resolvedRoot, resolveErr := filepath.EvalSymlinks(game.ModFolderPath)
		if resolveErr == nil && sameOrChildPath(resolvedRoot, resolvedTarget) {
			return nil
		}
	}
	return contractError("Path is outside the managed mod folder")
}

func (t *Tools) wuwaToolDir() (string, error) {
	return t.appDataPath(filepath.Join(appdata.ToolsDir, wuwaFixerDirName))
}

func (t *Tools) wuwaSupportedImporter(importer *string) bool {
	return importer == nil || strings.EqualFold(*importer, "WWMI")
}

func (t *Tools) wuwaGetCachedLatestRelease(ctx context.Context) (*wuwaLatestReleaseCache, error) {
	raw, err := t.getAppState(ctx, wuwaLatestReleaseKey)
	if err != nil || raw == nil {
		return nil, err
	}
	var release wuwaLatestReleaseCache
	_ = json.Unmarshal([]byte(*raw), &release)
	if release.Version == "" || release.Asset.Name == "" || release.Asset.BrowserDownloadURL == "" {
		return nil, nil
	}
	return &release, nil
}

func (t *Tools) wuwaGetRateState(ctx context.Context) (*GitHubRateState, error) {
	raw, err := t.getAppState(ctx, githubCoreRateKey)
	if err != nil || raw == nil {
		return nil, err
	}
	var state GitHubRateState
	_ = json.Unmarshal([]byte(*raw), &state)
	if state.Limit == 0 && state.Remaining == 0 && state.Reset == 0 && state.Used == 0 && state.Resource == "" {
		return nil, nil
	}
	return &state, nil
}

func (t *Tools) wuwaRefreshRateState(ctx context.Context) *GitHubRateState {
	body, responseHeader, err := t.wuwaFetchBytes(ctx, "https://api.github.com/rate_limit", http.Header{"Accept": []string{"application/vnd.github+json"}}, 2<<20)
	if responseHeader != nil {
		if state, captureErr := t.wuwaCaptureRate(ctx, responseHeader); captureErr == nil && state != nil {
			return state
		}
	}
	if err != nil {
		return nil
	}
	var payload struct {
		Rate *GitHubRateState `json:"rate"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Rate == nil {
		return nil
	}
	payload.Rate.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if payload.Rate.Resource == "" {
		payload.Rate.Resource = "core"
	}
	raw, _ := json.Marshal(payload.Rate)
	_ = t.setAppState(ctx, githubCoreRateKey, string(raw))
	return payload.Rate
}

func (t *Tools) wuwaCaptureRate(ctx context.Context, header http.Header) (*GitHubRateState, error) {
	parse := func(name string) (int64, bool) {
		value := header.Get(name)
		if value == "" {
			return 0, false
		}
		number, err := strconv.ParseInt(value, 10, 64)
		return number, err == nil
	}
	limit, okLimit := parse("X-RateLimit-Limit")
	remaining, okRemaining := parse("X-RateLimit-Remaining")
	reset, okReset := parse("X-RateLimit-Reset")
	used, okUsed := parse("X-RateLimit-Used")
	if !okLimit || !okRemaining || !okReset || !okUsed {
		return nil, nil
	}
	resource := header.Get("X-RateLimit-Resource")
	if resource == "" {
		resource = "core"
	}
	state := &GitHubRateState{Limit: limit, Remaining: remaining, Reset: reset, Used: used, Resource: resource, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	raw, _ := json.Marshal(state)
	if err := t.setAppState(ctx, githubCoreRateKey, string(raw)); err != nil {
		return nil, err
	}
	return state, nil
}

func (t *Tools) wuwaFetchBytes(ctx context.Context, rawURL string, header http.Header, maxSize int64) ([]byte, http.Header, error) {
	if t.http == nil {
		return nil, nil, errors.New("tools HTTP client is not configured")
	}
	response, err := t.http.Fetch(ctx, rawURL, infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	responseHeader := response.Header.Clone()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, responseHeader, fmt.Errorf("HTTP %d", response.StatusCode) //nolint:staticcheck // Electron contract text.
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return nil, responseHeader, err
	}
	if int64(len(data)) > maxSize {
		return nil, responseHeader, fmt.Errorf("response exceeds %d bytes", maxSize)
	}
	return data, responseHeader, nil
}

func parseWuwaLatestRelease(payload wuwaReleaseResponse) (wuwaLatestReleaseCache, error) {
	if payload.TagName == "" {
		return wuwaLatestReleaseCache{}, contractError("Latest Wuwa Mod Fixer release is missing tag_name")
	}
	for _, asset := range payload.Assets {
		if wuwaBinaryRE.MatchString(asset.Name) && asset.BrowserDownloadURL != "" {
			return wuwaLatestReleaseCache{Version: payload.TagName, Asset: wuwaLatestAsset{Name: asset.Name, BrowserDownloadURL: asset.BrowserDownloadURL, Digest: asset.Digest}}, nil
		}
	}
	return wuwaLatestReleaseCache{}, contractError("Latest Wuwa Mod Fixer release is missing a Windows executable asset")
}

func buildWuwaCLIArgs(modPath, configPath string, options WuwaFixerOptions) ([]string, error) {
	if options.DerivedHashes && options.StableTexture {
		return nil, contractError("Derived hashes and stable texture cannot be enabled together")
	}
	if options.AeroFix == "" {
		options.AeroFix = "none"
	}
	if options.AeroFix != "none" && options.AeroFix != "1" && options.AeroFix != "2" {
		return nil, errors.New("invalid aero fix value")
	}
	args := []string{"--cli", "--path", modPath, "--config", configPath}
	if options.DerivedHashes {
		args = append(args, "--derived-hashes")
	}
	if options.StableTexture {
		args = append(args, "--stable-texture")
	}
	if options.AemeathMech {
		args = append(args, "--aemeath-mech")
	}
	if options.Rendering33 {
		args = append(args, "--rendering-33")
	}
	if options.AeroFix != "none" {
		args = append(args, "--aero-fix", options.AeroFix)
	}
	return args, nil
}

func listWuwaBackupFiles(root string) ([]string, error) {
	paths, err := doublestar.FilepathGlob(
		filepath.Join(root, "**", "*.BAK"),
		doublestar.WithCaseInsensitive(),
		doublestar.WithFilesOnly(),
	)
	sort.Strings(paths)
	return paths, err
}

func collectWuwaBackupGroups(root string) ([]WuwaBackupGroup, error) {
	paths, err := listWuwaBackupFiles(root)
	if err != nil {
		return nil, err
	}
	byGroup := make(map[string][]WuwaBackupFile)
	for _, bakPath := range paths {
		match := wuwaBackupRE.FindStringSubmatch(filepath.Base(bakPath))
		if len(match) != 3 {
			continue
		}
		groupKey := match[2][:16]
		byGroup[groupKey] = append(byGroup[groupKey], WuwaBackupFile{CurrentPath: bakPath, OriginalPath: filepath.Join(filepath.Dir(bakPath), match[1]), Timestamp: match[2], GroupKey: groupKey})
	}
	groups := make([]WuwaBackupGroup, 0, len(byGroup))
	for key, files := range byGroup {
		groups = append(groups, WuwaBackupGroup{GroupKey: key, Files: files})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].GroupKey > groups[j].GroupKey })
	return groups, nil
}

func copyRegularFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("backup is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".wuwa-rollback-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err = io.Copy(temp, input); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceAtomic(tempPath, target)
}

func verifyWuwaDigest(data []byte, digest *string) error {
	if digest == nil || *digest == "" {
		return nil
	}
	algorithm, expected, ok := strings.Cut(*digest, ":")
	if !ok || !strings.EqualFold(algorithm, "sha256") || expected == "" {
		return contractError("Unsupported Wuwa Mod Fixer digest format")
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), expected) {
		return contractError("Wuwa Mod Fixer download digest mismatch")
	}
	return nil
}

func extractWuwaVersion(name string) *string {
	match := wuwaBinaryVersionRE.FindStringSubmatch(name)
	if len(match) != 2 {
		return nil
	}
	version := match[1]
	return &version
}

func compareToolVersions(left, right string) int {
	parse := func(value string) []int {
		parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(value, "v"), "V"), ".")
		out := make([]int, len(parts))
		for i, part := range parts {
			out[i], _ = strconv.Atoi(part)
		}
		return out
	}
	a, b := parse(left), parse(right)
	for i := range max(len(a), len(b)) {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

func sameOrChildPath(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func wuwaRateLimited(state *GitHubRateState) bool {
	return state != nil && state.Remaining <= 0 && time.Unix(state.Reset, 0).After(time.Now())
}

func parseRFC3339(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func mustRFC3339(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
