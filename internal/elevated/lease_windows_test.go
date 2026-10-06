//go:build windows

package elevated

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/platform"
)

// recordingGateway stands in for the helper lifecycle and counts how often it is asked to start.
type recordingGateway struct {
	acquires   int
	releases   int
	acquireErr error
	applyErr   error
	// applyErrs answers the first requests in order, ahead of applyErr.
	applyErrs []error
	batches   [][]FileOp
}

func (g *recordingGateway) Acquire(context.Context) (func(), error) {
	g.acquires++
	if g.acquireErr != nil {
		return nil, g.acquireErr
	}
	return func() { g.releases++ }, nil
}

func (g *recordingGateway) ApplyFiles(_ context.Context, ops []FileOp) error {
	g.batches = append(g.batches, ops)
	if len(g.applyErrs) > 0 {
		err := g.applyErrs[0]
		g.applyErrs = g.applyErrs[1:]
		return err
	}
	return g.applyErr
}

func TestNewCopyOpBindsSourceDigest(t *testing.T) {
	t.Parallel()

	source, digest := stagedFile(t, "new settings")
	target := filepath.Join(t.TempDir(), "Engine.ini")
	op, err := NewCopyOp(source, target)
	if err != nil {
		t.Fatal(err)
	}
	if want := (FileOp{Kind: FileOpCopy, Source: source, Target: target, SHA256: digest}); op != want {
		t.Fatalf("NewCopyOp = %+v, want %+v", op, want)
	}
	if err := validateFileOp(op); err != nil {
		t.Fatalf("helper would refuse the operation: %v", err)
	}

	if _, err := NewCopyOp(filepath.Join(t.TempDir(), "missing.bin"), target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source error = %v", err)
	}
}

