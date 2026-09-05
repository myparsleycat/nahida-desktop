package tools

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	targetD3D11DLL           = "d3d11.dll"
	d3dBuildStatePrefix      = "mod_tools:d3d_build:"
	d3dBuildTempDirName      = "nahida-tools-d3d-build"
	diversifierBackupPre     = targetD3D11DLL + ".pepd-backup-"
	fixer4001Event           = "tools:4001FixerProgress"
	fixer4001VSDevCmdPathKey = "mod_tools:4001-fixer:vs-devcmd-path"
	vsDevCmdFileName         = "vcvars64.bat"
)

var (
	d3dBuildIDRE     = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	providerRE       = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	backupHashRE     = regexp.MustCompile(`^d3d11\.dll\.pepd-backup-([a-f0-9]{7})-\d+\.bak$`)
	vsDevCmdEditions = []string{"Community", "Professional", "Enterprise", "Insiders", "BuildTools"}
	vsDevCmdVersions = []string{"2025", "2022", "18", "17"}
)

type releaseCacheEntry struct {
	versions []string
	fetched  time.Time
}

type releaseFetchCall struct {
	done chan struct{}
	err  error
}

type Fixer4001State struct {
	IsBuilding   bool    `json:"isBuilding"`
	ActiveTask   *string `json:"activeTask"`
	Progress     string  `json:"progress"`
	ErrorMessage string  `json:"errorMessage"`
}

type Fixer4001ProgressEvent struct {
	Task         *string `json:"task"`
	Code         string  `json:"code"`
	ErrorMessage string  `json:"errorMessage,omitempty"`
}

type Fixer4001BuildInput struct {
	Provider     string  `json:"provider"`
	Version      string  `json:"version"`
	ImporterKey  string  `json:"importerKey"`
	ImporterPath *string `json:"importerPath,omitempty"`
}

type Fixer4001ImporterInput struct {
	ImporterKey  string  `json:"importerKey"`
	ImporterPath *string `json:"importerPath,omitempty"`
}

type Fixer4001PathInput struct {
	ImporterPath *string `json:"importerPath,omitempty"`
}

type Fixer4001Result struct {
	Success      bool    `json:"success"`
	ErrorMessage *string `json:"errorMessage,omitempty"`
	BackupPath   *string `json:"backupPath,omitempty"`
}

type Fixer4001BuildToolsResult struct {
	Found bool   `json:"found"`
	Path  string `json:"path"`
}

type DiversificationState struct {
	HasBackup  bool    `json:"hasBackup"`
	BackupPath *string `json:"backupPath"`
}

type PEDiversificationReport struct {
	DiscoveredRegions int                  `json:"discovered_regions"`
	ModifiedRegions   int                  `json:"modified_regions"`
	InputSHA256       string               `json:"input_sha256"`
	OutputSHA256      *string              `json:"output_sha256"`
	Patches           []PEDiversifierPatch `json:"patches"`
}

type PEDiversifierPatch struct {
	CandidateID int `json:"candidate_id"`
}

type ImporterWriteAccess struct {
	Writable          bool                   `json:"writable"`
	Locked            bool                   `json:"locked"`
	RequiresElevation bool                   `json:"requiresElevation"`
	Processes         []platform.ProcessInfo `json:"processes"`
}

func (t *Tools) FourThousandOneFixerGetState() Fixer4001State {
	t.fixerMu.Lock()
	defer t.fixerMu.Unlock()
	return Fixer4001State{
		IsBuilding: t.fixerTask != nil, ActiveTask: cloneStringPointer(t.fixerTask),
		Progress: t.fixerProgress, ErrorMessage: t.fixerError,
	}
}

func (t *Tools) FourThousandOneFixerGetProviderReleases(ctx context.Context, provider string) ([]string, error) {
	return t.get4001ProviderReleases(ctx, provider, false)
}

