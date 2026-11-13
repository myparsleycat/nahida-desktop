package liquidemitter

import (
	"fmt"
	"path/filepath"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func validateBodyPath(root, name string) error {
	if platform.SamePathFold(root, name) || !platform.SameOrChildPath(root, name) {
		return infra.ContractError(ErrorSurfaceUnavailable + ":path")
	}
	physicalRoot, err := platform.PhysicalPath(root)
	if err != nil {
		return fmt.Errorf("resolve body runtime root %q: %w", root, err)
	}

	// Atomic replacement writes through the parent even when the existing file links back inside the root.
	for i, path := range []string{name, filepath.Dir(name)} {
		physical, err := platform.PhysicalPath(path)
		if err != nil {
			return fmt.Errorf("resolve body path %q: %w", path, err)
		}
		if !platform.SameOrChildPath(physicalRoot, physical) ||
			i == 0 && platform.SamePathFold(physicalRoot, physical) {
			return infra.AnnotateError(infra.ContractError(ErrorSurfaceUnavailable+":path"), infra.Diagnostic{
				Fields: map[string]any{"runtime_root": root, "file": name, "resolved_path": physical},
			})
		}
	}
	return nil
}
