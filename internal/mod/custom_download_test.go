package mod

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/gamebanana"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

func TestValidateDownloadURLMatchesElectronErrors(t *testing.T) {
	for _, test := range []struct {
		url     string
		wantErr string
	}{
		{url: "https://example.com/file.zip"},
		{url: "HTTP://example.com/file.zip"},
		{url: "://broken", wantErr: "INVALID_DOWNLOAD_URL"},
		{url: "https://", wantErr: "INVALID_DOWNLOAD_URL"},
		{url: "relative/file.zip", wantErr: "INVALID_DOWNLOAD_URL"},
		{url: "ftp://example.com/file.zip", wantErr: "UNSUPPORTED_DOWNLOAD_URL_PROTOCOL"},
	} {
		err := validateDownloadURL(test.url)
		if test.wantErr == "" && err != nil {
			t.Fatalf("validateDownloadURL(%q) = %v", test.url, err)
		}
		if test.wantErr != "" && (err == nil || err.Error() != test.wantErr) {
			t.Fatalf("validateDownloadURL(%q) = %v, want %q", test.url, err, test.wantErr)
		}
	}
}

func TestValidateHuiHeadRejectsNonOKResponse(t *testing.T) {
	if err := validateHuiHead(downloadHead{ok: true, status: 200, statusText: "OK"}); err != nil {
		t.Fatal(err)
	}
	err := validateHuiHead(downloadHead{status: 404, statusText: "Not Found"})
	if err == nil || err.Error() != "Failed to get real file URL: Not Found" {
		t.Fatalf("validateHuiHead = %v", err)
	}
}

func TestCustomDownloadPublicRunnersEndToEnd(t *testing.T) {
	customArchive := nteBootstrapZip(t, map[string]string{"CustomRoot/mod.ini": "custom"})
	gameBananaArchive := nteBootstrapZip(t, map[string]string{"OriginalGB/mod.ini": "gamebanana"})
	huiArchive := nteBootstrapZip(t, map[string]string{"OriginalHui/mod.ini": "hui"})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apiv13/Mod/10/ProfilePage":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"_idRow":10,"_sName":"GB Mod","_sProfileUrl":"%s/mods/10","_aSubmitter":{"_sName":"author","_sProfileUrl":"%s/member/1"},"_aGame":{"_idRow":8552,"_sName":"Game"},"_aCategory":{"_sName":"Characters"},"_aFiles":[{"_idRow":20,"_sFile":"gb.zip","_tsDateAdded":1,"_nDownloadCount":1,"_sDownloadUrl":"%s/gb.zip","_sMd5Checksum":"abc","_sVersion":"1.0"}]}`, server.URL, server.URL, server.URL)
		case "/custom.zip":
			serveCustomDownloadFixture(w, request, customArchive, "custom.zip", true)
		case "/gb.zip":
			serveCustomDownloadFixture(w, request, gameBananaArchive, "gb.zip", true)
		case "/hui.zip":
			serveCustomDownloadFixture(w, request, huiArchive, "hui.zip", false)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	t.Run("custom URL", func(t *testing.T) {
		destination := t.TempDir()
		service, transfers := customDownloadTestService(t, server, destination, "", false)
		status, err := service.DownloadFromURL(context.Background(), server.URL+"/custom.zip", destination)
		if err != nil || status != "started" {
			t.Fatalf("DownloadFromURL = %q, %v", status, err)
		}
		processCustomDownloadQueue(t, transfers, int64(len(customArchive)))
		assertCustomDownloadFile(t, filepath.Join(destination, "CustomRoot", "mod.ini"), "custom")
	})

	t.Run("GameBanana", func(t *testing.T) {
		destination := t.TempDir()
		service, transfers := customDownloadTestService(t, server, destination, "Selected GB", true)
		status, err := service.DownloadGameBananaFile(context.Background(), GameBananaDownloadProps{ItemID: 10, FileID: 20})
		if err != nil || status != "started" {
			t.Fatalf("DownloadGameBananaFile = %q, %v", status, err)
		}
		processCustomDownloadQueue(t, transfers, int64(len(gameBananaArchive)))
		modPath := filepath.Join(destination, "Selected GB")
		assertCustomDownloadFile(t, filepath.Join(modPath, "mod.ini"), "gamebanana")
		raw, err := os.ReadFile(filepath.Join(modPath, modDownloadMetadataFileName))
		if err != nil {
			t.Fatal(err)
		}
		var metadata map[string]any
		if err := json.Unmarshal(raw, &metadata); err != nil || metadata["source"] != "gamebanana" {
			t.Fatalf("metadata = %s, %v", raw, err)
		}
	})

	t.Run("Hui", func(t *testing.T) {
		destination := t.TempDir()
		service, transfers := customDownloadTestService(t, server, destination, "Selected Hui", false)
		status, err := service.HuiDownload(context.Background(), "Hui Package", server.URL+"/hui.zip")
		if err != nil || status != "started" {
			t.Fatalf("HuiDownload = %q, %v", status, err)
		}
		processCustomDownloadQueue(t, transfers, int64(len(huiArchive)))
		assertCustomDownloadFile(t, filepath.Join(destination, "Selected Hui", "mod.ini"), "hui")
	})
}