func TestFileLeaseHoldsOneLeaseForAnAction(t *testing.T) {
	t.Parallel()

	gateway := &recordingGateway{}
	lease := NewFileLease(gateway)
	root := t.TempDir()
	first := []FileOp{{Kind: FileOpRemove, Target: filepath.Join(root, "first.bak")}}
	second := []FileOp{{Kind: FileOpRemove, Target: filepath.Join(root, "second.bak")}}

	if err := lease.Apply(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if gateway.acquires != 0 {
		t.Fatal("an empty request started the helper")
	}
	for _, ops := range [][]FileOp{first, second} {
		if err := lease.Apply(t.Context(), ops); err != nil {
			t.Fatal(err)
		}
	}
	if gateway.acquires != 1 || gateway.releases != 0 || len(gateway.batches) != 2 {
		t.Fatalf(
			"acquires = %d, releases = %d, batches = %d; want one held lease for two batches",
			gateway.acquires, gateway.releases, len(gateway.batches),
		)
	}

	lease.Release()
	lease.Release()
	if gateway.releases != 1 {
		t.Fatalf("releases = %d, want 1", gateway.releases)
	}

	if err := lease.Apply(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if gateway.acquires != 2 {
		t.Fatalf("acquires after release = %d, want a new lease", gateway.acquires)
	}
}

func TestFileLeaseRemembersDeclinedConsent(t *testing.T) {
	t.Parallel()

	declined := errors.New("the operation was canceled by the user")
	gateway := &recordingGateway{acquireErr: declined}
	lease := NewFileLease(gateway)
	ops := []FileOp{{Kind: FileOpRemove, Target: filepath.Join(t.TempDir(), "stale.bak")}}

	for range 2 {
		if err := lease.Apply(t.Context(), ops); !errors.Is(err, declined) {
			t.Fatalf("Apply = %v, want the declined consent", err)
		}
	}
	if gateway.acquires != 1 || len(gateway.batches) != 0 {
		t.Fatalf("acquires = %d, batches = %d; want one prompt and no request", gateway.acquires, len(gateway.batches))
	}
	lease.Release()
	if gateway.releases != 0 {
		t.Fatal("a lease that was never granted was released")
	}
}

func TestFileLeaseTellsUnavailableHelperFromRefusedRequest(t *testing.T) {
	t.Parallel()

	ops := []FileOp{{Kind: FileOpRemove, Target: filepath.Join(t.TempDir(), "stale.bak")}}
	declined := errors.New("the operation was canceled by the user")
	refused := errors.New("target is not a regular file")

	var acquire *AcquireError
	err := NewFileLease(&recordingGateway{acquireErr: declined}).Apply(t.Context(), ops)
	if !errors.As(err, &acquire) || err.Error() != declined.Error() {
		t.Fatalf("Apply = %v, want the declined consent marked as an unavailable helper", err)
	}
	err = NewFileLease(&recordingGateway{applyErr: refused}).Apply(t.Context(), ops)
	if !errors.Is(err, refused) || errors.As(err, &acquire) {
		t.Fatalf("Apply = %v, want the refused request left unmarked", err)
	}
}

func TestFileLeaseHoldSharesTheLeaseWithFileRequests(t *testing.T) {
	t.Parallel()

	ops := []FileOp{{Kind: FileOpRemove, Target: filepath.Join(t.TempDir(), "stale.bak")}}
	gateway := &recordingGateway{}
	lease := NewFileLease(gateway)
	if err := lease.Apply(t.Context(), ops); err != nil {
		t.Fatal(err)
	}
	if err := lease.Hold(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gateway.acquires != 1 {
		t.Fatalf("acquires = %d, want the lease of the file request reused", gateway.acquires)
	}

	declined := errors.New("the operation was canceled by the user")
	gateway = &recordingGateway{acquireErr: declined}
	lease = NewFileLease(gateway)
	var acquire *AcquireError
	for range 2 {
		if err := lease.Hold(t.Context()); !errors.As(err, &acquire) || !errors.Is(err, declined) {
			t.Fatalf("Hold = %v, want the declined consent", err)
		}
	}
	if gateway.acquires != 1 {
		t.Fatalf("acquires = %d, want one prompt", gateway.acquires)
	}
	if err := NewFileLease(nil).Hold(t.Context()); err == nil {
		t.Fatal("hold without a helper succeeded")
	}
}

func TestFileLeaseReacquiresLostHelper(t *testing.T) {
	t.Parallel()

	ops := []FileOp{{Kind: FileOpRemove, Target: filepath.Join(t.TempDir(), "stale.bak")}}
	lost := fmt.Errorf("%w: elevated helper is not running", platform.ErrElevatedHelperRequired)

	t.Run("new helper takes the request", func(t *testing.T) {
		t.Parallel()
		gateway := &recordingGateway{applyErrs: []error{nil, lost}}
		lease := NewFileLease(gateway)
		for range 2 {
			if err := lease.Apply(t.Context(), ops); err != nil {
				t.Fatal(err)
			}
		}
		if gateway.acquires != 2 || gateway.releases != 1 || len(gateway.batches) != 3 {
			t.Fatalf(
				"acquires = %d, releases = %d, batches = %d; want the lost lease replaced and the request resent",
				gateway.acquires, gateway.releases, len(gateway.batches),
			)
		}

		lease.Release()
		if gateway.releases != 2 {
			t.Fatalf("releases = %d, want the replacement lease released", gateway.releases)
		}
	})

	t.Run("helper lost again", func(t *testing.T) {
		t.Parallel()
		gateway := &recordingGateway{applyErrs: []error{lost, lost}}
		lease := NewFileLease(gateway)
		if err := lease.Apply(t.Context(), ops); !errors.Is(err, platform.ErrElevatedHelperRequired) {
			t.Fatalf("Apply = %v, want the lost helper reported", err)
		}
		if gateway.acquires != 2 {
			t.Fatalf("acquires = %d, want one replacement", gateway.acquires)
		}
	})

	t.Run("failed request is not resent", func(t *testing.T) {
		t.Parallel()
		broken := errors.New("read elevated helper response: i/o timeout")
		gateway := &recordingGateway{applyErrs: []error{broken}}
		if err := NewFileLease(gateway).Apply(t.Context(), ops); !errors.Is(err, broken) {
			t.Fatalf("Apply = %v, want the transport failure", err)
		}
		if gateway.acquires != 1 || len(gateway.batches) != 1 {
			t.Fatalf(
				"acquires = %d, batches = %d; a request that may have run was sent again",
				gateway.acquires, len(gateway.batches),
			)
		}
	})
}

func TestFileLeaseRefusesBeforeAskingForConsent(t *testing.T) {
	t.Parallel()

	source, digest := stagedFile(t, "new settings")
	for _, test := range []struct {
		name string
		op   FileOp
	}{
		{name: "network target", op: FileOp{
			Kind: FileOpCopy, Source: source, Target: `\\server\share\d3d11.dll`, SHA256: digest,
		}},
		{name: "relative target", op: FileOp{Kind: FileOpRemove, Target: "d3d11.dll"}},
		{name: "missing digest", op: FileOp{
			Kind: FileOpCopy, Source: source, Target: filepath.Join(t.TempDir(), "d3d11.dll"),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gateway := &recordingGateway{}
			if err := NewFileLease(gateway).Apply(t.Context(), []FileOp{test.op}); err == nil {
				t.Fatal("unsafe request was sent")
			}
			if gateway.acquires != 0 {
				t.Fatal("a request the helper refuses started the helper")
			}
		})
	}

	t.Run("no helper", func(t *testing.T) {
		t.Parallel()
		ops := []FileOp{{Kind: FileOpRemove, Target: filepath.Join(t.TempDir(), "stale.bak")}}
		if err := NewFileLease(nil).Apply(t.Context(), ops); err == nil {
			t.Fatal("request without a helper succeeded")
		}
	})
}
