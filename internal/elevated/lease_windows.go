//go:build windows

package elevated

import (
	"context"
	"errors"
	"fmt"

	"nahida.live/desktop/internal/platform"
)

// FileGateway reaches the helper's file operations. Acquire starts the helper, asking for UAC
// consent unless it already runs, and keeps it running until release is called.
type FileGateway interface {
	Acquire(ctx context.Context) (release func(), err error)
	ApplyFiles(ctx context.Context, ops []FileOp) error
}

// FileLease applies the file operations of one user action under a single helper lease, so the
// action asks for UAC consent at most once while the helper stays up. The first Apply acquires
// the helper; a declined or failed acquisition is returned by every later Apply instead of
// asking again. It is not safe for concurrent use.
type FileLease struct {
	gateway  FileGateway
	acquired bool
	release  func()
	err      error
}

// AcquireError is a helper that could not be started for a lease, which includes declined UAC
// consent. It reads as the failure it wraps, so callers keep their own error prefixes.
type AcquireError struct{ Err error }

func (e *AcquireError) Error() string { return e.Err.Error() }

func (e *AcquireError) Unwrap() error { return e.Err }

func NewFileLease(gateway FileGateway) *FileLease {
	return &FileLease{gateway: gateway}
}

// Hold starts the helper for the action without a file request, for an action that also needs
// the helper for something else. Like Apply, it asks for consent at most once.
func (l *FileLease) Hold(ctx context.Context) error {
	if l == nil || l.gateway == nil {
		return errors.New("elevated helper is not configured")
	}
	return l.hold(ctx)
}

func (l *FileLease) Apply(ctx context.Context, ops []FileOp) error {
	if len(ops) == 0 {
		return nil
	}
	if l == nil || l.gateway == nil {
		return errors.New("elevated helper is not configured")
	}

	// The helper would refuse these, so they must not cost the user a UAC prompt first.
	for _, op := range ops {
		if err := validateFileOp(op); err != nil {
			return fmt.Errorf("%s %q: %w", op.Kind, op.Target, err)
		}
	}

	if err := l.hold(ctx); err != nil {
		return err
	}
	err := l.gateway.ApplyFiles(ctx, ops)
	if !errors.Is(err, platform.ErrElevatedHelperRequired) {
		return err
	}

	// The helper was lost under the lease, as happens when an earlier request is cancelled in
	// flight. This request never reached it, so a new helper can take it: the rollback of a
	// cancelled action depends on that, at the cost of a second prompt.
	l.Release()
	if err := l.hold(ctx); err != nil {
		return err
	}
	return l.gateway.ApplyFiles(ctx, ops)
}

func (l *FileLease) hold(ctx context.Context) error {
	if !l.acquired {
		l.acquired = true
		release, err := l.gateway.Acquire(ctx)
		if err != nil {
			l.err = &AcquireError{Err: err}
		} else {
			l.release = release
		}
	}
	return l.err
}

// Release ends the action. An Apply after it starts a new one and may ask for consent again.
func (l *FileLease) Release() {
	if l == nil {
		return
	}
	if l.release != nil {
		l.release()
	}
	l.acquired, l.release, l.err = false, nil, nil
}