func TestCanceledCustomDownloadCannotBeResumedOrRetried(t *testing.T) {
	requestStarted := make(chan struct{})
	var getRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="custom.bin"`)
		if request.Method == http.MethodHead {
			return
		}
		getRequests.Add(1)
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	destination := t.TempDir()
	service, transfers := customDownloadTestService(t, server, destination, "", false)
	status, err := service.DownloadFromURL(context.Background(), server.URL+"/custom.bin", destination)
	if err != nil || status != "started" {
		t.Fatalf("DownloadFromURL = %q, %v", status, err)
	}
	records := transfers.List()
	if len(records) != 1 {
		t.Fatalf("transfers = %#v", records)
	}
	pid := records[0].PID
	done := make(chan error, 1)
	go func() { done <- transfers.ProcessQueue(context.Background()) }()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("custom download did not start")
	}
	if err := transfers.Cancel(pid); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("custom download did not stop")
	}
	if err := transfers.Resume(pid); err != nil {
		t.Fatal(err)
	}
	if err := transfers.Retry(pid); err != nil {
		t.Fatal(err)
	}
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	record, _ := transfers.Get(pid)
	if record.Status != transfer.StatusCanceled || getRequests.Load() != 1 {
		t.Fatalf("transfer = %#v, GET requests = %d", record.Snapshot, getRequests.Load())
	}
}

func serveCustomDownloadFixture(w http.ResponseWriter, request *http.Request, payload []byte, name string, knownSize bool) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if knownSize {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	}
	if request.Method != http.MethodHead {
		_, _ = w.Write(payload)
	}
}

func customDownloadTestService(t *testing.T, server *httptest.Server, destination, selectedName string, withGameBanana bool) (*Mod, *transfer.Transfer) {
	t.Helper()
	httpClient := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline,
	})
	transfers := transfer.New()
	var service *Mod
	emit := func(name string, data ...any) {
		if name != "pathSelector:modeSelect" || len(data) == 0 {
			return
		}
		payload, _ := data[0].(map[string]any)
		selectionID, _ := payload["selectionId"].(string)
		var fileName *string
		if selectedName != "" {
			fileName = &selectedName
		}
		_ = service.SelectModManagerPath(context.Background(), selectionID, destination, fileName)
	}
	var gameBananaService *gamebanana.GameBanana
	if withGameBanana {
		gameBananaService = gamebanana.NewWithOptions(gamebanana.Options{
			HTTP: httpClient, BaseURL: server.URL + "/apiv13", SiteURL: server.URL,
		})
	}
	service = NewWithOptions(Options{
		FS: platform.NewFS(), Settings: testSettings{}, Archive: infra.NewArchive(), HTTP: httpClient,
		Transfer: transfers, GameBanana: gameBananaService, EventEmit: emit,
	})
	return service, transfers
}

func processCustomDownloadQueue(t *testing.T, transfers *transfer.Transfer, wantBytes int64) {
	t.Helper()
	if err := transfers.ProcessQueue(context.Background()); err != nil {
		t.Fatal(err)
	}
	records := transfers.List()
	if len(records) != 1 {
		t.Fatalf("transfers = %#v", records)
	}
	record, ok := transfers.Get(records[0].PID)
	if !ok || record.Status != transfer.StatusCompleted || record.Progress != 100 || record.TransferredFiles != 1 || record.TransferredSize != wantBytes {
		t.Fatalf("transfer = %#v, exists=%v", record.Snapshot, ok)
	}
}

func assertCustomDownloadFile(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("downloaded file %s = %q, %v", path, raw, err)
	}
}
