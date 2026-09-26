package app

import (
	"context"
	"testing"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

func TestStartDeepLinkDownloadRequiresServices(t *testing.T) {
	rt := &runtime{}

	if _, err := rt.startDeepLinkDownload(
		context.Background(),
		deepLinkDownload{Kind: "drive", ID: "abc"},
	); err == nil ||
		err.Error() != "auth service is not configured" {
		t.Fatalf("drive without auth = %v", err)
	}
	link := &drive.DownloadLink{LinkID: "link", Token: "token"}
	if _, err := rt.startDeepLinkDownload(
		context.Background(),
		deepLinkDownload{Kind: "link", ID: "abc", Link: link},
	); err == nil ||
		err.Error() != "drive service is not configured" {
		t.Fatalf("link without drive = %v", err)
	}
	mod := &drive.DownloadModAccess{Token: "token", Sig: "sig"}
	if _, err := rt.startDeepLinkDownload(
		context.Background(),
		deepLinkDownload{Kind: "mod", ID: "abc", Mod: mod},
	); err == nil ||
		err.Error() != "drive service is not configured" {
		t.Fatalf("mod without login = %v", err)
	}
}

func TestStartDeepLinkDownloadModCredentials(t *testing.T) {
	rt := &runtime{}
	tests := []struct {
		name    string
		access  drive.DownloadModAccess
		wantErr string
	}{
		{name: "no credentials", wantErr: "mod download requires a token or signature"},
		{
			name:    "token only",
			access:  drive.DownloadModAccess{Token: "token"},
			wantErr: "drive service is not configured",
		},
		{
			name:    "signature only",
			access:  drive.DownloadModAccess{Sig: "sig"},
			wantErr: "drive service is not configured",
		},
		{
			name:    "both credentials",
			access:  drive.DownloadModAccess{Token: "token", Sig: "sig"},
			wantErr: "drive service is not configured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := rt.startDeepLinkDownload(context.Background(), deepLinkDownload{
				Kind: "mod", ID: "abc", Mod: &tt.access,
			})
			if status != "" || err == nil || err.Error() != tt.wantErr {
				t.Fatalf("mod download = %q, %v; want empty status and %q", status, err, tt.wantErr)
			}
		})
	}
}

func TestStartDeepLinkDownloadUsesPathSelectorInsteadOfNativeDialog(t *testing.T) {
	transfers := transfer.New()
	service := drive.NewWithOptions(drive.Options{
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
