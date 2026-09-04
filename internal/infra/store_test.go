package infra_test

import (
	"context"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

func TestOpenStoreReconcilesAndServesSetting(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data.db")
	ctx := context.Background()
	store, err := infra.OpenStore(ctx, path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	s := setting.New(store.DB)
	got, err := s.Get(ctx, setting.KeyGeneralRunInBackground)
	if err != nil || got != true {
		t.Fatalf("Get = %#v %v, want true", got, err)
	}
}
