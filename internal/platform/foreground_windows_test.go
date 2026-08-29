//go:build windows

package platform

import "testing"

func TestForceForegroundWindowIgnoresZeroHandle(t *testing.T) {
	t.Parallel()
	ForceForegroundWindow(0)
}
