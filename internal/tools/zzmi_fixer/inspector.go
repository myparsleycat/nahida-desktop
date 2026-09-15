package zzmifixer

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	fixinspection "nahida.live/desktop/internal/tools/fix_inspection"
	zzmiengine "nahida.live/desktop/internal/tools/zzmi"
)

var inspectorHashPattern = regexp.MustCompile(`(?i)^\s*hash\s*=\s*([a-f0-9]{8})\b`)

type Inspector struct {
	service *Service
}

func NewInspector(service *Service) *Inspector {
	return &Inspector{service: service}
}

func (z *Inspector) CanInspect(importer string) bool {
	return strings.EqualFold(importer, "ZZMI")
}

func (z *Inspector) Inspect(ctx context.Context, modPath string) (*fixinspection.FixInspectionResult, error) {
	var target string
	if z.service.client != nil {
		resolved, _, err := z.service.zzmiRequireTarget(ctx, modPath)
		if err != nil {
			return nil, err
		}
		target = resolved
	} else {
		info, err := os.Stat(modPath)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return &fixinspection.FixInspectionResult{
				NeedsFix: false,
				Importer: "ZZMI",
				ToolName: "ZZMI Mod Fixer",
			}, nil
		}
		target = modPath
	}

	pack, _, err := z.service.zzmiLoadActivePack()
	if err != nil {
		return nil, err
	}

	janeHashes, dialynHashes := collectRemapperHashes(pack)
	hasJane, hasDialyn, err := z.checkRemapperHashes(ctx, target, janeHashes, dialynHashes)
	if err != nil {
		return nil, err
	}

	var actions []string
	var summaries []string
	var allAffected []string

	hashResult, err := zzmiengine.Run(ctx, target, zzmiengine.ToolHash, pack, nil)
	if err != nil {
		return nil, fmt.Errorf("inspect ZZMI hash fixes: %w", err)
	}
	if len(hashResult.Changes) > 0 {
		actions = append(actions, zzmiengine.ToolHash)
		summaries = append(summaries, fmt.Sprintf("%d outdated hash file(s)", len(hashResult.Changes)))
		for _, c := range hashResult.Changes {
			rel, relErr := filepath.Rel(target, c.Path)
			if relErr != nil {
				rel = filepath.Base(c.Path)
			}
			allAffected = append(allAffected, rel)
		}
	}

	if hasJane {
		janeResult, err := zzmiengine.Run(ctx, target, zzmiengine.ToolJane, pack, nil)
		if err != nil {
			return nil, fmt.Errorf("inspect ZZMI Jane remapping: %w", err)
		}
		if len(janeResult.Changes) > 0 {
			actions = append(actions, zzmiengine.ToolJane)
			summaries = append(summaries, "Jane Doe blend remapping required")
			for _, c := range janeResult.Changes {
				rel, relErr := filepath.Rel(target, c.Path)
				if relErr != nil {
					rel = filepath.Base(c.Path)
				}
				allAffected = append(allAffected, rel)
			}
		}
	}

	if hasDialyn {
		dialynResult, err := zzmiengine.Run(ctx, target, zzmiengine.ToolDialyn, pack, nil)
		if err != nil {
			return nil, fmt.Errorf("inspect ZZMI Dialyn remapping: %w", err)
		}
		if len(dialynResult.Changes) > 0 {
			actions = append(actions, zzmiengine.ToolDialyn)
			summaries = append(summaries, "Dialyn blend remapping required")
			for _, c := range dialynResult.Changes {
				rel, relErr := filepath.Rel(target, c.Path)
				if relErr != nil {
					rel = filepath.Base(c.Path)
				}
				allAffected = append(allAffected, rel)
			}
		}
	}

	if len(actions) > 0 {
		return &fixinspection.FixInspectionResult{
			NeedsFix:      true,
			Importer:      "ZZMI",
			ToolName:      "ZZMI Mod Fixer",
			Summary:       strings.Join(summaries, ", "),
			Details:       summaries,
			AffectedFiles: allAffected,
			ActionTool:    actions[0],
		}, nil
	}

	return &fixinspection.FixInspectionResult{
		NeedsFix: false,
		Importer: "ZZMI",
		ToolName: "ZZMI Mod Fixer",
	}, nil
}

func collectRemapperHashes(pack *zzmiengine.RulePack) (janeHashes, dialynHashes map[string]bool) {
	janeHashes = make(map[string]bool)
	dialynHashes = make(map[string]bool)

	for pos, blend := range pack.Jane.PositionToBlend {
		janeHashes[strings.ToLower(pos)] = true
		janeHashes[strings.ToLower(blend)] = true
	}
	for _, hash := range pack.Jane.ValidHashes {
		janeHashes[strings.ToLower(hash)] = true
	}

	for pos, blend := range pack.Dialyn.PositionToBlend {
		dialynHashes[strings.ToLower(pos)] = true
		dialynHashes[strings.ToLower(blend)] = true
	}
	for _, hash := range pack.Dialyn.ValidHashes {
		dialynHashes[strings.ToLower(hash)] = true
	}

	// Follow update_hash chains so indirect legacy hashes inherit remapper ownership.
	for {
		added := false
		for oldHash, commands := range pack.HashCommands {
			lowerOld := strings.ToLower(oldHash)
			for _, cmd := range commands {
				if cmd.Op == "update_hash" && len(cmd.Args) > 0 {
					if targetHash, ok := cmd.Args[0].(string); ok {
						lowerTarget := strings.ToLower(targetHash)
						if janeHashes[lowerTarget] && !janeHashes[lowerOld] {
							janeHashes[lowerOld] = true
							added = true
						}
						if dialynHashes[lowerTarget] && !dialynHashes[lowerOld] {
							dialynHashes[lowerOld] = true
							added = true
						}
					}
				}
			}
		}
		if !added {
			break
		}
	}

	return janeHashes, dialynHashes
}

func (z *Inspector) checkRemapperHashes(
	ctx context.Context,
	target string,
	janeHashes, dialynHashes map[string]bool,
) (hasJane bool, hasDialyn bool, err error) {
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path != target && entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		upper := strings.ToUpper(entry.Name())
		if path != target && entry.IsDir() &&
			(strings.HasPrefix(upper, "DISABLED") || strings.HasPrefix(upper, "DESKTOP")) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ini") ||
			strings.HasPrefix(upper, "DISABLED") ||
			strings.HasPrefix(upper, "DESKTOP") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			match := inspectorHashPattern.FindStringSubmatch(line)
			if len(match) == 2 {
				hash := strings.ToLower(match[1])
				if janeHashes[hash] {
					hasJane = true
				}
				if dialynHashes[hash] {
					hasDialyn = true
				}
				if hasJane && hasDialyn {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	return hasJane, hasDialyn, err
}
