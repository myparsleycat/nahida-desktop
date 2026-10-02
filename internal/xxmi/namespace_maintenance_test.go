package xxmi

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCheckNamespaceGameStoppedRejectsRunningAndUnknownProcesses(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		pid     int
		failure error
	}{
		{name: "stopped"},
		{name: "externally launched game", pid: 123},
		{name: "process enumeration unavailable", failure: errors.New("access denied")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			x := New()
			x.UseClient(newXXMITestClient(t))
			useBuiltinLauncher(t, x)
			var queried []string
			x.findProcess = func(_ context.Context, name string) (int, error) {
				queried = append(queried, name)
				if strings.EqualFold(name, "Client-Win64-Shipping.exe") {
					return test.pid, test.failure
				}
				return 0, nil
			}
			err := x.CheckNamespaceGameStopped(t.Context(), "WWMI")
			if test.pid != 0 && !errors.Is(err, ErrNamespaceGameRunning) {
				t.Fatalf("running game = %v", err)
			}
			if test.failure != nil && !errors.Is(err, test.failure) {
				t.Fatalf("process failure = %v", err)
			}
			if test.pid == 0 && test.failure == nil && err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(queried, "Client-Win64-Shipping.exe") {
				t.Fatalf("shipping process was not queried: %v", queried)
			}
		})
	}
}

func TestWithNamespaceMutationExcludesLaunchAndReleasesGate(t *testing.T) {
	t.Parallel()
	x := New()
	failure := errors.New("transaction failed")
	err := x.WithNamespaceMutation(t.Context(), "wwmi", func() error {
		if err := x.StartGame(t.Context(), "WWMI"); err == nil || err.Error() != "XXMI_BUSY" {
			t.Fatalf("launch during transaction = %v", err)
		}
		return failure
	})
	if !errors.Is(err, failure) || !x.acquireImporter("WWMI") {
		t.Fatalf("transaction = %v, importer gate was not released", err)
	}
	x.releaseImporter("WWMI")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := x.WithNamespaceMutation(ctx, "WWMI", func() error {
		t.Fatal("canceled mutation ran")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNamespaceLaunchPreparationRunsUnderImporterGate(t *testing.T) {
	t.Parallel()
	x := New()
	failure := errors.New("unfinished namespace transaction")
	x.UseNamespaceLaunchPreparation(func(_ context.Context, key string) error {
		if key != "WWMI" || x.acquireImporter(key) {
			t.Fatal("launch preparation did not hold importer gate")
		}
		return failure
	})
	if err := x.StartGame(t.Context(), " wwmi "); !errors.Is(err, failure) {
		t.Fatalf("launch = %v", err)
	}
	if !x.acquireImporter("WWMI") {
		t.Fatal("launch failure retained importer gate")
	}
	x.releaseImporter("WWMI")
}
