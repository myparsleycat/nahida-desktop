package xxmi

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// ErrNamespaceGameRunning keeps namespace writes out of games started by any launcher.
var ErrNamespaceGameRunning = errors.New("XXMI_GAME_RUNNING")

// UseNamespaceLaunchPreparation registers a pre-launch check under the importer operation gate.
//
//wails:ignore
func (x *XXMI) UseNamespaceLaunchPreparation(prepare func(context.Context, string) error) {
	x.mu.Lock()
	x.namespaceLaunchPreparation = prepare
	x.mu.Unlock()
}

func (x *XXMI) prepareNamespaceLaunch(ctx context.Context, importer string) error {
	x.mu.RLock()
	prepare := x.namespaceLaunchPreparation
	x.mu.RUnlock()
	if prepare == nil {
		return nil
	}
	return prepare(ctx, importer)
}

// WithNamespaceMutation excludes application launches and importer maintenance during a namespace transaction.
//
//wails:ignore
func (x *XXMI) WithNamespaceMutation(ctx context.Context, key string, change func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key = strings.ToUpper(strings.TrimSpace(key))
	if _, ok := lookupImporterPackage(key); !ok {
		return fmt.Errorf("unknown namespace importer %q", key)
	}
	if !x.acquireImporter(key) {
		return errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(key)
	return change()
}

// CheckNamespaceGameStopped is conservative: a matching game image blocks writes
// even when its full path cannot be queried or belongs to another installation.
//
//wails:ignore
func (x *XXMI) CheckNamespaceGameStopped(ctx context.Context, key string) error {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return fmt.Errorf("unknown namespace importer %q", key)
	}
	names := append(slices.Clone(spec.gameExeNames), spec.processNames...)
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return fmt.Errorf("resolve namespace game processes: %w", err)
	}
	if external {
		runtime, err := x.externalHuntingRuntime(ctx, key)
		if err != nil {
			return fmt.Errorf("resolve external namespace game processes: %w", err)
		}
		names = append(names, runtime.GameEXENames...)
	}
	if len(names) == 0 || x.findProcess == nil {
		return errors.New("namespace game process information is unavailable")
	}
	seen := map[string]bool{}
	for _, name := range names {
		// Image-only matching does not mistake access-denied processes for stopped games.
		name = filepath.Base(strings.TrimSpace(name))
		if name == "." || name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		pid, err := x.findProcess(ctx, name)
		if err != nil {
			return fmt.Errorf("check namespace game process %q: %w", name, err)
		}
		if pid != 0 {
			return fmt.Errorf("%w: %s", ErrNamespaceGameRunning, name)
		}
	}
	return ctx.Err()
}
