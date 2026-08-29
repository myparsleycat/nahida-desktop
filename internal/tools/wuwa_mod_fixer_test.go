package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

type wuwaRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn wuwaRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestBuildWuwaCLIArgs(t *testing.T) {
	args, err := buildWuwaCLIArgs(`C:\Mods\A`, `C:\Tools\config.json`, WuwaFixerOptions{
		DerivedHashes: true,
		AemeathMech:   true,
		Rendering33:   true,
		AeroFix:       "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--cli", "--path", `C:\Mods\A`, "--config", `C:\Tools\config.json`, "--derived-hashes", "--aemeath-mech", "--rendering-33", "--aero-fix", "2"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	if _, err := buildWuwaCLIArgs("mods", "config", WuwaFixerOptions{DerivedHashes: true, StableTexture: true}); err == nil {
		t.Fatal("mutually exclusive texture options were accepted")
	}
	if _, err := buildWuwaCLIArgs("mods", "config", WuwaFixerOptions{AeroFix: "3"}); err == nil {
		t.Fatal("invalid aero fix was accepted")
	}
}

func TestParseWuwaLatestReleaseAndDigest(t *testing.T) {
	digest := "sha256:00"
	payload := wuwaReleaseResponse{TagName: "v1.2.3"}
	payload.Assets = append(payload.Assets, struct {
		Name               string  `json:"name"`
		BrowserDownloadURL string  `json:"browser_download_url"`
		Digest             *string `json:"digest"`
	}{Name: "Wuwa_Mod_Fixer_v1.2.3.exe", BrowserDownloadURL: "https://example.test/fixer.exe", Digest: &digest})
	release, err := parseWuwaLatestRelease(payload)
	if err != nil || release.Version != "v1.2.3" || release.Asset.Name != "Wuwa_Mod_Fixer_v1.2.3.exe" {
		t.Fatalf("release = %#v, %v", release, err)
	}
	data := []byte("verified executable")
	sum := sha256.Sum256(data)
	valid := "sha256:" + hex.EncodeToString(sum[:])
	if err := verifyWuwaDigest(data, &valid); err != nil {
		t.Fatalf("valid digest rejected: %v", err)
	}
	invalid := "sha256:" + hex.EncodeToString(make([]byte, sha256.Size))
	if err := verifyWuwaDigest(data, &invalid); err == nil {
		t.Fatal("digest mismatch was accepted")
	}
}

func TestParseWuwaLatestReleaseAcceptsElectronAssetPattern(t *testing.T) {
	payload := wuwaReleaseResponse{TagName: "v1.2-beta"}
	payload.Assets = append(payload.Assets, struct {
		Name               string  `json:"name"`
		BrowserDownloadURL string  `json:"browser_download_url"`
		Digest             *string `json:"digest"`
	}{Name: "Wuwa_Mod_Fixer_v1.2-beta.exe", BrowserDownloadURL: "https://example.test/fixer.exe"})
	release, err := parseWuwaLatestRelease(payload)
	if err != nil || release.Asset.Name != "Wuwa_Mod_Fixer_v1.2-beta.exe" {
		t.Fatalf("release = %#v, %v", release, err)
	}
	if version := extractWuwaVersion(release.Asset.Name); version != nil {
		t.Fatalf("Electron's stricter filename version extraction should return nil, got %q", *version)
	}
}

func TestWuwaBackupScanSizeAndRollback(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	modPath := filepath.Join(root, "Character")
	if err := os.MkdirAll(modPath, 0o700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(modPath, "body.ini")
	if err := os.WriteFile(original, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldBackup := filepath.Join(modPath, "body.ini_2026-01-02 03-04-05.BAK")
	newBackup := filepath.Join(modPath, "body.ini_2026-01-02 03-05-00.123.BAK")
	if err := os.WriteFile(oldBackup, []byte("oldest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBackup, []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	createdThenRemoved := filepath.Join(modPath, "created.ini")
	zeroBackup := createdThenRemoved + "_2026-01-02 03-05-01.BAK"
	if err := os.WriteFile(createdThenRemoved, []byte("generated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zeroBackup, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)
	useToolsTestAppData(t, service, t.TempDir())
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "WW", ModFolderPath: root}); err != nil {
		t.Fatal(err)
	}
	groups, err := service.WuwaFixerScanBackups(ctx, modPath)
	if err != nil || len(groups) != 2 || groups[0].GroupKey != "2026-01-02 03-05" {
		t.Fatalf("groups = %#v, %v", groups, err)
	}
	size, err := service.WuwaFixerGetBackupSize(ctx, modPath)
	if err != nil || size.Count != 3 || size.Bytes != int64(len("oldest")+len("newer")+len("generated")) {
		t.Fatalf("size = %#v, %v", size, err)
	}
	if err := service.WuwaFixerRollbackToGroup(ctx, modPath, "2026-01-02 03-04"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(original)
	if err != nil || string(got) != "oldest" {
		t.Fatalf("rolled back body = %q, %v", got, err)
	}
	if _, err := os.Stat(createdThenRemoved); !os.IsNotExist(err) {
		t.Fatalf("zero-byte backup did not delete generated original: %v", err)
	}
	if remaining, err := listWuwaBackupFiles(modPath); err != nil || len(remaining) != 0 {
		t.Fatalf("remaining backups = %#v, %v", remaining, err)
	}
}

func TestWuwaBackupOperationsRejectOutsideManagedRoot(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	managed := t.TempDir()
	outside := t.TempDir()
	service := New()
	service.UseClient(client)
	useToolsTestAppData(t, service, t.TempDir())
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "WW", ModFolderPath: managed}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.WuwaFixerScanBackups(ctx, outside); err == nil || err.Error() != "Path is outside the managed mod folder" {
		t.Fatalf("outside path error = %v", err)
	}
}

func TestWuwaRequireModPathUsesOnlyPrimaryRootAndElectronErrorPriority(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	managed := t.TempDir()
	linked := t.TempDir()
	linkedTarget := filepath.Join(linked, "Character")
	if err := os.MkdirAll(linkedTarget, 0700); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.UseClient(client)
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: "WW", ModFolderPath: managed, LinkedModFolderPath: &linked}); err != nil {
		t.Fatal(err)
	}
	if err := service.wuwaRequireModPath(ctx, linkedTarget); err == nil || err.Error() != "Path is outside the managed mod folder" {
		t.Fatalf("linked root error = %v", err)
	}
	if err := service.wuwaRequireModPath(ctx, filepath.Join(t.TempDir(), "missing")); err == nil || err.Error() != "Path is outside the managed mod folder" {
		t.Fatalf("outside missing error = %v", err)
	}
	if err := service.wuwaRequireModPath(ctx, filepath.Join(managed, "missing")); err == nil || err.Error() != "Destination path does not exist" {
		t.Fatalf("inside missing error = %v", err)
	}
	file := filepath.Join(managed, "mod.ini")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.wuwaRequireModPath(ctx, file); err != nil {
		t.Fatalf("managed file path should pass Electron pathExists contract: %v", err)
	}
}

func TestListWuwaBackupFilesFollowsFileSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.bin")
	link := filepath.Join(root, "linked_2026-01-02 03-04-05.BAK")
	if err := os.WriteFile(target, []byte("backup"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	paths, err := listWuwaBackupFiles(root)
	if err != nil || len(paths) != 1 || filepath.Clean(paths[0]) != filepath.Clean(link) {
		t.Fatalf("paths = %#v, %v", paths, err)
	}
}

func TestWuwaInstalledFallbackUsesFirstFilesystemMatch(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)
	userData := useToolsTestAppData(t, service, t.TempDir())
	toolDir := filepath.Join(userData, "tools", wuwaFixerDirName)
	if err := os.MkdirAll(toolDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Wuwa_Mod_Fixer_v9.0.0.exe", "Wuwa_Mod_Fixer_v1.0.0.exe", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(toolDir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := os.Open(toolDir)
	if err != nil {
		t.Fatal(err)
	}
	names, err := dir.Readdirnames(-1)
	_ = dir.Close()
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, name := range names {
		if wuwaBinaryRE.MatchString(name) {
			want = name
			break
		}
	}
	got, err := service.wuwaGetInstalledBinaryInfo(ctx)
	if err != nil || got.BinaryPath == nil || filepath.Base(*got.BinaryPath) != want {
		t.Fatalf("installed = %#v, %v; first filesystem match = %q", got, err, want)
	}
}

type wuwaNotificationSettings struct {
	value any
	err   error
}

func (s wuwaNotificationSettings) Get(context.Context, string) (any, error) { return s.value, s.err }
func (wuwaNotificationSettings) GetBisectPreserveD3dx(context.Context) (bool, error) {
	return false, nil
}
func (wuwaNotificationSettings) GetDisabledPrefixStyle(context.Context) (string, error) {
	return "underscore", nil
}

func TestWuwaAutomaticUpdateNotificationHonorsSettingAndUsesNativeNotification(t *testing.T) {
	for _, test := range []struct {
		name       string
		settings   wuwaNotificationSettings
		wantNotify bool
	}{
		{name: "setting read error", settings: wuwaNotificationSettings{err: fmt.Errorf("db failed")}},
		{name: "disabled", settings: wuwaNotificationSettings{value: false}},
		{name: "enabled", settings: wuwaNotificationSettings{value: true}, wantNotify: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			toasts, notifications := 0, 0
			service := NewWithOptions(Options{
				Settings: test.settings,
				EventEmit: func(name string, _ ...any) {
					if name == "fn:toast" {
						toasts++
					}
				},
				Notify: func(title, body string) error {
					notifications++
					if title != "Wuwa Mod Fixer updated" || body != "vv1 → vv2" {
						t.Fatalf("notification = %q / %q", title, body)
					}
					return nil
				},
			})
			service.notifyWuwaAutomaticUpdate(context.Background(), "v1", "v2")
			want := 0
			if test.wantNotify {
				want = 1
			}
			if toasts != want || notifications != want {
				t.Fatalf("toast/native = %d/%d, want %d/%d", toasts, notifications, want, want)
			}
		})
	}
}

func TestWuwaConfigDownloadErrorMatchesElectronMessage(t *testing.T) {
	transport := wuwaRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable", Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	})
	service := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: &http.Client{Transport: transport}})})
	useToolsTestAppData(t, service, t.TempDir())
	_, err := service.wuwaEnsureLatestConfig(context.Background())
	if err == nil || err.Error() != "Failed to download Wuwa Mod Fixer config: HTTP 503" {
		t.Fatalf("error = %v", err)
	}
}

