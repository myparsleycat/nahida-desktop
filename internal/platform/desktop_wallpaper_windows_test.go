//go:build windows

package platform

import (
	"image"
	"testing"
)

func TestCoversMonitor(t *testing.T) {
	t.Parallel()
	// A secondary monitor left of the primary one, with a taskbar along its bottom.
	work := image.Rect(-1920, 0, 0, 1040)
	for _, test := range []struct {
		name      string
		window    image.Rectangle
		maximized bool
		want      bool
	}{
		{name: "maximized", window: image.Rect(-10, -10, 10, 10), maximized: true, want: true},
		{name: "borderless full screen", window: image.Rect(-1920, 0, 0, 1080), want: true},
		{name: "nearly all of the work area", window: image.Rect(-1900, 0, 0, 1040), want: true},
		{name: "half of the work area", window: image.Rect(-1920, 0, -960, 1040), want: false},
		{name: "full screen on the next monitor", window: image.Rect(0, 0, 1920, 1080), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := coversMonitor(test.window, test.maximized, work); got != test.want {
				t.Fatalf("coversMonitor(%v, %t) = %t, want %t", test.window, test.maximized, got, test.want)
			}
		})
	}
}
