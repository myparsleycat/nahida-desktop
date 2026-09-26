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
