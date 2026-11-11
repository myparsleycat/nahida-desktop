package app

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"nahida.live/desktop/internal/infra"
)

func TestTrayUpdateCheckConcurrentClicksPreserveProgress(t *testing.T) {
	t.Parallel()
	for _, outcome := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "failure", err: errors.New("check failed")},
		{name: "cancellation", err: context.Canceled},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				started, finish, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
				engine := &trayUpdaterEngine{check: func() (*infra.UpdateRelease, error) {
					close(started)
					<-finish
					return nil, outcome.err
				}}
				updater := infra.NewUpdater()
				if err := updater.Configure(
					infra.UpdaterOptions{Engine: engine, Settings: trayUpdaterSettings{}},
				); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = updater.ServiceShutdown() })
				rt := &runtime{updater: updater}
				go func() {
					rt.checkForUpdatesFromTray()
					close(done)
				}()
				<-started
				rt.observeUpdaterStatus(infra.UpdaterStatus{IsDownloading: true})
				rt.checkForUpdatesFromTray()
				rt.updateCheck.mu.Lock()
				preserved := rt.updateCheck.sawDownload && !rt.updateCheck.awaitingSettle
				rt.updateCheck.mu.Unlock()
				close(finish)
				<-done
				if !preserved {
					t.Fatal("concurrent click reset the active check's download progress")
				}

				calls := 0
				engine.check = func() (*infra.UpdateRelease, error) {
					calls++
					return nil, nil
				}
				rt.checkForUpdatesFromTray()
				if calls != 1 {
					t.Fatalf("check after %s called engine %d times, want 1", outcome.name, calls)
				}
			})
		})
	}
}

type trayUpdaterEngine struct {
	check func() (*infra.UpdateRelease, error)
}

func (e *trayUpdaterEngine) Check(context.Context, bool) (*infra.UpdateRelease, error) {
	return e.check()
}

func (*trayUpdaterEngine) DownloadAndInstall(context.Context, func(int64, int64)) error { return nil }

func (*trayUpdaterEngine) Restart(context.Context) error { return nil }

type trayUpdaterSettings struct{}

func (trayUpdaterSettings) GetAutoUpdateMode(context.Context) (string, error) { return "off", nil }

func (trayUpdaterSettings) GetIncludePrerelease(context.Context) (bool, error) { return false, nil }

func (trayUpdaterSettings) GetLanguage(context.Context) (string, error) { return "en", nil }
