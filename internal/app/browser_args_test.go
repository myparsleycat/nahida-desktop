package app

import (
	"slices"
	"testing"
)

func TestWindowsApplicationOptionsMatchElectronBrowserPolicies(t *testing.T) {
	opts := windowsApplicationOptions()
	for _, argument := range []string{
		"--enable-experimental-web-platform-features",
		"--disable-renderer-backgrounding",
		"--autoplay-policy=no-user-gesture-required",
	} {
		if !slices.Contains(opts.AdditionalBrowserArgs, argument) {
			t.Fatalf("missing WebView2 browser argument %q in %v", argument, opts.AdditionalBrowserArgs)
		}
	}
	if !opts.DisableQuitOnLastWindowClosed {
		t.Fatal("Windows background lifecycle option was lost")
	}
}
