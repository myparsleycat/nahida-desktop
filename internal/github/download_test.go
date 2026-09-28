package github

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
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

func TestDownloadFileReportsReceivedAndTotalBytes(t *testing.T) {
	const body = "package"
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			ContentLength: int64(len(body)), Body: io.NopCloser(strings.NewReader(body)), Request: request,
		}, nil
	})}
	client := New(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: httpClient, Status: infra.BackendOnline,
	})})
	updates := [][2]int64{}
	err := client.DownloadFile(context.Background(), FileRequest{
		Repo:        Repo{Owner: "SpectrumQT", Name: "XXMI-Libs-Package"},
		URL:         "https://github.com/SpectrumQT/XXMI-Libs-Package/releases/download/v1/test.zip",
		Destination: filepath.Join(t.TempDir(), "test.zip"),
		Progress: func(downloaded, total int64) {
			updates = append(updates, [2]int64{downloaded, total})
		},
	})
	if err != nil || len(updates) < 2 || updates[0] != [2]int64{0, int64(len(body))} ||
		updates[len(updates)-1] != [2]int64{int64(len(body)), int64(len(body))} {
		t.Fatalf("updates = %v, err = %v", updates, err)
	}
}