func (t *Tools) get4001ProviderReleases(ctx context.Context, provider string, refresh bool) ([]string, error) {
	provider = strings.TrimSpace(provider)
	if !providerRE.MatchString(provider) {
		return nil, errors.New("invalid GitHub provider")
	}
	t.fixerMu.Lock()
	entry, found := t.releaseCache[provider]
	if found && (!refresh || time.Since(entry.fetched) < time.Minute) {
		versions := slices.Clone(entry.versions)
		t.fixerMu.Unlock()
		return versions, nil
	}
	if call := t.releaseCalls[provider]; call != nil {
		t.fixerMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
		}
		t.fixerMu.Lock()
		entry, found = t.releaseCache[provider]
		versions := slices.Clone(entry.versions)
		t.fixerMu.Unlock()
		if !found {
			return nil, call.err
		}
		return versions, call.err
	}
	call := &releaseFetchCall{done: make(chan struct{})}
	t.releaseCalls[provider] = call
	t.fixerMu.Unlock()
	finish := func(versions []string, fetchErr error) ([]string, error) {
		t.fixerMu.Lock()
		if fetchErr == nil {
			t.releaseCache[provider] = releaseCacheEntry{versions: slices.Clone(versions), fetched: time.Now()}
		}
		call.err = fetchErr
		if t.releaseCalls[provider] == call {
			delete(t.releaseCalls, provider)
		}
		close(call.done)
		cached := slices.Clone(t.releaseCache[provider].versions)
		t.fixerMu.Unlock()
		if fetchErr != nil {
			return nil, fetchErr
		}
		return cached, nil
	}
	if t.http == nil {
		return finish(nil, errors.New("4001 fixer HTTP client is not configured"))
	}
	rawURL := fmt.Sprintf("https://api.github.com/repos/%s/XXMI-Libs-Package/releases", provider)
	header := make(http.Header)
	header.Set("Accept", "application/vnd.github+json")
	header.Set("X-GitHub-Api-Version", "2026-03-10")
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	response, err := t.http.Fetch(ctx, rawURL, infra.FetchOptions{Method: http.MethodGet, Header: header, DisableHTTPErrors: true})
	if err != nil {
		return finish(nil, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return finish(nil, fmt.Errorf("failed to fetch XXMI libs releases for %s: %s", provider, response.Status))
	}
	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&releases); err != nil {
		return finish(nil, err)
	}
	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		name := strings.TrimSpace(release.TagName)
		if name != "" && !strings.EqualFold(name, "main") && !strings.EqualFold(name, "master") {
			versions = append(versions, name)
		}
	}
	return finish(versions, nil)
}

func (t *Tools) FourThousandOneFixerUpdateReleases(ctx context.Context) error {
	_, err := t.get4001ProviderReleases(ctx, "SpectrumQT", true)
	return err
}

// Start4001ReleasePrefetch mirrors the Electron service constructor's
// best-effort release warmup without delaying application startup.
//
//wails:ignore
func (t *Tools) Start4001ReleasePrefetch() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := t.FourThousandOneFixerUpdateReleases(ctx); err != nil && t.log != nil {
			t.log.Warn("Automatic XXMI libs release prefetch failed: "+err.Error(), "4001Fixer:updateReleases")
		}
	}()
}

func (t *Tools) FourThousandOneFixerGetBuildToolsPath(ctx context.Context) (string, error) {
	value, err := t.getAppState(ctx, fixer4001VSDevCmdPathKey)
	if err != nil || value == nil {
		return "", err
	}
	return strings.TrimSpace(*value), nil
}

func (t *Tools) FourThousandOneFixerSetBuildToolsPath(ctx context.Context, path string) (Fixer4001BuildToolsResult, error) {
	resolved := resolveVSDevCmd(path)
	if resolved == "" {
		return Fixer4001BuildToolsResult{}, nil
	}
	if err := t.setAppState(ctx, fixer4001VSDevCmdPathKey, resolved); err != nil {
		return Fixer4001BuildToolsResult{}, err
	}
	return Fixer4001BuildToolsResult{Found: true, Path: resolved}, nil
}

func (t *Tools) FourThousandOneFixerClearBuildToolsPath(ctx context.Context) error {
	return t.deleteAppState(ctx, fixer4001VSDevCmdPathKey)
}

func (t *Tools) locate4001VSDevCmd(ctx context.Context) (string, error) {
	stored, err := t.FourThousandOneFixerGetBuildToolsPath(ctx)
	if err != nil {
		return "", err
	}
	if stored != "" {
		if isLocalFilesystemPath(stored) && regularFile(stored) {
			return stored, nil
		}
		return "", nil
	}
	return findVSDevCmd(), nil
}

func (t *Tools) FourThousandOneFixerGetDiversificationState(input Fixer4001PathInput) (DiversificationState, error) {
	path, ok := existingImporterPath(input.ImporterPath)
	if !ok {
		return DiversificationState{}, nil
	}
	backup, err := t.findDiversifierBackup(path)
	if err != nil {
		return DiversificationState{}, err
	}
	return DiversificationState{HasBackup: backup != nil, BackupPath: backup}, nil
}