func TestWuwaRunLetsFileDestinationReachExternalToolLog(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := t.TempDir()
	binary := filepath.Join(root, "Wuwa_Mod_Fixer_v1.0.0.exe")
	destination := filepath.Join(root, "mod.ini")
	for path, body := range map[string]string{binary: "not-an-executable", destination: "mod"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.AppState.Upsert(ctx, wuwaBinaryPathKey, binary, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	transport := wuwaRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	var logs []FixToolLogEvent
	service := NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: &http.Client{Transport: transport}}),
		EventEmit: func(name string, data ...any) {
			if name == fixToolLogEvent && len(data) == 1 {
				if event, ok := data[0].(FixToolLogEvent); ok {
					logs = append(logs, event)
				}
			}
		},
	})
	service.UseClient(client)
	useToolsTestAppData(t, service, root)
	if err := service.WuwaFixerRun(ctx, destination, WuwaFixerOptions{}); err != nil {
		t.Fatalf("run returned raw error: %v", err)
	}
	if len(logs) == 0 || !strings.HasPrefix(logs[len(logs)-1].Message, "Error: ") {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestWuwaInstallVerifiesAndPersistsRelease(t *testing.T) {
	ctx := context.Background()
	binary := []byte("portable Wuwa fixer executable")
	sum := sha256.Sum256(binary)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	assetURL := "https://downloads.example.test/Wuwa_Mod_Fixer_v1.2.3.exe"
	response := func(status int, body []byte, githubRate bool) *http.Response {
		header := make(http.Header)
		if githubRate {
			header.Set("X-RateLimit-Limit", "60")
			header.Set("X-RateLimit-Remaining", "58")
			header.Set("X-RateLimit-Reset", "2000000000")
			header.Set("X-RateLimit-Used", "2")
			header.Set("X-RateLimit-Resource", "core")
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body))}
	}
	transport := wuwaRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case "https://api.github.com/rate_limit":
			return response(http.StatusOK, []byte(`{"rate":{"limit":60,"remaining":59,"reset":2000000000,"used":1,"resource":"core"}}`), true), nil
		case wuwaReleasesLatestURL:
			payload := fmt.Sprintf(`{"tag_name":"v1.2.3","assets":[{"name":"Wuwa_Mod_Fixer_v1.2.3.exe","browser_download_url":%q,"digest":%q}]}`, assetURL, digest)
			return response(http.StatusOK, []byte(payload), true), nil
		case assetURL:
			return response(http.StatusOK, binary, false), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", request.URL)
		}
	})
	client := openToolsTestDB(t)
	service := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: &http.Client{Transport: transport}})})
	service.UseClient(client)
	userData := useToolsTestAppData(t, service, t.TempDir())
	toolDir := filepath.Join(userData, "tools", wuwaFixerDirName)
	if err := os.MkdirAll(toolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldBinary := filepath.Join(toolDir, "Wuwa_Mod_Fixer_v0.9.0.exe")
	if err := os.WriteFile(oldBinary, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := service.WuwaFixerInstallOrUpdate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || status.InstalledVersion == nil || *status.InstalledVersion != "v1.2.3" || status.BinaryPath == nil {
		t.Fatalf("status = %#v", status)
	}
	installed, err := os.ReadFile(*status.BinaryPath)
	if err != nil || !bytes.Equal(installed, binary) {
		t.Fatalf("installed bytes = %q, %v", installed, err)
	}
	if _, err := os.Stat(oldBinary); !os.IsNotExist(err) {
		t.Fatalf("old binary still exists: %v", err)
	}
	storedPath, err := client.AppState.GetValue(ctx, wuwaBinaryPathKey)
	if err != nil || storedPath == nil || *storedPath != *status.BinaryPath {
		t.Fatalf("stored path = %v, %v", storedPath, err)
	}
}
