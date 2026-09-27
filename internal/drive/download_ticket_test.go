package drive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestRedeemModDownloadTicketAndUseGrant(t *testing.T) {
	const grant = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/akasha/mod/download-ticket/redeem":
			calls++
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
	if calls != 1 || redeemed.ItemID != "root" || redeemed.Name != "My Mod" || redeemed.Grant != grant {
		t.Fatalf("redemption = %+v, calls = %d", redeemed, calls)
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