func (t *Tools) FourThousandOneFixerCheckImporterWriteAccess(input Fixer4001PathInput) ImporterWriteAccess {
	path, ok := existingImporterPath(input.ImporterPath)
	if !ok {
		return ImporterWriteAccess{Processes: []platform.ProcessInfo{}}
	}
	access := t.fs.GetFileWriteAccess(filepath.Join(path, targetD3D11DLL), path)
	return ImporterWriteAccess{
		Writable: access.Writable, Locked: access.Locked,
		RequiresElevation: !access.Writable && !access.Locked, Processes: access.Processes,
	}
}

func (t *Tools) FourThousandOneFixerBuildDll(ctx context.Context, input Fixer4001BuildInput) (result Fixer4001Result) {
	if !t.begin4001Task("build-dll") {
		return result
	}
	defer t.end4001Task()
	t.update4001Progress("XXMI_INIT", "")
	importerPath, ok := existingImporterPath(input.ImporterPath)
	if !ok {
		t.update4001Progress("XXMI_ERR_GIMI_NOT_FOUND", "")
		return result
	}
	if err := t.ensureXXMILauncherClosed(ctx); err != nil {
		return t.failed4001("XXMI_ERR_LAUNCHER_CLOSE_FAILED", err)
	}
	finalDestination := filepath.Join(importerPath, targetD3D11DLL)
	access := t.fs.GetFileWriteAccess(finalDestination, importerPath)
	if access.Locked {
		return t.failed4001("XXMI_ERR_DLL_IN_USE", errors.New(t.fs.FormatProcessList(access.Processes)))
	}
	useElevated := !access.Writable

	t.update4001Progress("XXMI_FIND_VS", "")
	vcvarsPath, err := t.locate4001VSDevCmd(ctx)
	if err != nil {
		return t.failed4001("XXMI_ERR_BUILD_FAILED", err)
	}
	if vcvarsPath == "" {
		t.update4001Progress("XXMI_ERR_VS_NOT_FOUND", "")
		return result
	}
	if !providerRE.MatchString(strings.TrimSpace(input.Provider)) || strings.TrimSpace(input.Version) == "" {
		return t.failed4001("XXMI_ERR_BUILD_FAILED", errors.New("invalid provider or version"))
	}
	buildID, err := newToolsID()
	if err != nil {
		return t.failed4001("XXMI_ERR_BUILD_FAILED", err)
	}
	tempDir := filepath.Join(os.TempDir(), d3dBuildTempDirName, buildID)
	defer func() {
		t.reportCleanup(os.RemoveAll(tempDir), "FourThousandOneFixerBuildDll")
		if client, clientErr := t.requireClient(); clientErr == nil {
			_ = client.AppState.Delete(context.Background(), d3dBuildStatePrefix+buildID)
		}
	}()
	if err := t.trackD3DBuild(ctx, buildID, tempDir); err != nil {
		return t.failed4001("XXMI_ERR_BUILD_FAILED", err)
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return t.failed4001("XXMI_ERR_BUILD_FAILED", err)
	}
	projectPath, err := t.prepareD3DSource(ctx, tempDir, input.Provider, input.Version)
	if err != nil {
		return t.failed4001("XXMI_ERR_BUILD_FAILED", err)
	}
	t.update4001Progress("XXMI_BUILDING", "")
	if err := executeD3DBuild(ctx, vcvarsPath, projectPath); err != nil {
		return t.failed4001Build(err)
	}
	builtDLL := filepath.Join(projectPath, "x64", "Release", targetD3D11DLL)
	if _, err := os.Stat(builtDLL); err != nil {
		t.update4001Progress("XXMI_ERR_DLL_NOT_FOUND", "")
		return result
	}
	if err := installFileCopies([]fileCopy{{Source: builtDLL, Target: finalDestination}}, useElevated); err != nil {
		return t.failed4001Install(err, finalDestination, "XXMI_ERR_BUILD_FAILED")
	}
	t.enableUnsafeMode(ctx, input.ImporterKey)
	t.removeDiversifierBackups(importerPath, useElevated)
	t.update4001Progress("XXMI_BUILD_SUCCESS", "")
	return Fixer4001Result{Success: true}
}

