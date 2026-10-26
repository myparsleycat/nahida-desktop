//go:build windows

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// The helper runs elevated from a folder its user can write to. Package initializers run before main
// restricts DLL loading, so a dependency whose initializer loads a library by name, as the Wails UI packages
// do, would load a DLL planted beside the helper. New dependencies have to be reviewed for that first.
var allowedHelperDependencies = []string{
	"nahida.live/desktop/cmd/nahida-elevated-helper",
	"nahida.live/desktop/internal/appdata",
	"nahida.live/desktop/internal/diskio",
	"nahida.live/desktop/internal/elevated",
	"nahida.live/desktop/internal/platform",
	"nahida.live/desktop/internal/xxmi/inject",
	"golang.org/x/sys/",
	"golang.org/x/sync/semaphore",
	"github.com/Microsoft/go-winio",
	"github.com/rodrigocfd/windigo/",
}

func TestHelperDependenciesAreReviewed(t *testing.T) {
	output, err := exec.CommandContext(
		t.Context(), "go", "list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", ".",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	for dependency := range strings.FieldsSeq(string(output)) {
		if !allowedHelperDependency(dependency) {
			t.Errorf("elevated helper depends on %s, which is not in the reviewed list", dependency)
		}
	}
}

func allowedHelperDependency(dependency string) bool {
	for _, allowed := range allowedHelperDependencies {
		if dependency == allowed || strings.HasPrefix(dependency, strings.TrimSuffix(allowed, "/")+"/") {
			return true
		}
	}
	return false
}
