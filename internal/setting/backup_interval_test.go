package setting

import (
	"slices"
	"testing"
	"time"
)

func TestBackupIntervalDuration(t *testing.T) {
	t.Parallel()

	want := map[string]time.Duration{
		"6h":  6 * time.Hour,
		"12h": 12 * time.Hour,
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
	}
	for name, duration := range want {
		if !slices.Contains(BackupIntervals, name) {
			t.Errorf("%q is missing from available intervals", name)
		}
		got, ok := BackupIntervalDuration(name)
		if !ok || got != duration {
			t.Errorf("BackupIntervalDuration(%q) = %v, %t; want %v", name, got, ok, duration)
		}
	}
	if got, ok := BackupIntervalDuration("not-an-interval"); ok || got != 0 {
		t.Errorf("invalid interval = %v, %t", got, ok)
	}
}