func (t *Tools) FourThousandOneFixerDiversifyDllPadding(ctx context.Context, input Fixer4001ImporterInput) (result Fixer4001Result) {
	if !t.begin4001Task("diversify-dll") {
		return result
	}
	defer t.end4001Task()
	t.update4001Progress("XXMI_OBFUSCATE_INIT", "")
	importerPath, ok := existingImporterPath(input.ImporterPath)
	if !ok {
		t.update4001Progress("XXMI_ERR_GIMI_NOT_FOUND", "")
		return result
	}
	backup, err := t.findDiversifierBackup(importerPath)
	if err != nil {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", err)
	}
	if backup != nil {
		t.update4001Progress("XXMI_OBFUSCATE_BACKUP_EXISTS", "")
		return Fixer4001Result{BackupPath: backup}
	}
	target := filepath.Join(importerPath, targetD3D11DLL)
	if !regularFile(target) {
		t.update4001Progress("XXMI_ERR_DLL_NOT_FOUND", "")
		return result
	}
	if err := t.ensureXXMILauncherClosed(ctx); err != nil {
		return t.failed4001("XXMI_ERR_LAUNCHER_CLOSE_FAILED", err)
	}
	access := t.fs.GetFileWriteAccess(target, importerPath)
	if access.Locked {
		return t.failed4001("XXMI_ERR_DLL_IN_USE", errors.New(t.fs.FormatProcessList(access.Processes)))
	}
	useElevated := !access.Writable
	tempRoot := filepath.Join(os.TempDir(), d3dBuildTempDirName)
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", err)
	}
	tempFile, err := os.CreateTemp(tempRoot, targetD3D11DLL+".*.nahida-diversified.tmp")
	if err != nil {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", err)
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		t.reportCleanup(os.Remove(tempPath), "FourThousandOneFixerDiversifyDllPadding")
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", err)
	}
	t.reportCleanup(os.Remove(tempPath), "FourThousandOneFixerDiversifyDllPadding")
	defer func() { t.reportCleanup(os.Remove(tempPath), "FourThousandOneFixerDiversifyDllPadding") }()
	t.update4001Progress("XXMI_OBFUSCATING", "")
	report, err := t.runPEDiversifier(ctx, target, tempPath)
	if err != nil {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", err)
	}
	currentHash, err := hashFile(target)
	if err != nil {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", err)
	}
	diversifiedHash, err := hashFile(tempPath)
	if err != nil && report.DiscoveredRegions > 0 {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", err)
	}
	if report.InputSHA256 != "" && !strings.EqualFold(report.InputSHA256, currentHash) {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", errors.New("PE padding diversifier input hash mismatch"))
	}
	if report.OutputSHA256 != nil && diversifiedHash != "" && !strings.EqualFold(*report.OutputSHA256, diversifiedHash) {
		return t.failed4001("XXMI_ERR_OBFUSCATE_FAILED", errors.New("PE padding diversifier output hash mismatch"))
	}
	if report.DiscoveredRegions == 0 {
		t.update4001Progress("XXMI_ERR_OBFUSCATE_NO_CANDIDATES", "")
		return result
	}
	if len(report.Patches) == 0 {
		message := fmt.Sprintf("Found %d JMP-rel8 candidate(s), but none were safe to patch.", report.DiscoveredRegions)
		return t.failed4001("XXMI_ERR_OBFUSCATE_NO_CANDIDATES", errors.New(message))
	}
	hashAfter := ""
	if report.OutputSHA256 != nil {
		hashAfter = *report.OutputSHA256
	} else {
		hashAfter = diversifiedHash
	}
	if report.ModifiedRegions == 0 || strings.EqualFold(report.InputSHA256, hashAfter) {
		t.update4001Progress("XXMI_OBFUSCATE_ALREADY_APPLIED", "")
		return result
	}
	hashPrefix := diversifiedHash[:min(7, len(diversifiedHash))]
	backupPath := filepath.Join(importerPath, fmt.Sprintf("%s%s-%d.bak", diversifierBackupPre, hashPrefix, time.Now().Unix()))
	if err := installFileCopies([]fileCopy{{Source: target, Target: backupPath}, {Source: tempPath, Target: target}}, useElevated); err != nil {
		_ = removeFilePaths([]string{backupPath}, useElevated)
		return t.failed4001Install(err, target, "XXMI_ERR_OBFUSCATE_FAILED")
	}
	t.enableUnsafeMode(ctx, input.ImporterKey)
	t.update4001Progress("XXMI_OBFUSCATE_SUCCESS", "")
	if t.log != nil {
		t.log.Info(fmt.Sprintf("Successfully diversified padding in %s; backup=%s; candidates=%d; mutations=%d; hashBefore=%s; hashAfter=%s", target, backupPath, report.DiscoveredRegions, report.ModifiedRegions, report.InputSHA256, hashAfter), "4001Fixer:diversifyD3D11DllPadding")
	}
	return Fixer4001Result{Success: true, BackupPath: stringPointer(backupPath)}
}

