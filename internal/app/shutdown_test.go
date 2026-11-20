package app

import (
	"bytes"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"nahida.live/desktop/internal/infra"
)

func closeTestRuntime(rt *runtime) {
	_ = rt.Close(infra.NewStallWatch(rt.log, "Runtime", "shutdown", nil))
}

type blockingWriter struct {
	out     io.Writer
	release <-chan struct{}
}

func (w blockingWriter) Write(p []byte) (int, error) {
	<-w.release
	return w.out.Write(p)
}

func TestCloseWithin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		hang      bool
		blockLog  bool
		wantExits int32
	}{
		{name: "close returns in time"},
		{name: "hung close", hang: true, wantExits: 1},
		{name: "hung close and log write", hang: true, blockLog: true, wantExits: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				var output bytes.Buffer
				release := make(chan struct{})
				var writer io.Writer = &output
				if tt.blockLog {
					writer = blockingWriter{out: &output, release: release}
				}
				log := infra.NewLogWithOptions(infra.LogOptions{Writer: writer, DisableFile: true})
				watch := infra.NewStallWatch(log, "Runtime", "shutdown", nil)
				var exits atomic.Int32
				done := make(chan error, 1)
				go func() {
					done <- closeWithin(func() error {
						watch.Stage("transfer")
						if tt.hang {
							<-release
						}
						watch.Stop()
						return nil
					}, watch, time.Second, time.Second, func() { exits.Add(1) })
				}()

				// Shorter than the watch's own stall warning, whose timer would wait on the mutex Expire holds.
				time.Sleep(10 * time.Second)
				if got := exits.Load(); got != tt.wantExits {
					t.Errorf("exit called %d times, want %d", got, tt.wantExits)
				}
				if tt.hang && !tt.blockLog {
					for _, want := range []string{`"stage":"transfer"`, `"status":"expired"`, "app.closeWithin"} {
						if !strings.Contains(output.String(), want) {
							t.Errorf("log lacks %s: %s", want, output.String())
						}
					}
				}

				close(release)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
