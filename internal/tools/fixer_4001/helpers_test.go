package fixer4001

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/elevated"
)

func openToolsTestDB(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	if err := client.Reconcile(context.Background()); err != nil {
		_ = client.Close()
		t.Fatalf("Reconcile: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// recordingElevatedFiles stands in for the elevated helper, so no test asks for UAC consent. It
// records what it was asked to do and, like the helper, checks each staged source against its
// digest; the sources are read during the call because callers remove them afterwards.
type recordingElevatedFiles struct {
	acquires   int
	releases   int
	acquireErr error
	applyErr   error
	// applies makes the copies reach the disk before applyErr is returned, as a helper does
	// whose response is lost on the way back.
	applies bool
	ops     []elevated.FileOp
	content map[string]string
}

func (h *recordingElevatedFiles) Acquire(context.Context) (func(), error) {
	h.acquires++
	if h.acquireErr != nil {
		return nil, h.acquireErr
	}
	return func() { h.releases++ }, nil
}

func (h *recordingElevatedFiles) ApplyFiles(_ context.Context, ops []elevated.FileOp) error {
	if h.content == nil {
		h.content = make(map[string]string)
	}
	for _, op := range ops {
		h.ops = append(h.ops, op)
		if op.Kind != elevated.FileOpCopy {
			continue
		}
		data, err := os.ReadFile(op.Source)
		if err != nil {
			return err
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != op.SHA256 {
			return errors.New("staged file does not match its digest: " + op.Target)
		}
		h.content[op.Target] = string(data)
		if h.applies {
			if err := writeProtectedFile(op.Target, data); err != nil {
				return err
			}
		}
	}
	return h.applyErr
}

// writeProtectedFile writes path the way the helper can and this process cannot: through the
// read-only attribute, which it puts back.
func writeProtectedFile(path string, data []byte) error {
	info, err := os.Stat(path)
	readOnly := err == nil && info.Mode().Perm()&0o200 == 0
	if readOnly {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if readOnly {
		return os.Chmod(path, 0o400)
	}
	return nil
}

func (h *recordingElevatedFiles) summary() []string {
	summary := make([]string, 0, len(h.ops))
	for _, op := range h.ops {
		summary = append(summary, string(op.Kind)+" "+op.Target)
	}
	return summary
}

// makeReadOnly makes writes to path fail with access denied for every user, administrators
// included, which is how a file in a protected folder looks to this process.
func makeReadOnly(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
}