func (t *Tools) FourThousandOneFixerRestoreDiversifiedDll(ctx context.Context, input Fixer4001PathInput) (result Fixer4001Result) {
	if !t.begin4001Task("restore-dll") {
		return result
	}
	defer t.end4001Task()
	t.update4001Progress("XXMI_RESTORE_INIT", "")
	importerPath, ok := existingImporterPath(input.ImporterPath)
	if !ok {
		t.update4001Progress("XXMI_ERR_GIMI_NOT_FOUND", "")
		return result
	}
	backup, err := t.findDiversifierBackup(importerPath)
	if err != nil {
		return t.failed4001("XXMI_ERR_RESTORE_FAILED", err)
	}
	if backup == nil {
		t.update4001Progress("XXMI_ERR_RESTORE_BACKUP_NOT_FOUND", "")
		return result
	}
	if err := t.ensureXXMILauncherClosed(ctx); err != nil {
		return t.failed4001("XXMI_ERR_LAUNCHER_CLOSE_FAILED", err)
	}
	target := filepath.Join(importerPath, targetD3D11DLL)
	access := t.fs.GetFileWriteAccess(target, importerPath)
	if access.Locked {
		return t.failed4001("XXMI_ERR_DLL_IN_USE", errors.New(t.fs.FormatProcessList(access.Processes)))
	}
	t.update4001Progress("XXMI_RESTORING", "")
	if err := installFileCopies([]fileCopy{{Source: *backup, Target: target}}, !access.Writable); err != nil {
		return t.failed4001Install(err, target, "XXMI_ERR_RESTORE_FAILED")
	}
	t.removeDiversifierBackups(importerPath, !access.Writable)
	t.update4001Progress("XXMI_RESTORE_SUCCESS", "")
	return Fixer4001Result{Success: true, BackupPath: backup}
}

func (t *Tools) begin4001Task(task string) bool {
	t.fixerMu.Lock()
	defer t.fixerMu.Unlock()
	if t.fixerTask != nil {
		return false
	}
	t.fixerTask = stringPointer(task)
	t.fixerError = ""
	return true
}

func (t *Tools) end4001Task() {
	t.fixerMu.Lock()
	t.fixerTask = nil
	t.fixerMu.Unlock()
}

func (t *Tools) update4001Progress(code, errorMessage string) {
	t.fixerMu.Lock()
	t.fixerProgress = code
	t.fixerError = errorMessage
	task := cloneStringPointer(t.fixerTask)
	t.fixerMu.Unlock()
	t.emitEvent(fixer4001Event, Fixer4001ProgressEvent{Task: task, Code: code, ErrorMessage: errorMessage})
}

func (t *Tools) failed4001(code string, err error) Fixer4001Result {
	message := ""
	if err != nil {
		message = err.Error()
		t.logError(err, "4001Fixer")
	}
	t.update4001Progress(code, message)
	return Fixer4001Result{ErrorMessage: stringPointer(message)}
}

func (t *Tools) failed4001Install(err error, target, fallbackCode string) Fixer4001Result {
	lock := t.fs.IsLockedPathError(err, target)
	if lock.IsLocked {
		return t.failed4001("XXMI_ERR_DLL_IN_USE", errors.New(t.fs.FormatProcessList(lock.Processes)))
	}
	var elevatedErr elevatedFileCopyError
	if errors.As(err, &elevatedErr) {
		return t.failed4001("XXMI_ERR_ELEVATION_FAILED", err)
	}
	return t.failed4001(fallbackCode, err)
}

func (t *Tools) failed4001Build(err error) Fixer4001Result {
	t.logError(err, "4001Fixer")
	message := extractBuildErrorMessage(err)
	t.update4001Progress("XXMI_ERR_BUILD_FAILED", message)
	return Fixer4001Result{ErrorMessage: stringPointer(message)}
}

