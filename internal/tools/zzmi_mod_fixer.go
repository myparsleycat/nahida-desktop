package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
	zzmiengine "nahida.live/desktop/internal/tools/zzmi"
)

const (
	zzmiFixerDirName      = "zzmi-mod-fixer"
	zzmiLatestReleaseURL  = "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/releases/latest"
	zzmiRulesFileName     = "rules.json.zst"
	zzmiRulesManifestName = "rules.manifest.json"
	zzmiRulesCacheDirName = "rules"
	zzmiRulesActiveName   = "active-rules"
	zzmiMaxDownload       = 64 << 20
	zzmiCheckCooldown     = 1 * time.Hour
	zzmiLatestReleaseKey  = "mod_tools:zzmi-mod-fixer:latest-release"
)

type ZZMIFixerRuleStatus struct {
	ActiveSource          string  `json:"activeSource"`
	ActiveTag             string  `json:"activeTag"`
	ActiveCommit          string  `json:"activeCommit"`
	LatestTag             *string `json:"latestTag,omitempty"`
	LatestCommit          *string `json:"latestCommit,omitempty"`
	UpdateAvailable       bool    `json:"updateAvailable"`
	CheckedRemotely       bool    `json:"checkedRemotely"`
	RateLimited           bool    `json:"rateLimited"`
	IncompatibilityReason *string `json:"incompatibilityReason,omitempty"`
}

type ZZMIFixerPrepareResult struct {
	Supported   bool                `json:"supported"`
	Rules       ZZMIFixerRuleStatus `json:"rules"`
	BackupBytes int64               `json:"backupBytes"`
	BackupCount int                 `json:"backupCount"`
}

type ZZMIFixerRunInput struct {
	Path string `json:"path"`
	Tool string `json:"tool"`
}

type ZZMIFixerRunResult struct {
	SessionID    *string  `json:"sessionId,omitempty"`
	ScannedINI   int      `json:"scannedIni"`
	ChangedINI   int      `json:"changedIni"`
	ChangedBUF   int      `json:"changedBuf"`
	SkippedFiles int      `json:"skippedFiles"`
	Warnings     []string `json:"warnings"`
}

