package fixinspection

import (
	"context"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func openToolsTestDB(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	if err := client.Reconcile(context.Background()); err != nil {
		_ = client.Close()
		t.Fatalf("Reconcile: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