func (t *Tools) ensureXXMILauncherClosed(ctx context.Context) error {
	if t.xxmi == nil {
		return nil
	}
	return t.xxmi.EnsureLauncherClosed(ctx)
}

func existingImporterPath(input *string) (string, bool) {
	if input == nil || strings.TrimSpace(*input) == "" {
		return "", false
	}
	path, err := filepath.Abs(strings.TrimSpace(*input))
	if err != nil {
		return "", false
	}
	info, err := os.Stat(path)
	return path, err == nil && info.IsDir()
}

func (t *Tools) prepareD3DSource(ctx context.Context, tempDir, provider, version string) (string, error) {
	if t.download == nil || t.archive == nil {
		return "", errors.New("4001 fixer download/archive dependencies are not configured")
	}
	t.update4001Progress("XXMI_DOWNLOAD_REPO", "")
	rawURL := fmt.Sprintf("https://github.com/%s/XXMI-Libs-Package/archive/refs/tags/%s.zip",
		provider, url.PathEscape(strings.TrimSpace(version)))
	zipPath := filepath.Join(tempDir, "repo.zip")
	header := make(http.Header)
	header.Set("User-Agent", "nahida-desktop")
	header.Set("Referer", fmt.Sprintf("https://github.com/%s/XXMI-Libs-Package", provider))
	if err := t.download.File(ctx, infra.DownloadRequest{URL: rawURL, Destination: zipPath, Header: header}); err != nil {
		return "", err
	}
	t.update4001Progress("XXMI_EXTRACT_REPO", "")
	extracted, err := t.archive.Extract(ctx, zipPath, tempDir, infra.ExtractOptions{}, nil)
	if err != nil {
		return "", err
	}
	return findStereovisionProject(extracted)
}

func findStereovisionProject(root string) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && path != root {
			rel, _ := filepath.Rel(root, path)
			if strings.Count(rel, string(filepath.Separator)) > 3 {
				return filepath.SkipDir
			}
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "StereovisionHacks.sln") {
			found = filepath.Dir(path)
			return errProjectFound
		}
		return nil
	})
	if errors.Is(err, errProjectFound) {
		err = nil
	}
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("StereovisionHacks.sln not found in release archive")
	}
	return found, nil
}

var errProjectFound = errors.New("project found")

func findVSDevCmd() string {
	for _, root := range defaultVSInstallRoots() {
		for _, version := range vsDevCmdVersions {
			for _, edition := range vsDevCmdEditions {
				candidate := filepath.Join(root, version, edition, "VC", "Auxiliary", "Build", vsDevCmdFileName)
				if regularFile(candidate) {
					return candidate
				}
			}
		}
	}
	return ""
}

func defaultVSInstallRoots() []string {
	return []string{
		filepath.Join(envOr("ProgramFiles", `C:\Program Files`), "Microsoft Visual Studio"),
		filepath.Join(envOr("ProgramFiles(x86)", `C:\Program Files (x86)`), "Microsoft Visual Studio"),
	}
}

func resolveVSDevCmd(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) && !isLocalFilesystemPath(path) {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	if !isLocalFilesystemPath(abs) {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil {
		return ""
	}
	if info.Mode().IsRegular() {
		if strings.EqualFold(info.Name(), vsDevCmdFileName) {
			return abs
		}
		return ""
	}
	if !info.IsDir() {
		return ""
	}
	for _, candidate := range vsDevCmdCandidates(abs) {
		if regularFile(candidate) {
			return candidate
		}
	}
	return ""
}

func isLocalFilesystemPath(path string) bool {
	if path == "" {
		return false
	}
	volume := filepath.VolumeName(path)
	if volume == "" {
		return filepath.IsAbs(path)
	}
	// Drive-letter volumes only (e.g. "C:"). Reject UNC and \\?\ / \\.\ namespaces.
	return len(volume) == 2 && volume[1] == ':'
}

func vsDevCmdCandidates(root string) []string {
	candidates := []string{
		filepath.Join(root, vsDevCmdFileName),
		filepath.Join(root, "VC", "Auxiliary", "Build", vsDevCmdFileName),
	}
	for _, edition := range vsDevCmdEditions {
		candidates = append(candidates, filepath.Join(root, edition, "VC", "Auxiliary", "Build", vsDevCmdFileName))
	}
	for _, version := range vsDevCmdVersions {
		for _, edition := range vsDevCmdEditions {
			candidates = append(candidates, filepath.Join(root, version, edition, "VC", "Auxiliary", "Build", vsDevCmdFileName))
		}
	}
	return candidates
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (t *Tools) trackD3DBuild(ctx context.Context, id, dir string) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]string{"id": id, "tempDir": dir})
	if err != nil {
		return err
	}
	return client.AppState.Upsert(ctx, d3dBuildStatePrefix+id, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
}

