package app

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/myparsleycat/fairy"
)

// modelViewerArgument returns the folder a --model-viewer launch names, which the installer's Explorer
// context menu passes, resolved against the launch's working directory.
func modelViewerArgument(args []string, workingDir string) string {
	for i := 1; i+1 < len(args); i++ {
		if args[i] != "--model-viewer" || strings.TrimSpace(args[i+1]) == "" || strings.HasPrefix(args[i+1], "--") ||
			strings.HasPrefix(strings.ToLower(args[i+1]), "nahida:") {
			continue
		}
		path := args[i+1]
		if !filepath.IsAbs(path) {
			path = filepath.Join(workingDir, path)
		}
		if absolute, err := filepath.Abs(path); err == nil {
			return filepath.Clean(absolute)
		}
	}
	return ""
}

func xxmiLaunchArgument(args []string) string {
	for index := 1; index+1 < len(args); index++ {
		if args[index] != "--xxmi-launch" {
			continue
		}
		switch strings.ToUpper(args[index+1]) {
		case "GIMI", "SRMI", "HIMI", "ZZMI", "WWMI", "EFMI":
			return strings.ToUpper(args[index+1])
		}
	}
	return ""
}

func newLaunchHandler(
	openViewer func(string),
	openMain func(),
	handleArguments func([]string),
	download func([]string),
	quickStart func(string, bool),
) func(fairy.Launch) {
	initial := true
	return func(launch fairy.Launch) {
		first := initial
		initial = false
		if path := modelViewerArgument(launch.Args, launch.Dir); path != "" {
			openViewer(path)
			return
		}
		if key := xxmiLaunchArgument(launch.Args); key != "" {
			quickStart(key, first)
			return
		}
		if first {
			openMain()
		} else {
			handleArguments(launch.Args)
		}
		download(launch.Args)
	}
}

// launchDispatcher serialises the cold launch and forwarded launches, including those arriving during backend
// boot.
type launchDispatcher struct {
	mu       sync.Mutex
	queue    []fairy.Launch
	handle   func(fairy.Launch)
	draining bool
}

func (d *launchDispatcher) Enqueue(launch fairy.Launch) {
	d.mu.Lock()
	d.queue = append(d.queue, launch)
	d.drain()
}

func (d *launchDispatcher) Start(initial fairy.Launch, handle func(fairy.Launch)) {
	d.mu.Lock()
	d.handle = handle
	d.queue = append([]fairy.Launch{initial}, d.queue...)
	d.drain()
}

// drain takes ownership of the locked mutex and releases it around handler calls.
func (d *launchDispatcher) drain() {
	if d.handle == nil || d.draining {
		d.mu.Unlock()
		return
	}
	d.draining = true
	for len(d.queue) > 0 {
		launch := d.queue[0]
		d.queue = d.queue[1:]
		handle := d.handle
		d.mu.Unlock()
		handle(launch)
		d.mu.Lock()
	}
	d.draining = false
	d.mu.Unlock()
}
