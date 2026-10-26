package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

func TestStartDeepLinkDownloadRequiresModCredentials(t *testing.T) {
	rt := &runtime{}

	status, err := rt.startDeepLinkDownload(context.Background(), deepLinkDownload{
		Kind: "mod", ID: "abc", Mod: &drive.DownloadModAccess{},
	})
	if status != "" || err == nil || err.Error() != "mod download requires a token or signature" {
		t.Fatalf("mod download without credentials = %q, %v", status, err)
	}
}

func TestStartDeepLinkDownloadUsesPathSelectorInsteadOfNativeDialog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/akasha/mod/download-ticket/redeem" {
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(
			[]byte(`{"itemId":"root","name":"Real Mod Name","isDir":true,"grant":"` + strings.Repeat("x", 43) + `"}`),
		)
	}))
	defer server.Close()
	transfers := transfer.New()
	service := drive.NewWithOptions(drive.Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: server.Client(), BackendURL: server.URL, Status: infra.BackendOnline,
		}),
		FS:           platform.NewFS(),
		Transfer:     transfers,
		Download:     infra.NewDownload(),
		PathSelector: cancelPathSelector{},
	})
	rt := &runtime{drive: service}

	status, err := rt.startDeepLinkDownload(context.Background(), deepLinkDownload{
		Kind: "link",
		ID:   "file",
		Name: "file.bin",
		Link: &drive.DownloadLink{LinkID: "link", Token: "token"},
	})
	if err != nil || status != "canceled" {
		t.Fatalf("link download = %q, %v", status, err)
	}
	if got := transfers.List(); len(got) != 0 {
		t.Fatalf("transfers = %#v", got)
	}
	status, err = rt.startDeepLinkDownload(context.Background(), deepLinkDownload{Kind: "mod-ticket", Ticket: "ticket"})
	if err != nil || status != "canceled" {
		t.Fatalf("ticket canceled after redemption = %q, %v", status, err)
	}
}

type cancelPathSelector struct{}

func (cancelPathSelector) SelectDownloadPath(
	context.Context,
	string,
	string,
	[]string,
	bool,
) (*string, *string, error) {
	return nil, nil, nil
}
