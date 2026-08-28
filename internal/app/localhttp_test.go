package app

import (
	"context"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/transfer"
)

func TestLocalHTTPMessageValidationAndCompatibility(t *testing.T) {
	rt := &runtime{}

	response, err := rt.handleLocalHTTPMessage(context.Background(), []byte{0xff})
	if err == nil || response != "invalid data" {
		t.Fatalf("malformed CBOR = %q, %v", response, err)
	}

	payload, err := cbor.Marshal(map[string]any{"type": "unknown"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	response, err = rt.handleLocalHTTPMessage(context.Background(), payload)
	if err != nil || response != "download started" {
		t.Fatalf("unknown type = %q, %v", response, err)
	}

	payload, err = cbor.Marshal(map[string]any{"type": "hui", "title": "mod", "fileUrl": "https://example.test/file"})
	if err != nil {
		t.Fatalf("marshal hui: %v", err)
	}
	response, err = rt.handleLocalHTTPMessage(context.Background(), payload)
	if err == nil || response != "download error" || err.Error() != "hui download services are not configured" {
		t.Fatalf("hui gap = %q, %v", response, err)
	}

	payload, err = cbor.Marshal(map[string]any{"type": "hui", "title": "", "fileUrl": ""})
	if err != nil {
		t.Fatalf("marshal empty hui: %v", err)
	}
	response, err = rt.handleLocalHTTPMessage(context.Background(), payload)
	if err == nil || response != "download error" || err.Error() != "hui download services are not configured" {
		t.Fatalf("empty hui was rejected before dispatch = %q, %v", response, err)
	}

	payload, err = cbor.Marshal(map[string]any{
		"type": "live",
		"id":   "",
		"link": map[string]any{"linkId": "link", "token": "token"},
	})
	if err != nil {
		t.Fatalf("marshal empty live id: %v", err)
	}
	response, err = rt.handleLocalHTTPMessage(context.Background(), payload)
	if err == nil || response != "download error" || err.Error() != "drive service is not configured" {
		t.Fatalf("empty live id was rejected before dispatch = %q, %v", response, err)
	}

	for name, message := range map[string]map[string]any{
		"missing hui title": {"type": "hui", "fileUrl": ""},
		"missing live id":   {"type": "live", "link": map[string]any{"linkId": "link", "token": "token"}},
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := cbor.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			response, err := rt.handleLocalHTTPMessage(context.Background(), payload)
			if err == nil || response != "download error" {
				t.Fatalf("missing required field = %q, %v", response, err)
			}
		})
	}
}

func TestHandleLiveDownloadUsesPathSelectorInsteadOfNativeDialog(t *testing.T) {
	transfers := transfer.New()
	service := drive.NewWithOptions(drive.Options{
		FS:           platform.NewFS(),
		Transfer:     transfers,
		Download:     infra.NewDownload(),
		PathSelector: cancelPathSelector{},
	})
	rt := &runtime{drive: service}
	payload, err := cbor.Marshal(map[string]any{
		"type":          "live",
		"id":            "file",
		"isDir":         false,
		"suggestedName": "file.bin",
		"link":          map[string]any{"linkId": "link", "token": "token"},
	})
	if err != nil {
		t.Fatalf("marshal live: %v", err)
	}
	response, err := rt.handleLocalHTTPMessage(context.Background(), payload)
	if err != nil || response != "download canceled" {
		t.Fatalf("live = %q, %v", response, err)
	}
	if got := transfers.List(); len(got) != 0 {
		t.Fatalf("transfers = %#v", got)
	}
}

type cancelPathSelector struct{}

func (cancelPathSelector) SelectDownloadPath(context.Context, string, string, []string, bool) (*string, *string, error) {
	return nil, nil, nil
}
