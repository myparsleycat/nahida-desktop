package setting_test

import (
	"context"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/setting"
)

func TestConsumerOpenGetSet(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data.db")
	ctx := context.Background()
	s, err := setting.Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Set(ctx, setting.KeyGeneralLogLevel, "info"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get(ctx, setting.KeyGeneralLogLevel)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "info" {
		t.Fatalf("Get = %#v, want info", got)
	}
}
