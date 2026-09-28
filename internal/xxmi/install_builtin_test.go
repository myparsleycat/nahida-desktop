package xxmi

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
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

func TestInstallBuiltinImporterPreservesModsAndExternalConfig(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	externalConfig, err := os.ReadFile(filepath.Join(root, xxmiConfigName))
	if err != nil {
		t.Fatal(err)
	}
	importerFolder := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(filepath.Join(importerFolder, "Mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"d3dx.ini": "user ini", "Mods/user.ini": "user mod"} {
		if err := os.WriteFile(filepath.Join(importerFolder, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	zipBody := writeXXMITestZip(t, map[string]string{
		"Core/GIMI/main.ini": "global $version = 1.23\n", "d3dx.ini": "package ini",
		"Mods/package.ini": "package mod", "ShaderFixes/new.hlsl": "shader",
	})
	digest := sha256.Sum256(zipBody)
	assetURL := "https://github.com/SilentNightSound/GIMI-Package/releases/download/v1.2.3/GIMI-PACKAGE-v1.2.3.zip"
	releases := fmt.Sprintf(
		`[ {"tag_name":"v1.2.3","body":"## Changes\nunsigned", "assets":[{"name":"GIMI-PACKAGE-v1.2.3.zip","browser_download_url":%q,"digest":"sha256:%s"}]} ]`,
		assetURL,
		hex.EncodeToString(digest[:]),
	)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch request.URL.String() {
		case "https://api.github.com/repos/SilentNightSound/GIMI-Package/releases":
			body = []byte(releases)
		case assetURL:
			body = zipBody
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(infraClient)
	service := NewWithOptions(Options{HTTP: infraClient, Download: download, Archive: infra.NewArchive()})
	service.UseClient(client)
	if err := service.EnableImporter(ctx, "GIMI", importerFolder); err != nil {
		t.Fatal(err)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	cfg.OverwriteINI = false
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	input := InstallImporterPackageInput{Importer: "GIMI", Version: "v1.2.3"}
	if err := service.InstallImporterPackage(
		ctx,
		input,
	); err == nil ||
		!strings.Contains(err.Error(), "XXMI_UNSIGNED_RELEASE") {
		t.Fatalf("unsigned release accepted without confirmation: %v", err)
	}
	input.AllowUnsigned = true
	if err := service.InstallImporterPackage(ctx, input); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(importerFolder, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(importerFolder, "d3dx.ini"), "user ini")
	assertFile(t, filepath.Join(importerFolder, "ShaderFixes", "new.hlsl"), "shader")
	if _, err := os.Stat(filepath.Join(importerFolder, "Mods", "package.ini")); !os.IsNotExist(err) {
		t.Fatalf("package mod copied: %v", err)
	}
	gotConfig, err := os.ReadFile(filepath.Join(root, xxmiConfigName))
	if err != nil || !bytes.Equal(gotConfig, externalConfig) {
		t.Fatalf("external config changed: %v", err)
	}
}
