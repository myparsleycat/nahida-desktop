package xxmi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

func newXXMITestClient(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	return client
}

func useBuiltinLauncher(t *testing.T, service *XXMI) {
	t.Helper()
	if err := service.SetLauncherMode(context.Background(), LauncherBuiltin); err != nil {
		t.Fatal(err)
	}
}

func useExternalLauncher(t *testing.T, service *XXMI, root string) {
	t.Helper()
	if err := service.SaveXXMIPath(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := service.SetLauncherMode(context.Background(), LauncherExternal); err != nil {
		t.Fatal(err)
	}
}

func TestLauncherModeDefaultsToExternalLauncher(t *testing.T) {
	ctx := context.Background()
	client := newXXMITestClient(t)
	service := New()
	service.UseClient(client)

	mode, err := service.GetLauncherMode(ctx)
	if err != nil || mode != LauncherExternal {
		t.Fatalf("fresh mode = %q, %v", mode, err)
	}
	if err := client.XXMIImporters.Upsert(ctx, "GIMI", `{"enabled":true}`); err != nil {
		t.Fatal(err)
	}
	mode, err = service.GetLauncherMode(ctx)
	if err != nil || mode != LauncherExternal {
		t.Fatalf("mode with unselected built-in importers = %q, %v", mode, err)
	}

	if err := service.SetLauncherMode(ctx, LauncherBuiltin); err != nil {
		t.Fatal(err)
	}
	mode, err = service.GetLauncherMode(ctx)
	if err != nil || mode != LauncherBuiltin {
		t.Fatalf("selected mode = %q, %v", mode, err)
	}
	if err := service.SetLauncherMode(ctx, LauncherExternal); err != nil {
		t.Fatal(err)
	}
	mode, err = service.GetLauncherMode(ctx)
	if err != nil || mode != LauncherExternal {
		t.Fatalf("mode after switching back = %q, %v", mode, err)
	}
}

func TestSetLauncherModeRejectsUnknownModeAndActiveLaunch(t *testing.T) {
	ctx := context.Background()
	service := New()
	service.UseClient(newXXMITestClient(t))

	if err := service.SetLauncherMode(ctx, "portable"); err == nil {
		t.Fatal("unknown mode was accepted")
	}
	if !service.acquireImporter("GIMI") {
		t.Fatal("acquire importer")
	}
	defer service.releaseImporter("GIMI")
	if err := service.SetLauncherMode(ctx, LauncherBuiltin); err == nil || err.Error() != "XXMI_BUSY" {
		t.Fatalf("error = %v, want XXMI_BUSY", err)
	}
}

func TestExternalModeServesLauncherConfigToConsumers(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	manifestDir := filepath.Join(root, "Resources", "Packages", "XXMI")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(manifestDir, "Manifest.json"),
		[]byte(`{"version":"v1.2.3"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	var events []string
	service := NewWithOptions(Options{EventEmit: func(name string, _ ...any) {
		events = append(events, name)
	}})
	service.UseClient(newXXMITestClient(t))
	useExternalLauncher(t, service, root)

	if len(events) != 1 || events[0] != "renderer:reload" {
		t.Fatalf("events = %v, want one reload for the saved path", events)
	}
	data, err := service.GetXXMIData(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if data.Mode != LauncherExternal || data.XXMIPath == nil || *data.XXMIPath != root ||
		data.DLLVersion == nil || *data.DLLVersion != "v1.2.3" {
		t.Fatalf("data = %+v", data)
	}
	if len(data.EnabledImporters) != 1 || data.EnabledImporters[0].Key != "GIMI" ||
		data.EnabledImporters[0].ImporterFolder != filepath.Join(root, "GIMI") {
		t.Fatalf("enabled importers = %+v", data.EnabledImporters)
	}

	runtime, err := service.ResolveHuntingRuntime(ctx, "gimi")
	if err != nil || runtime.INIPath != filepath.Join(root, "GIMI", "d3dx.ini") {
		t.Fatalf("hunting runtime = %+v, %v", runtime, err)
	}
	overview, err := service.GetOverview(ctx)
	if err != nil || !overview.Configured || overview.LauncherMode != LauncherExternal ||
		overview.Root != root || len(overview.Importers) != 1 {
		t.Fatalf("overview = %+v, %v", overview, err)
	}
	version, ok := service.DeployedLibsVersion(ctx, "GIMI")
	if !ok || version != "v1.2.3" {
		t.Fatalf("deployed libs = %q, %v", version, ok)
	}
	updates, err := service.CheckUpdates(ctx, false)
	if err != nil || len(updates) != 0 {
		t.Fatalf("updates = %+v, %v", updates, err)
	}
}

func TestExternalModeTreatsCorruptConfigAsUnconfigured(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	service := New()
	service.UseClient(newXXMITestClient(t))
	useExternalLauncher(t, service, root)
	if err := os.WriteFile(filepath.Join(root, xxmiConfigName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	importers, err := service.GetEnabledImporters(ctx)
	if err != nil || len(importers) != 0 {
		t.Fatalf("importers = %+v, %v", importers, err)
	}
	overview, err := service.GetOverview(ctx)
	if err != nil || overview.Configured {
		t.Fatalf("overview = %+v, %v", overview, err)
	}
	if err := service.StartGame(ctx, "GIMI"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("start error = %v", err)
	}
}

func TestSetExternalImporterEnabledHidesImporterFromConsumers(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	service := New()
	service.UseClient(newXXMITestClient(t))
	useExternalLauncher(t, service, root)
	var changes []int
	service.UseExternalImportersChanged(func(ctx context.Context) {
		importers, err := service.GetEnabledImporters(ctx)
		if err != nil {
			t.Fatal(err)
		}
		changes = append(changes, len(importers))
	})

	if err := service.SetExternalImporterEnabled(ctx, "unknown", false); err == nil {
		t.Fatal("unknown importer was accepted")
	}
	if !service.acquireImporter("GIMI") {
		t.Fatal("acquire importer")
	}
	if err := service.SetExternalImporterEnabled(ctx, "GIMI", false); err == nil || err.Error() != "XXMI_BUSY" {
		t.Fatalf("error = %v, want XXMI_BUSY", err)
	}
	service.releaseImporter("GIMI")
	if len(changes) != 0 {
		t.Fatalf("failed changes notified consumers: %v", changes)
	}

	if err := service.SetExternalImporterEnabled(ctx, "gimi", false); err != nil {
		t.Fatal(err)
	}
	if err := service.SetExternalImporterEnabled(ctx, "GIMI", false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(changes, []int{0}) {
		t.Fatalf("disable notifications = %v, want [0]", changes)
	}
	data, err := service.GetXXMIData(ctx)
	if err != nil || len(data.EnabledImporters) != 0 ||
		len(data.DisabledImporters) != 1 || data.DisabledImporters[0].Key != "GIMI" {
		t.Fatalf("data after disable = %+v, %v", data, err)
	}
	if _, err := service.ResolveHuntingRuntime(ctx, "GIMI"); err == nil {
		t.Fatal("hunting runtime resolved for a disabled importer")
	}
	if err := service.StartGame(ctx, "GIMI"); err == nil || !strings.Contains(err.Error(), "is disabled") {
		t.Fatalf("start error = %v", err)
	}
	detected, err := service.DetectExternalLauncher(ctx)
	if err != nil || len(detected.Importers) != 1 || detected.Importers[0] != "GIMI" {
		t.Fatalf("detected = %+v, %v", detected, err)
	}

	if err := service.SetExternalImporterEnabled(ctx, "GIMI", true); err != nil {
		t.Fatal(err)
	}
	if err := service.SetExternalImporterEnabled(ctx, "GIMI", true); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(changes, []int{0, 1}) {
		t.Fatalf("enable notifications = %v, want [0 1]", changes)
	}
	data, err = service.GetXXMIData(ctx)
	if err != nil || len(data.EnabledImporters) != 1 || len(data.DisabledImporters) != 0 {
		t.Fatalf("data after enable = %+v, %v", data, err)
	}
}

func TestExternalInstallImporterPackageOverlaysAndPreservesMods(t *testing.T) {
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	gimi := filepath.Join(root, "GIMI")
	for _, directory := range []string{
		filepath.Join(gimi, "Mods"), filepath.Join(gimi, "Core"), filepath.Join(gimi, "ShaderFixes"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(gimi, "Mods", "user-mod.ini"):    "keep-mod",
		filepath.Join(gimi, "d3dx.ini"):                "user-ini",
		filepath.Join(gimi, "Core", "obsolete.ini"):    "gone",
		filepath.Join(gimi, "Core", "keep.ini"):        "old-keep",
		filepath.Join(gimi, "ShaderFixes", "old.hlsl"): "gone",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	zipBody := writeXXMITestZip(t, map[string]string{
		"Core/GIMI/main.ini": "global $version = 1.23\n",
		"Core/auto_update.xcmd": "[PreInstall]\ndelete = Core/obsolete.ini\n\n" +
			"[PostInstall]\ndelete = ShaderFixes/old.hlsl\n",
		"Core/keep.ini":            "new-keep",
		"d3dx.ini":                 "package-ini",
		"ShaderFixes/new.hlsl":     "new-shader",
		"Mods/should-not-copy.ini": "from-zip",
	})
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if !strings.Contains(request.URL.Path, "/SilentNightSound/GIMI-Package/releases/download/") ||
			!strings.HasSuffix(request.URL.Path, "/GIMI-PACKAGE-v1.2.3.zip") {
			t.Fatalf("URL = %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(zipBody)), Request: request,
		}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(infraClient)
	service := NewWithOptions(Options{HTTP: infraClient, Download: download, Archive: infra.NewArchive()})
	service.UseClient(newXXMITestClient(t))
	useExternalLauncher(t, service, root)

	if err := service.InstallImporterPackage(
		context.Background(), InstallImporterPackageInput{Importer: "GIMI", Version: "v1.2.3"},
	); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(gimi, "Mods", "user-mod.ini"), "keep-mod")
	assertFile(t, filepath.Join(gimi, "d3dx.ini"), "user-ini")
	assertFile(t, filepath.Join(gimi, "Core", "keep.ini"), "new-keep")
	assertFile(t, filepath.Join(gimi, "ShaderFixes", "new.hlsl"), "new-shader")
	for _, removed := range []string{
		filepath.Join(gimi, "Core", "obsolete.ini"),
		filepath.Join(gimi, "ShaderFixes", "old.hlsl"),
		filepath.Join(gimi, "Mods", "should-not-copy.ini"),
	} {
		if _, err := os.Stat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still present: %v", removed, err)
		}
	}
	rawConfig, err := os.ReadFile(filepath.Join(root, xxmiConfigName))
	if err != nil || !bytes.Contains(rawConfig, []byte(`"auto_update": false`)) ||
		!bytes.Contains(rawConfig, []byte(`"deployed_version": "1.2.3"`)) {
		t.Fatalf("config = %q, error = %v", rawConfig, err)
	}
	data, err := service.GetXXMIData(context.Background())
	if err != nil || len(data.EnabledImporters) != 1 || data.EnabledImporters[0].InstalledVersion == nil ||
		*data.EnabledImporters[0].InstalledVersion != "1.2.3" {
		t.Fatalf("data = %+v, err = %v", data, err)
	}
}

func TestInstallDLLVersionStagesAndValidatesBeforeCopy(t *testing.T) {
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("package/d3d11.dll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("dll")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := []byte(`{"version":"v1.2.3"}`)
		if strings.HasSuffix(request.URL.Path, ".zip") {
			body = archive.Bytes()
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(body)), Request: request,
		}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(infraClient)
	service := NewWithOptions(Options{HTTP: infraClient, Download: download, Archive: infra.NewArchive()})
	service.UseClient(newXXMITestClient(t))
	service.findProcess = noGameProcess
	useExternalLauncher(t, service, root)

	if err := service.InstallDLLVersion(context.Background(), InstallDLLVersionInput{Version: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "Resources", "Packages", "XXMI", "d3d11.dll"), "dll")
	manifest, err := os.ReadFile(filepath.Join(root, "Resources", "Packages", "XXMI", "Manifest.json"))
	if err != nil || !bytes.Contains(manifest, []byte("v1.2.3")) {
		t.Fatalf("manifest = %q, error = %v", manifest, err)
	}
	rawConfig, err := os.ReadFile(filepath.Join(root, xxmiConfigName))
	if err != nil || !bytes.Contains(rawConfig, []byte(`"auto_update": false`)) {
		t.Fatalf("config = %q, error = %v", rawConfig, err)
	}
}

func TestWaitForVisibleProcessRejectsHeadlessProcessUntilWindowAppears(t *testing.T) {
	t.Parallel()
	windowChecks := 0
	pid, err := waitForVisibleProcessWith(
		context.Background(),
		"Game.exe",
		time.Second,
		time.Millisecond,
		func(context.Context, string) (int, error) { return 4242, nil },
		func(gotPID int) bool {
			if gotPID != 4242 {
				t.Fatalf("window check pid = %d", gotPID)
			}
			windowChecks++
			return windowChecks >= 2
		},
	)
	if err != nil || pid != 4242 {
		t.Fatalf("waitForVisibleProcessWith = %d, %v", pid, err)
	}
	if windowChecks != 2 {
		t.Fatalf("window checks = %d, want 2", windowChecks)
	}
}

func TestWaitForVisibleProcessHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitForVisibleProcessWith(
		ctx,
		"Game.exe",
		time.Second,
		time.Millisecond,
		func(context.Context, string) (int, error) { return 0, nil },
		func(int) bool { return false },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForVisibleProcessWith error = %v, want context.Canceled", err)
	}
}

func TestUnsafeModeSignatureSupportsXXMIKeyTypes(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	username := current.Username
	if index := strings.LastIndexAny(username, `\/`); index >= 0 {
		username = username[index+1:]
	}
	digest := sha256.Sum256([]byte(username))

	for _, test := range []struct {
		name string
		key  crypto.Signer
	}{
		{name: "RSA", key: rsaKey},
		{name: "ECDSA P-384", key: ecdsaKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			security := filepath.Join(root, "Resources", "Security")
			if err := os.MkdirAll(security, 0o700); err != nil {
				t.Fatal(err)
			}
			der, err := x509.MarshalPKCS8PrivateKey(test.key)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(security, "private_key.der"),
				[]byte(base64.StdEncoding.EncodeToString(der)), 0o600); err != nil {
				t.Fatal(err)
			}

			encoded, err := unsafeModeSignature(root)
			if err != nil {
				t.Fatal(err)
			}
			signature, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			switch key := test.key.Public().(type) {
			case *rsa.PublicKey:
				if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
					t.Fatal(err)
				}
			case *ecdsa.PublicKey:
				if !ecdsa.VerifyASN1(key, digest[:], signature) {
					t.Fatal("invalid ECDSA SHA-256 signature")
				}
			}
		})
	}
}
