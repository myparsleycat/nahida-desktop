package ui

import (
	"context"
	"testing"
	"testing/synctest"
)

func TestBackupActionWaitsForStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		startup, cancel := context.WithCancel(t.Context())
		defer cancel()
		shell := &Shell{deps: Deps{Ready: func(context.Context) error {
			<-startup.Done()
			return startup.Err()
		}}}
		called := false
		shell.backupAction("run", nil, func(context.Context) error {
			called = true
			return nil
		})
		synctest.Wait()
		if called {
			t.Error("backup ran while startup maintenance was pending")
		}

		cancel()
		synctest.Wait()
		if called {
			t.Error("backup ran after startup maintenance was canceled")
		}
	})
}
