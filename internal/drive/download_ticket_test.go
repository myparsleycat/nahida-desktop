package drive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

func TestRedeemModDownloadTicketAndUseGrant(t *testing.T) {
	const grant = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/akasha/mod/download-ticket/redeem":
			calls.Add(1)
			if r.Method != http.MethodPost || r.URL.Query().Get("ticket") != "" {
				t.Errorf("unexpected redemption request: %s %s", r.Method, r.URL)
			}
			var input map[string]string
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			if input["ticket"] != "short-lived" {
				t.Errorf("ticket body = %v", input)
			}
			_, _ = w.Write(
				[]byte(`{"itemId":"root","name":"My Mod","isDir":true,"grant":"` + grant + `","expiresIn":86400}`),
			)
		case "/akasha/mod/download/root":
			if r.Header.Get("x-mod-download-grant") != grant || r.Header.Get("x-token") != "" ||
				r.Header.Get("x-sig") != "" {
				t.Errorf("metadata headers = %v", r.Header)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write(
				[]byte(
					"event: metadata\ndata: {\"root\":{\"id\":\"root\",\"name\":\"My Mod\"},\"totalBytes\":0}\n\nevent: complete\ndata: {}\n\n",
				),
			)
		case "/akasha/mod/file/download":
			if r.Header.Get("x-mod-download-grant") != grant || r.Header.Get("x-token") != "" ||
				r.Header.Get("x-sig") != "" {
				t.Errorf("presign headers = %v", r.Header)
			}
			_, _ = w.Write([]byte(`{"url":"https://example.test/signed"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline,
	})})
	redeemed, err := d.redeemModDownloadTicket(context.Background(), "short-lived")
	if err != nil {
		t.Fatal(err)
	}
	count := calls.Load()
	if count != 1 || redeemed.ItemID != "root" || redeemed.Name != "My Mod" || redeemed.Grant != grant {
		t.Fatalf("redemption = %+v, calls = %d", redeemed, count)
	}
	if _, err := d.fetchModDownloadMetadata(
		context.Background(),
		[]DownloadItem{{ID: redeemed.ItemID, IsDir: true}},
		DownloadModAccess{Grant: grant},
	); err != nil {
		t.Fatal(err)
	}
	url, err := d.fetchPresignedDownloadURL(
		context.Background(),
		"child",
		downloadAccess{Mod: &DownloadModAccess{Grant: grant}},
	)
	if err != nil || url != "https://example.test/signed" {
		t.Fatalf("presign = %q, %v", url, err)
	}
}

func TestStartDownloadRedeemsTicketBeforePathSelection(t *testing.T) {
	const grant = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var redemptions, selections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/akasha/mod/download-ticket/redeem" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		redemptions.Add(1)
		_, _ = w.Write([]byte(`{"itemId":"root","name":"Real Mod Name","isDir":true,"grant":"` + grant + `"}`))
	}))
	defer server.Close()
	d := NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline,
		}),
		FS: platform.NewFS(), Transfer: transfer.New(), Download: infra.NewDownload(),
		PathSelector: ticketPathSelectorFunc(func(_ context.Context, name, _ string, names []string, selectFile bool) (*string, *string, error) {
			selections.Add(1)
			if name != "Real Mod Name" || len(names) != 1 || names[0] != name || selectFile {
				t.Errorf("selector got name %q, names %q, selectFile %v", name, names, selectFile)
			}
			return nil, nil, nil
		}),
	})
	result, err := d.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{Name: "Akasha Mod", IsDir: true}}, ModTicket: "ticket",
	})
	if err != nil || result.Status != "canceled" {
		t.Fatalf("download = %+v, %v", result, err)
	}
	if redemptions.Load() != 1 || selections.Load() != 1 {
		t.Fatalf("redemptions = %d, selections = %d", redemptions.Load(), selections.Load())
	}
}

func TestStartDownloadRejectsInvalidTicketBeforePathSelection(t *testing.T) {
	var selections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid_download_capability", http.StatusUnauthorized)
	}))
	defer server.Close()
	d := NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline,
		}),
		FS: platform.NewFS(), Transfer: transfer.New(), Download: infra.NewDownload(),
		PathSelector: ticketPathSelectorFunc(func(context.Context, string, string, []string, bool) (*string, *string, error) {
			selections.Add(1)
			return nil, nil, nil
		}),
	})
	_, err := d.StartDownload(context.Background(), StartDownloadParams{
		Items: []DownloadItem{{Name: "Akasha Mod", IsDir: true}}, ModTicket: "invalid",
	})
	var redemptionErr *ModTicketRedemptionError
	if !errors.As(err, &redemptionErr) || selections.Load() != 0 {
		t.Fatalf("error = %v, selections = %d", err, selections.Load())
	}
}

type ticketPathSelectorFunc func(context.Context, string, string, []string, bool) (*string, *string, error)

func (f ticketPathSelectorFunc) SelectDownloadPath(
	ctx context.Context, name, source string, names []string, selectFile bool,
) (*string, *string, error) {
	return f(ctx, name, source, names, selectFile)
}

func TestStartDownloadMarksOnlyTicketRedemptionFailures(t *testing.T) {
	var rejectTicket atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/akasha/mod/download-ticket/redeem":
			if rejectTicket.Load() {
				http.Error(w, "ticket unavailable", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(
				[]byte(`{"itemId":"root","name":"Mod","isDir":true,"grant":"` + strings.Repeat("x", 43) + `"}`),
			)
		default:
			http.Error(w, "metadata unavailable", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	d := NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline,
		}),
		FS: platform.NewFS(), Transfer: transfer.New(), Download: infra.NewDownload(),
	})
	params := StartDownloadParams{Items: []DownloadItem{{Name: "Mod"}}, ModTicket: "ticket"}

	params.TargetPath = t.TempDir() + "/missing"
	_, err := d.StartDownload(context.Background(), params)
	var redemptionErr *ModTicketRedemptionError
	if err == nil || errors.As(err, &redemptionErr) {
		t.Fatalf("destination error = %v; want non-redemption error", err)
	}

	params.TargetPath = t.TempDir()
	_, err = d.StartDownload(context.Background(), params)
	if err == nil || errors.As(err, &redemptionErr) {
		t.Fatalf("metadata error = %v; want non-redemption error", err)
	}

	rejectTicket.Store(true)
	_, err = d.StartDownload(context.Background(), params)
	if err == nil || !errors.As(err, &redemptionErr) {
		t.Fatalf("ticket error = %v; want redemption error", err)
	}
}

func TestRedeemModDownloadTicketRejectsBadResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"itemId":"root","name":"Mod","isDir":false,"grant":"` + strings.Repeat("x", 43) + `"}`))
	}))
	defer server.Close()
	d := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline,
	})})
	if _, err := d.redeemModDownloadTicket(context.Background(), "ticket"); err == nil {
		t.Fatal("invalid redemption accepted")
	}
}
