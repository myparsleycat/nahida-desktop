package github

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadFileSendsFileHeaders(t *testing.T) {
	const payload = "release payload"
	repo := Repo{Owner: "Moonholder", Name: "Wuwa_Mod_Fixer"}
	client := newTestClient(t, func(request *http.Request) (int, string) {
		if request.Header.Get("User-Agent") != fileUserAgent ||
			request.Header.Get("Referer") != "https://github.com/Moonholder/Wuwa_Mod_Fixer" {
			t.Errorf("headers = %v", request.Header)
		}
		return http.StatusOK, payload
	})
	destination := filepath.Join(t.TempDir(), "fixer.exe")

	err := client.DownloadFile(context.Background(), FileRequest{
		Repo: repo, URL: "https://example.test/fixer.exe", Destination: destination,
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(destination); err != nil || string(data) != payload {
		t.Fatalf("downloaded = %q, %v", data, err)
	}
}

func TestFetchFileReportsStatusAndLimit(t *testing.T) {
	repo := Repo{Owner: "SpectrumQT", Name: "XXMI-Libs-Package"}
	client := newTestClient(t, func(request *http.Request) (int, string) {
		if strings.HasSuffix(request.URL.Path, "/missing.json") {
			return http.StatusServiceUnavailable, "unavailable"
		}
		return http.StatusOK, `{"version":"v1"}`
	})

	data, err := client.FetchFile(context.Background(), repo, ReleaseFileURL(repo, "v1", "Manifest.json"), 1024)
	if err != nil || string(data) != `{"version":"v1"}` {
		t.Fatalf("data = %q, %v", data, err)
	}
	if _, err := client.FetchFile(
		context.Background(), repo, "https://example.test/missing.json", 1024,
	); err == nil || err.Error() != "HTTP 503" {
		t.Fatalf("status err = %v", err)
	}
	if _, err := client.FetchFile(context.Background(), repo, "https://example.test/a.json", 3); err == nil {
		t.Fatal("expected size limit error")
	}
}