type ZZMIBackupEntry struct {
	ID           string `json:"id"`
	OriginalPath string `json:"originalPath"`
	RelativePath string `json:"relativePath"`
	BackupName   string `json:"backupName"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	SHA256Before string `json:"sha256Before"`
	SHA256After  string `json:"sha256After"`
}

type ZZMIBackupSession struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Tool          string            `json:"tool"`
	TargetPath    string            `json:"targetPath"`
	RuleTag       string            `json:"ruleTag"`
	RuleCommit    string            `json:"ruleCommit"`
	CreatedAt     string            `json:"createdAt"`
	Status        string            `json:"status"`
	Size          int64             `json:"size"`
	Entries       []ZZMIBackupEntry `json:"entries"`
}

type ZZMIFixerRestoreInput struct {
	Path      string  `json:"path"`
	SessionID string  `json:"sessionId"`
	EntryID   *string `json:"entryId,omitempty"`
	Force     bool    `json:"force"`
}

type ZZMIFixerRestoreConflict struct {
	EntryID      string `json:"entryId"`
	OriginalPath string `json:"originalPath"`
	ExpectedHash string `json:"expectedHash"`
	CurrentHash  string `json:"currentHash"`
}

type ZZMIFixerRestoreResult struct {
	Restored  int                        `json:"restored"`
	Skipped   int                        `json:"skipped"`
	Conflicts []ZZMIFixerRestoreConflict `json:"conflicts"`
}

type ZZMIFixerDeleteBackupInput struct {
	Path      string  `json:"path"`
	SessionID string  `json:"sessionId"`
	EntryID   *string `json:"entryId,omitempty"`
}

type zzmiLatestRelease struct {
	Tag       string            `json:"tag"`
	Commit    string            `json:"commit"`
	Zipball   string            `json:"zipball"`
	Published string            `json:"published"`
	Blobs     map[string]string `json:"blobs"`
	CheckedAt string            `json:"checkedAt"`
}

type zzmiReleaseResponse struct {
	TagName     string `json:"tag_name"`
	ZipballURL  string `json:"zipball_url"`
	PublishedAt string `json:"published_at"`
}

type zzmiGitObject struct {
	Object struct {
		Type string `json:"type"`
		SHA  string `json:"sha"`
		URL  string `json:"url"`
	} `json:"object"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

type zzmiTreeResponse struct {
	Truncated bool `json:"truncated"`
	Tree      []struct {
		Path string `json:"path"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	} `json:"tree"`
}

func (t *Tools) ZZMIFixerPrepare(ctx context.Context, targetPath string, forceRefresh bool) (ZZMIFixerPrepareResult, error) {
	target, _, err := t.zzmiRequireTarget(ctx, targetPath)
	if err != nil {
		return ZZMIFixerPrepareResult{}, err
	}
	pack, source, err := t.zzmiLoadActivePack()
	if err != nil {
		return ZZMIFixerPrepareResult{}, err
	}
	status := ZZMIFixerRuleStatus{ActiveSource: source, ActiveTag: pack.UpstreamTag, ActiveCommit: pack.CommitSHA}
	latest, checked, checkErr := t.zzmiCheckLatest(ctx, forceRefresh)
	status.CheckedRemotely = checked
	if latest != nil {
		status.LatestTag, status.LatestCommit = &latest.Tag, &latest.Commit
		status.UpdateAvailable = !strings.EqualFold(pack.CommitSHA, latest.Commit)
	}
	if checkErr != nil {
		reason := checkErr.Error()
		status.IncompatibilityReason = &reason
	}
	if t.githubRate != nil {
		rate, _ := t.githubRate.GetRateState(ctx)
		status.RateLimited = t.githubRate.IsRateLimited(rate)
	}
	sessions, err := t.zzmiListBackups(target)
	if err != nil {
		return ZZMIFixerPrepareResult{}, err
	}
	var total int64
	for _, session := range sessions {
		total += session.Size
	}
	return ZZMIFixerPrepareResult{Supported: true, Rules: status, BackupBytes: total, BackupCount: len(sessions)}, nil
}

func (t *Tools) ZZMIFixerActivateLatestRules(ctx context.Context) (ZZMIFixerRuleStatus, error) {
	latest, _, err := t.zzmiCheckLatest(ctx, true)
	if err != nil {
		return ZZMIFixerRuleStatus{}, err
	}
	if latest == nil {
		return ZZMIFixerRuleStatus{}, errors.New("latest ZZMI rules are unavailable")
	}
	if err := validateZZMIZipballURL(latest.Zipball); err != nil {
		return ZZMIFixerRuleStatus{}, err
	}
	data, _, err := t.zzmiFetch(ctx, latest.Zipball, zzmiMaxDownload)
	if err != nil {
		return ZZMIFixerRuleStatus{}, fmt.Errorf("download ZZMI rules: %w", err)
	}
	pack, err := zzmiengine.CompileZip(bytes.NewReader(data), int64(len(data)), latest.Tag, latest.Commit, latest.Published, latest.Blobs)
	if err != nil {
		return ZZMIFixerRuleStatus{}, fmt.Errorf("validate ZZMI rules: %w", err)
	}
	compressed, err := zzmiengine.EncodePack(*pack)
	if err != nil {
		return ZZMIFixerRuleStatus{}, err
	}
	if err := t.zzmiStorePack(compressed, *pack); err != nil {
		return ZZMIFixerRuleStatus{}, err
	}
	return ZZMIFixerRuleStatus{ActiveSource: "cached", ActiveTag: pack.UpstreamTag, ActiveCommit: pack.CommitSHA, LatestTag: &latest.Tag, LatestCommit: &latest.Commit, CheckedRemotely: true}, nil
}

func (t *Tools) ZZMIFixerRun(ctx context.Context, input ZZMIFixerRunInput) (ZZMIFixerRunResult, error) {
	target, _, err := t.zzmiRequireTarget(ctx, input.Path)
	if err != nil {
		return ZZMIFixerRunResult{}, err
	}
	pack, _, err := t.zzmiLoadActivePack()
	if err != nil {
		return ZZMIFixerRunResult{}, err
	}
	run, runCtx, err := t.beginToolRun(ctx, nil)
	if err != nil {
		return ZZMIFixerRunResult{}, err
	}
	defer t.finishScriptRun(run)
	t.emitFixToolLog(fmt.Sprintf("ZZMI %s started with rules %s (%s)", input.Tool, pack.UpstreamTag, pack.CommitSHA[:8]), false)
	stage := "scan"
	result, err := zzmiengine.Run(runCtx, target, input.Tool, pack, func(message string) { t.emitFixToolLog(message, false) })
	if err != nil {
		return ZZMIFixerRunResult{}, t.zzmiLogRunError(err, input, stage, pack, "not-started")
	}
	output := ZZMIFixerRunResult{ScannedINI: result.ScannedINI, ChangedINI: result.ChangedINI, ChangedBUF: result.ChangedBUF, SkippedFiles: result.SkippedFiles, Warnings: result.Warnings}
	if len(result.Changes) == 0 {
		t.emitFixToolLog("No changes were required.", false)
		return output, nil
	}
	stage = "commit"
	session, err := t.zzmiCommit(runCtx, target, input.Tool, *pack, result.Changes)
	if err != nil {
		return ZZMIFixerRunResult{}, t.zzmiLogRunError(err, input, stage, pack, "automatic-rollback-attempted")
	}
	output.SessionID = &session.ID
	t.emitFixToolLog(fmt.Sprintf("ZZMI fixer completed: %d INI, %d buffers changed.", result.ChangedINI, result.ChangedBUF), false)
	return output, nil
}

func (t *Tools) ZZMIFixerListBackups(ctx context.Context, targetPath string) ([]ZZMIBackupSession, error) {
	target, _, err := t.zzmiRequireTarget(ctx, targetPath)
	if err != nil {
		return nil, err
	}
	return t.zzmiListBackups(target)
}

func (t *Tools) ZZMIFixerGetBackup(ctx context.Context, targetPath, sessionID string) (ZZMIBackupSession, error) {
	target, _, err := t.zzmiRequireTarget(ctx, targetPath)
	if err != nil {
		return ZZMIBackupSession{}, err
	}
	return t.zzmiReadSession(target, sessionID)
}

func (t *Tools) ZZMIFixerRestore(ctx context.Context, input ZZMIFixerRestoreInput) (ZZMIFixerRestoreResult, error) {
	target, _, err := t.zzmiRequireTarget(ctx, input.Path)
	if err != nil {
		return ZZMIFixerRestoreResult{}, err
	}
	run, runCtx, err := t.beginToolRun(ctx, nil)
	if err != nil {
		return ZZMIFixerRestoreResult{}, err
	}
	defer t.finishScriptRun(run)
	session, err := t.zzmiReadSession(target, input.SessionID)
	if err != nil {
		return ZZMIFixerRestoreResult{}, err
	}
	dir, err := t.zzmiSessionDir(target, session.ID)
	if err != nil {
		return ZZMIFixerRestoreResult{}, err
	}
	result := ZZMIFixerRestoreResult{}
	remaining := make([]ZZMIBackupEntry, 0, len(session.Entries))
	for _, entry := range session.Entries {
		if input.EntryID != nil && entry.ID != *input.EntryID {
			remaining = append(remaining, entry)
			continue
		}
		if err := runCtx.Err(); err != nil {
			return result, err
		}
		if _, err := secureSessionOriginal(target, entry.OriginalPath); err != nil {
			return result, err
		}
		current, readErr := os.ReadFile(entry.OriginalPath)
		currentHash := ""
		if readErr == nil {
			currentHash = sha256Hex(current)
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return result, readErr
		}
		if currentHash == entry.SHA256Before {
			backupPath := filepath.Join(dir, "files", entry.BackupName)
			if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return result, err
			}
			result.Skipped++
			continue
		}
		if currentHash != entry.SHA256After && !input.Force {
			result.Conflicts = append(result.Conflicts, ZZMIFixerRestoreConflict{EntryID: entry.ID, OriginalPath: entry.OriginalPath, ExpectedHash: entry.SHA256After, CurrentHash: currentHash})
			remaining = append(remaining, entry)
			continue
		}
		backupPath := filepath.Join(dir, "files", entry.BackupName)
		backup, err := os.ReadFile(backupPath)
		if err != nil || sha256Hex(backup) != entry.SHA256Before {
			return result, errors.New("ZZMI backup checksum verification failed")
		}
		if err := copyRegularFile(backupPath, entry.OriginalPath); err != nil {
			return result, err
		}
		if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
		result.Restored++
	}
	if input.EntryID != nil && result.Restored == 0 && result.Skipped == 0 && len(result.Conflicts) == 0 {
		return result, contractError("ZZMI backup entry not found")
	}
	session.Entries = remaining
	session.Size = backupEntriesSize(remaining)
	if len(remaining) == 0 {
		if err := os.RemoveAll(dir); err != nil {
			return result, err
		}
	} else if err := writeZZMISession(dir, session); err != nil {
		return result, err
	}
	return result, nil
}

func (t *Tools) ZZMIFixerDeleteBackup(ctx context.Context, input ZZMIFixerDeleteBackupInput) error {
	target, _, err := t.zzmiRequireTarget(ctx, input.Path)
	if err != nil {
		return err
	}
	session, err := t.zzmiReadSession(target, input.SessionID)
	if err != nil {
		return err
	}
	dir, _ := t.zzmiSessionDir(target, session.ID)
	if input.EntryID == nil {
		return os.RemoveAll(dir)
	}
	remaining := make([]ZZMIBackupEntry, 0, len(session.Entries))
	found := false
	for _, entry := range session.Entries {
		if entry.ID == *input.EntryID {
			found = true
			if err := os.Remove(filepath.Join(dir, "files", entry.BackupName)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		remaining = append(remaining, entry)
	}
	if !found {
		return contractError("ZZMI backup entry not found")
	}
	if len(remaining) == 0 {
		return os.RemoveAll(dir)
	}
	session.Entries = remaining
	session.Size = backupEntriesSize(remaining)
	return writeZZMISession(dir, session)
}

func (t *Tools) ZZMIFixerDeleteAllBackups(ctx context.Context, targetPath string) error {
	target, _, err := t.zzmiRequireTarget(ctx, targetPath)
	if err != nil {
		return err
	}
	dir, err := t.zzmiBackupTargetDir(target)
	if err != nil {
		return err
	}
	base, err := t.zzmiBackupBase()
	if err != nil {
		return err
	}
	if !sameOrChildPath(base, dir) || filepath.Clean(base) == filepath.Clean(dir) {
		return errors.New("unsafe ZZMI backup cleanup path")
	}
	return os.RemoveAll(dir)
}

func (t *Tools) zzmiRequireTarget(ctx context.Context, targetPath string) (string, string, error) {
	client, err := t.requireClient()
	if err != nil {
		return "", "", err
	}
	games, err := client.GamePaths.List(ctx)
	if err != nil {
		return "", "", err
	}
	target, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", contractError("Destination path does not exist")
		}
		return "", "", contractError("Path is outside the managed ZZMI mod folder")
	}
	target, _ = filepath.Abs(target)
	for _, game := range games {
		if game.Importer == nil || !strings.EqualFold(*game.Importer, "ZZMI") {
			continue
		}
		root, resolveErr := filepath.EvalSymlinks(game.ModFolderPath)
		if resolveErr != nil {
			continue
		}
		root, _ = filepath.Abs(root)
		if sameOrChildPath(root, target) {
			info, statErr := os.Stat(target)
			if statErr != nil || !info.IsDir() {
				return "", "", contractError("ZZMI fixer target must be a directory")
			}
			return target, root, nil
		}
	}
	return "", "", contractError("Path is outside the managed ZZMI mod folder")
}

func (t *Tools) zzmiLoadActivePack() (*zzmiengine.RulePack, string, error) {
	dir, err := t.appDataPath(filepath.Join(appdata.ToolsDir, zzmiFixerDirName))
	if err == nil {
		active, readErr := os.ReadFile(filepath.Join(dir, zzmiRulesActiveName))
		digest := strings.TrimSpace(string(active))
		if readErr == nil && validSHA256Hex(digest) {
			if pack, loadErr := loadZZMIPackVersion(filepath.Join(dir, zzmiRulesCacheDirName, strings.ToLower(digest)), digest); loadErr == nil {
				return pack, "cached", nil
			}
		}
	}
	pack, err := zzmiengine.LoadEmbedded()
	return pack, "embedded", err
}

func (t *Tools) zzmiStorePack(data []byte, pack zzmiengine.RulePack) error {
	dir, err := t.appDataPath(filepath.Join(appdata.ToolsDir, zzmiFixerDirName))
	if err != nil {
		return err
	}
	digest := sha256Hex(data)
	versionDir := filepath.Join(dir, zzmiRulesCacheDirName, digest)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return err
	}
	manifest, _ := json.MarshalIndent(map[string]any{"schemaVersion": pack.SchemaVersion, "tag": pack.UpstreamTag, "commit": pack.CommitSHA, "sha256": digest, "installedAt": time.Now().UTC().Format(time.RFC3339Nano)}, "", "  ")
	if err := writeAtomicBytes(filepath.Join(versionDir, zzmiRulesFileName), data); err != nil {
		return err
	}
	if err := writeAtomicBytes(filepath.Join(versionDir, zzmiRulesManifestName), manifest); err != nil {
		return err
	}
	if _, err := loadZZMIPackVersion(versionDir, digest); err != nil {
		return fmt.Errorf("verify stored ZZMI rules: %w", err)
	}
	return writeAtomicBytes(filepath.Join(dir, zzmiRulesActiveName), []byte(digest+"\n"))
}

func loadZZMIPackVersion(dir, expectedDigest string) (*zzmiengine.RulePack, error) {
	data, err := os.ReadFile(filepath.Join(dir, zzmiRulesFileName))
	if err != nil || !strings.EqualFold(expectedDigest, sha256Hex(data)) {
		return nil, errors.New("invalid cached ZZMI rule digest")
	}
	manifestData, err := os.ReadFile(filepath.Join(dir, zzmiRulesManifestName))
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
		SHA256 string `json:"sha256"`
	}
	if json.Unmarshal(manifestData, &manifest) != nil || !strings.EqualFold(manifest.SHA256, expectedDigest) {
		return nil, errors.New("invalid cached ZZMI rule manifest")
	}
	pack, err := zzmiengine.DecodePack(data)
	if err != nil || pack.UpstreamTag != manifest.Tag || !strings.EqualFold(pack.CommitSHA, manifest.Commit) {
		return nil, errors.New("cached ZZMI rule identity does not match its manifest")
	}
	return pack, nil
}

func validHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSHA256Hex(value string) bool {
	return validHex(value, sha256.Size*2)
}

func (t *Tools) zzmiGetCachedLatestRelease(ctx context.Context) *zzmiLatestRelease {
	raw, err := t.getAppState(ctx, zzmiLatestReleaseKey)
	if err != nil || raw == nil {
		return nil
	}
	var release zzmiLatestRelease
	if json.Unmarshal([]byte(*raw), &release) != nil || release.Tag == "" || release.Commit == "" || release.Zipball == "" {
		return nil
	}
	return &release
}

func (t *Tools) zzmiRememberLatest(ctx context.Context, latest *zzmiLatestRelease) error {
	stored := *latest
	stored.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if err := t.setAppState(ctx, zzmiLatestReleaseKey, string(raw)); err != nil {
		return err
	}
	*latest = stored
	return nil
}

func zzmiCacheFresh(checkedAt string, now time.Time) bool {
	parsed, err := time.Parse(time.RFC3339Nano, checkedAt)
	if err != nil || parsed.After(now) {
		return false
	}
	return now.Sub(parsed) < zzmiCheckCooldown
}

func (t *Tools) zzmiCheckLatest(ctx context.Context, force bool) (*zzmiLatestRelease, bool, error) {
	cached := t.zzmiGetCachedLatestRelease(ctx)
	if !force && cached != nil && zzmiCacheFresh(cached.CheckedAt, time.Now().UTC()) {
		return cached, false, nil
	}
	latest, checked, err := t.zzmiFetchLatest(ctx)
	if err != nil {
		if !force && cached != nil {
			if persistErr := t.zzmiRememberLatest(ctx, cached); persistErr != nil {
				t.logError(persistErr, "ZZMIFixerCache")
			}
			return cached, false, nil
		}
		return nil, checked, err
	}
	if err := t.zzmiRememberLatest(ctx, latest); err != nil {
		return nil, true, err
	}
	return latest, true, nil
}

func (t *Tools) zzmiFetchLatest(ctx context.Context) (*zzmiLatestRelease, bool, error) {
	if t.githubRate != nil {
		allowed, _, err := t.githubRate.CanUseGitHubAPI(ctx, infra.GitHubRateCheckOptions{RefreshIfMissing: true})
		if err != nil {
			return nil, false, err
		}
		if !allowed {
			return nil, false, errors.New("GitHub API rate limit is exhausted")
		}
	}
	var release zzmiReleaseResponse
	if err := t.zzmiFetchJSON(ctx, zzmiLatestReleaseURL, &release); err != nil {
		return nil, false, err
	}
	if release.TagName == "" || release.ZipballURL == "" {
		return nil, true, errors.New("latest ZZMI release is incomplete")
	}
	refURL := "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/git/ref/tags/" + url.PathEscape(release.TagName)
	var object zzmiGitObject
	if err := t.zzmiFetchJSON(ctx, refURL, &object); err != nil {
		return nil, true, err
	}
	commit := object.Object.SHA
	if object.Object.Type == "tag" {
		if !isGitHubAPIURL(object.Object.URL) {
			return nil, true, errors.New("unsafe annotated tag URL")
		}
		var tag zzmiGitObject
		if err := t.zzmiFetchJSON(ctx, object.Object.URL, &tag); err != nil {
			return nil, true, err
		}
		commit = tag.Object.SHA
		if commit == "" {
			commit = tag.SHA
		}
	}
	if !validHex(commit, 40) {
		return nil, true, errors.New("latest ZZMI release has an invalid commit")
	}
	var tree zzmiTreeResponse
	if err := t.zzmiFetchJSON(ctx, "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/git/trees/"+commit+"?recursive=1", &tree); err != nil {
		return nil, true, err
	}
	if tree.Truncated {
		return nil, true, errors.New("latest ZZMI Git tree is truncated")
	}
	blobs := map[string]string{}
	for _, entry := range tree.Tree {
		if entry.Type == "blob" && (strings.HasPrefix(entry.Path, "Source Codes/Assets/PlayerCharacterPYData/") || entry.Path == "Source Codes/Jane.remapper.py" || entry.Path == "Source Codes/Dialyn.remapper.py") {
			blobs[entry.Path] = entry.SHA
		}
	}
	return &zzmiLatestRelease{Tag: release.TagName, Commit: commit, Zipball: release.ZipballURL, Published: release.PublishedAt, Blobs: blobs}, true, nil
}

func (t *Tools) zzmiFetchJSON(ctx context.Context, rawURL string, target any) error {
	data, _, err := t.zzmiFetch(ctx, rawURL, 4<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}
func (t *Tools) zzmiFetch(ctx context.Context, rawURL string, max int64) ([]byte, http.Header, error) {
	if t.http == nil {
		return nil, nil, errors.New("tools HTTP client is not configured")
	}
	response, err := t.http.Fetch(ctx, rawURL, infra.FetchOptions{Method: http.MethodGet, Header: http.Header{"Accept": []string{"application/vnd.github+json"}}, DisableHTTPErrors: true})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	header := response.Header.Clone()
	if t.githubRate != nil {
		_, _ = t.githubRate.CaptureResponse(ctx, header)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		return nil, header, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil {
		return nil, header, err
	}
	if int64(len(data)) > max {
		return nil, header, errors.New("ZZMI response exceeds the size limit")
	}
	return data, header, nil
}
func validateZZMIZipballURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.github.com") || !strings.HasPrefix(parsed.Path, "/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/") {
		return errors.New("unsafe ZZMI zipball URL")
	}
	return nil
}
func isGitHubAPIURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && strings.EqualFold(parsed.Host, "api.github.com") && strings.HasPrefix(parsed.Path, "/repos/Vonksdesu/ZZZ-Mod-Fixer/git/")
}

func (t *Tools) zzmiBackupBase() (string, error) {
	return t.appDataPath(filepath.Join(appdata.ToolsDir, zzmiFixerDirName, "backups"))
}
func (t *Tools) zzmiBackupTargetDir(target string) (string, error) {
	base, err := t.zzmiBackupBase()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(target))))
	return filepath.Join(base, hex.EncodeToString(sum[:])), nil
}
func (t *Tools) zzmiSessionDir(target, sessionID string) (string, error) {
	if _, err := uuid.Parse(sessionID); err != nil {
		return "", contractError("Invalid ZZMI backup session")
	}
	base, err := t.zzmiBackupTargetDir(target)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, sessionID), nil
}

func (t *Tools) zzmiCommit(ctx context.Context, target, tool string, pack zzmiengine.RulePack, changes []zzmiengine.Change) (ZZMIBackupSession, error) {
	session := ZZMIBackupSession{SchemaVersion: 1, ID: uuid.NewString(), Tool: tool, TargetPath: target, RuleTag: pack.UpstreamTag, RuleCommit: pack.CommitSHA, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Status: "preparing"}
	dir, err := t.zzmiSessionDir(target, session.ID)
	if err != nil {
		return session, err
	}
	staging := filepath.Join(dir, "staging")
	filesDir := filepath.Join(dir, "files")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return session, err
	}
	keepSession := false
	defer func() {
		if !keepSession {
			_ = os.RemoveAll(dir)
		}
	}()
	defer func() { _ = os.RemoveAll(staging) }()
	for index, change := range changes {
		if err := ctx.Err(); err != nil {
			return session, err
		}
		safe, err := secureSessionOriginal(target, change.Path)
		if err != nil {
			return session, err
		}
		original, err := os.ReadFile(safe)
		if err != nil {
			return session, err
		}
		relative, _ := filepath.Rel(target, safe)
		entryID := fmt.Sprintf("%04d", index+1)
		backupName := entryID + filepath.Ext(safe)
		stagePath := filepath.Join(staging, entryID+".new")
		backupPath := filepath.Join(filesDir, backupName)
		if err := writeSyncFile(stagePath, change.Data); err != nil {
			return session, err
		}
		if err := copyRegularFile(safe, backupPath); err != nil {
			return session, err
		}
		backup, err := os.ReadFile(backupPath)
		if err != nil || sha256Hex(backup) != sha256Hex(original) {
			return session, errors.New("ZZMI backup verification failed")
		}
		entry := ZZMIBackupEntry{ID: entryID, OriginalPath: safe, RelativePath: relative, BackupName: backupName, Kind: change.Kind, Size: int64(len(original)), SHA256Before: sha256Hex(original), SHA256After: sha256Hex(change.Data)}
		session.Entries = append(session.Entries, entry)
		session.Size += entry.Size
	}
	if err := writeZZMISession(dir, session); err != nil {
		return session, err
	}
	replaced := []ZZMIBackupEntry{}
	for _, entry := range session.Entries {
		if err := ctx.Err(); err != nil {
			if rollbackErr := rollbackZZMIEntries(dir, replaced); rollbackErr != nil {
				session.Status = "partial"
				_ = writeZZMISession(dir, session)
				keepSession = true
				return session, errors.Join(err, rollbackErr)
			}
			return session, err
		}
		stagePath := filepath.Join(staging, entry.ID+".new")
		if err := copyRegularFile(stagePath, entry.OriginalPath); err != nil {
			rollbackErr := rollbackZZMIEntries(dir, replaced)
			if rollbackErr != nil {
				session.Status = "partial"
				_ = writeZZMISession(dir, session)
				keepSession = true
				return session, errors.Join(err, rollbackErr)
			}
			return session, err
		}
		replaced = append(replaced, entry)
	}
	session.Status = "completed"
	if err := writeZZMISession(dir, session); err != nil {
		rollbackErr := rollbackZZMIEntries(dir, replaced)
		if rollbackErr != nil {
			session.Status = "partial"
			_ = writeZZMISession(dir, session)
			keepSession = true
			return session, errors.Join(err, rollbackErr)
		}
		return session, err
	}
	keepSession = true
	return session, nil
}

func rollbackZZMIEntries(dir string, entries []ZZMIBackupEntry) error {
	var result error
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if err := copyRegularFile(filepath.Join(dir, "files", entry.BackupName), entry.OriginalPath); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}
func secureSessionOriginal(target, candidate string) (string, error) {
	targetAbs, _ := filepath.Abs(target)
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !sameOrChildPath(targetAbs, candidateAbs) {
		return "", errors.New("backup file escapes the selected ZZMI target")
	}
	if info, statErr := os.Lstat(candidateAbs); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("backup destination is a symbolic link")
	}
	resolvedTarget, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		return "", err
	}
	resolvedCandidate, err := filepath.EvalSymlinks(candidateAbs)
	if errors.Is(err, os.ErrNotExist) {
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(candidateAbs))
		if parentErr != nil {
			return "", parentErr
		}
		resolvedCandidate = filepath.Join(resolvedParent, filepath.Base(candidateAbs))
	} else if err != nil {
		return "", err
	}
	if !sameOrChildPath(resolvedTarget, resolvedCandidate) {
		return "", errors.New("backup file resolves outside the selected ZZMI target")
	}
	return candidateAbs, nil
}

func (t *Tools) zzmiCleanupAbandonedStaging() error {
	if t == nil || t.appData == nil {
		return nil
	}
	base, err := t.zzmiBackupBase()
	if err != nil {
		return err
	}
	targets, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, target := range targets {
		if !target.IsDir() || target.Type()&os.ModeSymlink != 0 {
			continue
		}
		sessions, readErr := os.ReadDir(filepath.Join(base, target.Name()))
		if readErr != nil {
			return readErr
		}
		for _, entry := range sessions {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if _, parseErr := uuid.Parse(entry.Name()); parseErr != nil {
				continue
			}
			dir := filepath.Join(base, target.Name(), entry.Name())
			staging := filepath.Join(dir, "staging")
			if _, statErr := os.Stat(staging); errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if !sameOrChildPath(base, staging) {
				return errors.New("unsafe abandoned ZZMI staging path")
			}
			if removeErr := os.RemoveAll(staging); removeErr != nil {
				return removeErr
			}
			manifestPath := filepath.Join(dir, "manifest.json")
			data, readErr := os.ReadFile(manifestPath)
			if readErr != nil {
				continue
			}
			var session ZZMIBackupSession
			if json.Unmarshal(data, &session) == nil && session.Status == "preparing" {
				session.Status = "partial"
				if writeErr := writeZZMISession(dir, session); writeErr != nil {
					return writeErr
				}
			}
		}
	}
	return nil
}
func writeSyncFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}
func writeAtomicBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".zzmi-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	_, writeErr := temp.Write(data)
	if writeErr == nil {
		writeErr = temp.Sync()
	}
	closeErr := temp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return replaceAtomic(name, path)
}
func writeZZMISession(dir string, session ZZMIBackupSession) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicBytes(filepath.Join(dir, "manifest.json"), data)
}
func (t *Tools) zzmiReadSession(target, sessionID string) (ZZMIBackupSession, error) {
	dir, err := t.zzmiSessionDir(target, sessionID)
	if err != nil {
		return ZZMIBackupSession{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return ZZMIBackupSession{}, contractError("ZZMI backup session not found")
	}
	if err != nil {
		return ZZMIBackupSession{}, err
	}
	var session ZZMIBackupSession
	if err := json.Unmarshal(data, &session); err != nil {
		return session, err
	}
	if session.SchemaVersion != 1 || session.ID != sessionID || !strings.EqualFold(filepath.Clean(session.TargetPath), filepath.Clean(target)) {
		return session, errors.New("invalid ZZMI backup manifest")
	}
	return session, nil
}
func (t *Tools) zzmiListBackups(target string) ([]ZZMIBackupSession, error) {
	base, err := t.zzmiBackupTargetDir(target)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return []ZZMIBackupSession{}, nil
	}
	if err != nil {
		return nil, err
	}
	sessions := []ZZMIBackupSession{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		session, readErr := t.zzmiReadSession(target, entry.Name())
		if readErr == nil {
			sessions = append(sessions, session)
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].CreatedAt > sessions[j].CreatedAt })
	return sessions, nil
}
func backupEntriesSize(entries []ZZMIBackupEntry) int64 {
	var size int64
	for _, entry := range entries {
		size += entry.Size
	}
	return size
}
func (t *Tools) zzmiLogRunError(err error, input ZZMIFixerRunInput, stage string, pack *zzmiengine.RulePack, rollback string) error {
	if err == nil {
		return nil
	}
	fields := map[string]any{"tool": input.Tool, "path": input.Path, "rollback": rollback}
	if pack != nil {
		fields["rules"] = pack.UpstreamTag
		fields["commit"] = pack.CommitSHA
	}
	return infra.ReportError(t.log, err, "Tools", infra.Diagnostic{
		Severity: infra.DiagnosticError, Operation: "zzmi-fixer", Stage: stage, Fields: fields,
	})
}
