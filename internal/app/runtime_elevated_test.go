package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/xxmi/inject"
)

func TestElevatedLifecycleRequestRejectsInvalidatedRead(t *testing.T) {
	t.Parallel()

	t.Run("disable during read", func(t *testing.T) {
		t.Parallel()

		lifecycle := newElevatedLifecycle(nil, nil, nil)
		lifecycle.request(func() (bool, bool) {
			lifecycle.cancelStartup()
			return true, true
		})
		if lifecycle.started {
			t.Fatal("request enqueued a read that a disable invalidated")
		}
	})

	t.Run("shutdown during read", func(t *testing.T) {
		t.Parallel()

		lifecycle := newElevatedLifecycle(nil, nil, nil)
		lifecycle.request(func() (bool, bool) {
			lifecycle.shutdown()
			return true, true
		})
		if lifecycle.started {
			t.Fatal("request enqueued a read that shutdown invalidated")
		}
	})
}

func TestElevatedLifecycleStartFailureKeepsEnabled(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{startErr: errors.New("uac declined")}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.setEnabled(true)
	got := waitHelperStatus(t, statuses)
	if !got.Enabled || got.Running {
		t.Fatalf("status = %+v, want enabled and not running", got)
	}
	if stub.starts != 1 {
		t.Fatalf("starts = %d, want 1", stub.starts)
	}
}

func TestElevatedLifecycleStartSuccessReportsRunning(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.setEnabled(true)
	got := waitHelperStatus(t, statuses)
	if !got.Enabled || !got.Running {
		t.Fatalf("status = %+v, want enabled and running", got)
	}
}

func TestElevatedLifecycleAcquireClosesTemporaryHelper(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{}
	lifecycle := newElevatedLifecycle(stub, nil, nil)
	defer lifecycle.shutdown()

	release, err := lifecycle.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !stub.Connected() {
		t.Fatal("temporary helper did not start")
	}
	release()
	if stub.Connected() {
		t.Fatal("temporary helper remained connected")
	}
}

func TestElevatedLifecycleDisableCancelsInFlightStart(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	stub := &stubElevatedClient{
		startFn: func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
	}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.setEnabled(true)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for helper start")
	}
	lifecycle.setEnabled(false)
	got := waitHelperStatus(t, statuses)
	if got.Enabled || got.Running {
		t.Fatalf("status = %+v, want disabled", got)
	}
	if gotStarts := stub.startCount(); gotStarts != 1 {
		t.Fatalf("starts = %d, want 1", gotStarts)
	}
	if stub.closes != 1 {
		t.Fatalf("closes = %d, want 1", stub.closes)
	}
}

func TestElevatedLifecycleDisableClearsStatus(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.setEnabled(true)
	_ = waitHelperStatus(t, statuses)

	lifecycle.setEnabled(false)
	got := waitHelperStatus(t, statuses)
	if got.Enabled || got.Running {
		t.Fatalf("status = %+v, want disabled", got)
	}
	if stub.closes != 1 {
		t.Fatalf("closes = %d, want 1", stub.closes)
	}
}

