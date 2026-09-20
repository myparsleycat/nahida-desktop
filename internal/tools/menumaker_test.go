package tools

import (
	"context"
	"errors"
	"testing"

	"nahida.live/desktop/internal/tools/menumaker"
)

func TestMenuMakerParseDelegates(t *testing.T) {
	_, err := New().MenuMakerParse(context.Background(), "")
	if !errors.Is(err, menumaker.ErrNoKeySections) {
		t.Fatalf("MenuMakerParse error = %v, want ErrNoKeySections", err)
	}
}
