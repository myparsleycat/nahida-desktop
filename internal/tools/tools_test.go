package tools

import (
	"context"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
)

func TestUseClientBindsWuwaFixer(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatalf("appdata.Open: %v", err)
	}
	service := New()
	service.UseAppData(data)
	service.UseClient(client)

	status, err := service.WuwaFixerGetStatus(ctx, nil)
	if err != nil {
		t.Fatalf("WuwaFixerGetStatus: %v", err)
	}
	if !status.Supported {
		t.Fatal("WuwaFixerGetStatus marked an importer-less game as unsupported")
	}
}
