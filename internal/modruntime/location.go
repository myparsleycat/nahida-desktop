package modruntime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/platform"
)

// ModLocation places a mod folder in the game whose Mods folder holds it.
type ModLocation struct {
	// Root is the absolute mod folder.
	Root string
	// ModsRoot and Importer are empty when no game's Mods folder holds Root.
	ModsRoot string
	Importer string
}

// ResolveMod returns where the mod folder at modPath lives. A folder that is
// a Mods folder itself is not a mod. Mods folders are often junctions, so
// they are compared both as written and as resolved by Windows, and the
// deepest one holding the mod wins.
func ResolveMod(ctx context.Context, games GamePaths, modPath string) (ModLocation, error) {
	if strings.TrimSpace(modPath) == "" {
		return ModLocation{}, nil
	}
	root, err := filepath.Abs(modPath)
	if err != nil {
		return ModLocation{}, fmt.Errorf("resolve mod path %q: %w", modPath, err)
	}
	location := ModLocation{Root: root}
	info, err := os.Stat(root)
	if err != nil {
		return location, fmt.Errorf("open mod folder %q: %w", root, err)
	}
	if !info.IsDir() {
		return location, nil
	}
	physical := finalOr(root)

	rows, err := games.List(ctx)
	if err != nil {
		return location, fmt.Errorf("list game paths: %w", err)
	}
	longest := -1
	for _, row := range rows {
		roots := []string{row.ModFolderPath}
		if row.LinkedModFolderPath != nil {
			roots = append(roots, *row.LinkedModFolderPath)
		}
		for _, gameRoot := range roots {
			if strings.TrimSpace(gameRoot) == "" {
				continue
			}
			for _, pair := range [][2]string{{gameRoot, root}, {finalOr(gameRoot), physical}} {
				if !platform.SameOrChildPath(pair[0], pair[1]) || platform.SamePathFold(pair[0], pair[1]) ||
					len(pair[0]) <= longest {
					continue
				}
				longest, location.ModsRoot, location.Importer = len(pair[0]), row.ModFolderPath, ""
				if row.Importer != nil {
					location.Importer = *row.Importer
				}
			}
		}
	}
	return location, nil
}

func finalOr(path string) string {
	if resolved, err := platform.FinalPath(path); err == nil {
		return resolved
	}
	return path
}