// CleanupStaleD3DBuilds removes only validated child directories of the fixed temp root.
//
//wails:ignore
func (t *Tools) CleanupStaleD3DBuilds(ctx context.Context) error {
	client, err := t.requireClient()
	if err != nil {
		return err
	}
	states, err := client.AppState.ListByPrefix(ctx, d3dBuildStatePrefix)
	if err != nil {
		return err
	}
	root := filepath.Join(os.TempDir(), d3dBuildTempDirName)
	for _, state := range states {
		id := strings.TrimPrefix(state.Key, d3dBuildStatePrefix)
		if d3dBuildIDRE.MatchString(id) {
			t.reportCleanup(os.RemoveAll(filepath.Join(root, id)), "CleanupStaleD3DBuilds")
		}
		if err := client.AppState.Delete(ctx, state.Key); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tools) findDiversifierBackup(importerPath string) (*string, error) {
	entries, err := os.ReadDir(importerPath)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), diversifierBackupPre) && strings.HasSuffix(entry.Name(), ".bak") {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) == 0 {
		return nil, nil
	}
	currentHash, hashErr := hashFile(filepath.Join(importerPath, targetD3D11DLL))
	hasCurrentHash := hashErr == nil
	for _, name := range names {
		candidate := filepath.Join(importerPath, name)
		match := backupHashRE.FindStringSubmatch(name)
		if match == nil {
			t.reportCleanup(os.Remove(candidate), "findDiversifierBackup")
			continue
		}
		if !hasCurrentHash || strings.HasPrefix(currentHash, match[1]) {
			return stringPointer(candidate), nil
		}
		t.reportCleanup(os.Remove(candidate), "findDiversifierBackup")
	}
	return nil, nil
}

func (t *Tools) removeDiversifierBackups(importerPath string, elevated bool) {
	entries, err := os.ReadDir(importerPath)
	if err != nil {
		return
	}
	var paths []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), diversifierBackupPre) && strings.HasSuffix(entry.Name(), ".bak") {
			paths = append(paths, filepath.Join(importerPath, entry.Name()))
		}
	}
	if err := removeFilePaths(paths, elevated); err != nil {
		t.logError(err, "4001Fixer:removeBackups")
	}
}

func hashFile(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = input.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (t *Tools) enableUnsafeMode(ctx context.Context, importerKey string) {
	if t.xxmi == nil || strings.TrimSpace(importerKey) == "" {
		return
	}
	xxmiPath, err := t.xxmi.GetXXMIPath(ctx)
	if err != nil || xxmiPath == nil {
		return
	}
	configPath := filepath.Join(*xxmiPath, "XXMI Launcher Config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.logError(err, "4001Fixer:enableUnsafeMode")
		return
	}
	importers, _ := config["Importers"].(map[string]any)
	importer, _ := importers[importerKey].(map[string]any)
	migoto, _ := importer["Migoto"].(map[string]any)
	unsafeMode, exists := migoto["unsafe_mode"].(bool)
	if !exists || unsafeMode {
		return
	}
	t.update4001Progress("XXMI_ENABLE_UNSAFE_MODE", "")
	signature, err := unsafeModeSignature(*xxmiPath)
	if err != nil {
		t.logError(err, "4001Fixer:enableUnsafeMode")
		return
	}
	migoto["unsafe_mode"] = true
	migoto["unsafe_mode_signature"] = signature
	encoded, err := json.MarshalIndent(config, "", "    ")
	if err == nil {
		err = os.WriteFile(configPath, append(encoded, '\n'), 0o600)
	}
	if err != nil {
		t.logError(err, "4001Fixer:enableUnsafeMode")
	}
}

func unsafeModeSignature(xxmiPath string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(xxmiPath, "Resources", "Security", "private_key.der"))
	if err != nil {
		return "", err
	}
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return "", err
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("XXMI private key is not RSA")
	}
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	username := current.Username
	if index := strings.LastIndexAny(username, `\\/`); index >= 0 {
		username = username[index+1:]
	}
	digest := sha256.Sum256([]byte(username))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}