func TestElevatedLifecycleCoalescesDuplicateStartWhileStarting(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	stub := &stubElevatedClient{
		startFn: func(ctx context.Context) error {
			close(started)
			select {
			case <-release:
				return errors.New("uac declined")
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.setEnabled(true)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for helper start")
	}
	lifecycle.setEnabled(true)
	close(release)

	got := waitHelperStatus(t, statuses)
	if !got.Enabled || got.Running {
		t.Fatalf("status = %+v, want enabled and not running", got)
	}
	if gotStarts := stub.startCount(); gotStarts != 1 {
		t.Fatalf("starts = %d, want 1", gotStarts)
	}
}

func TestElevatedLifecycleRetryAfterStartFailureStartsAgain(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{startErr: errors.New("uac declined")}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.setEnabled(true)
	_ = waitHelperStatus(t, statuses)
	lifecycle.setEnabled(true)
	got := waitHelperStatus(t, statuses)
	if !got.Enabled || got.Running {
		t.Fatalf("status = %+v, want enabled and not running after retry", got)
	}
	if gotStarts := stub.startCount(); gotStarts != 2 {
		t.Fatalf("starts = %d, want 2", gotStarts)
	}
}

func TestElevatedLifecycleSetEnabledTrueRestartsDisconnectedHelper(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.setEnabled(true)
	_ = waitHelperStatus(t, statuses)

	stub.setConnected(false)
	lifecycle.setEnabled(true)
	got := waitHelperStatus(t, statuses)
	if !got.Enabled || !got.Running {
		t.Fatalf("status = %+v, want enabled and running after restart", got)
	}
	if stub.starts != 2 {
		t.Fatalf("starts = %d, want 2", stub.starts)
	}
}

func TestElevatedLifecycleCheckHealthPublishesWhenDisconnected(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{}
	statuses := make(chan platform.ElevatedHelperStatus, 4)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.mu.Lock()
	lifecycle.desired = true
	lifecycle.mu.Unlock()
	lifecycle.checkHealth()

	got := waitHelperStatus(t, statuses)
	if !got.Enabled || got.Running {
		t.Fatalf("status = %+v, want enabled and not running", got)
	}
}

func TestElevatedLifecycleCheckHealthDoesNotPublishWhenConnected(t *testing.T) {
	t.Parallel()

	stub := &stubElevatedClient{}
	stub.setConnected(true)
	statuses := make(chan platform.ElevatedHelperStatus, 1)
	lifecycle := newElevatedLifecycle(stub, nil, statusEmitter(statuses))
	defer lifecycle.shutdown()

	lifecycle.mu.Lock()
	lifecycle.desired = true
	lifecycle.mu.Unlock()
	lifecycle.checkHealth()

	select {
	case status := <-statuses:
		t.Fatalf("published %+v while connected", status)
	default:
	}
}

func TestElevatedLifecycleStatusTreatsInFlightStartAsRunning(t *testing.T) {
	t.Parallel()

	lifecycle := newElevatedLifecycle(&stubElevatedClient{}, nil, nil)
	lifecycle.mu.Lock()
	lifecycle.desired = true
	lifecycle.mu.Unlock()
	lifecycle.starting.Store(true)

	got := lifecycle.status()
	if !got.Enabled || got.Running {
		t.Fatalf("status = %+v, want enabled and not running while starting", got)
	}
}

type stubElevatedClient struct {
	mu        sync.Mutex
	startErr  error
	startFn   func(context.Context) error
	connected bool
	starts    int
	closes    int
}

func (s *stubElevatedClient) Start(ctx context.Context) error {
	s.mu.Lock()
	s.starts++
	startFn := s.startFn
	startErr := s.startErr
	s.mu.Unlock()
	if startFn != nil {
		return startFn(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if startErr != nil {
		s.connected = false
		return startErr
	}
	s.connected = true
	return nil
}

func (s *stubElevatedClient) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closes++
	s.connected = false
	return nil
}

func (s *stubElevatedClient) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *stubElevatedClient) setConnected(connected bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = connected
}

func (s *stubElevatedClient) startCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starts
}

func TestXXMILauncherShutdownCancelsLaunch(t *testing.T) {
	t.Parallel()

	lifecycle := newElevatedLifecycle(nil, nil, nil)
	defer lifecycle.shutdown()
	client := &blockingLaunchClient{started: make(chan struct{})}
	launcher := xxmiElevatedLauncher{lifecycle: lifecycle, client: client}
	launchErr := make(chan error, 1)
	go func() {
		_, err := launcher.LaunchXXMI(context.Background(), inject.LaunchSpec{TimeoutSeconds: 600})
		launchErr <- err
	}()
	select {
	case <-client.started:
	case <-time.After(2 * time.Second):
		t.Fatal("launch did not start")
	}

	lifecycle.shutdown()
	select {
	case err := <-launchErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("LaunchXXMI = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown left the launch running")
	}
}

type blockingLaunchClient struct {
	started chan struct{}
}

func (b *blockingLaunchClient) LaunchXXMI(ctx context.Context, _ inject.LaunchSpec) (inject.LaunchResult, error) {
	close(b.started)
	<-ctx.Done()
	return inject.LaunchResult{}, ctx.Err()
}

func (b *blockingLaunchClient) HelperImageName() string { return "" }

func statusEmitter(statuses chan<- platform.ElevatedHelperStatus) func(string, ...any) {
	return func(name string, data ...any) {
		if name != elevatedStatusEvent || len(data) != 1 {
			return
		}
		status, ok := data[0].(platform.ElevatedHelperStatus)
		if !ok {
			return
		}
		statuses <- status
	}
}

func waitHelperStatus(t *testing.T, statuses <-chan platform.ElevatedHelperStatus) platform.ElevatedHelperStatus {
	t.Helper()
	select {
	case status := <-statuses:
		return status
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for elevated helper status")
		return platform.ElevatedHelperStatus{}
	}
}
