package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenFileBackedPragmasAndReopenDurability(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "local.db")
	ctx := context.Background()

	client, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	assertPragmas(t, client)

	want := "persisted-value"
	if err := client.Settings.Upsert(ctx, "reopen-key", &want); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := New(path)
	if err != nil {
		t.Fatalf("reopen New: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	assertPragmas(t, reopened)

	got, err := reopened.Settings.Get(ctx, "reopen-key")
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got == nil || got.Value == nil || *got.Value != want {
		t.Fatalf("row did not survive reopen: %+v", got)
	}
}

func assertPragmas(t *testing.T, client *Client) {
	t.Helper()

	var journal string
	if err := client.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if !strings.EqualFold(journal, "wal") {
		t.Fatalf("journal_mode = %q, want WAL", journal)
	}

	var foreignKeys int
	if err := client.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var busy int
	if err := client.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busy != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busy)
	}
}
