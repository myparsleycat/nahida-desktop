package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestConsumerSettingRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "consumer.db")
	client, err := db.New(path)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	want := "from-consumer"
	if err := client.Settings.Upsert(ctx, "consumer-key", &want); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := client.Settings.GetValue(ctx, "consumer-key")
	if err != nil {
		t.Fatalf("GetValue: %v", err)
	}
	if got == nil || *got != want {
		t.Fatalf("got %v, want %q", got, want)
	}
}
