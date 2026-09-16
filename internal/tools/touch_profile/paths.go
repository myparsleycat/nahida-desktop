package touchprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

func rebaseTouchAnalysis(analysis TouchModAnalysis, sourceRoot, targetRoot string) (TouchModAnalysis, error) {
	analysisRoot := filepath.Join(sourceRoot, analysis.ModRootRelativeToSource)
	ini, err := resolveTouchRelative(analysisRoot, analysis.INIRelativePath)
	if err != nil {
		return analysis, err
	}
	analysis.INIPath, err = remapTouchPath(ini, sourceRoot, targetRoot)
	if err != nil {
		return analysis, err
	}
	analysis.ModRoot = targetRoot
	for i := range analysis.Components {
		component := &analysis.Components[i]
		path, resolveErr := resolveTouchRelative(analysisRoot, component.PositionRelativePath)
		if resolveErr != nil {
			return analysis, resolveErr
		}
		component.PositionPath, err = remapTouchPath(path, sourceRoot, targetRoot)
		if err != nil {
			return analysis, err
		}
		component.IndexPaths = []string{}
		for _, relative := range component.IndexRelativePaths {
			path, resolveErr = resolveTouchRelative(analysisRoot, relative)
			if resolveErr != nil {
				return analysis, resolveErr
			}
			path, err = remapTouchPath(path, sourceRoot, targetRoot)
			if err != nil {
				return analysis, err
			}
			component.IndexPaths = append(component.IndexPaths, path)
		}
		if component.IndexRelativePath != nil {
			path, resolveErr = resolveTouchRelative(analysisRoot, *component.IndexRelativePath)
			if resolveErr != nil {
				return analysis, resolveErr
			}
			path, err = remapTouchPath(path, sourceRoot, targetRoot)
			if err != nil {
				return analysis, err
			}
			component.IndexPath = &path
		}
		if component.BlendRelativePath != nil {
			path, resolveErr = resolveTouchRelative(analysisRoot, *component.BlendRelativePath)
			if resolveErr != nil {
				return analysis, resolveErr
			}
			path, err = remapTouchPath(path, sourceRoot, targetRoot)
			if err != nil {
				return analysis, err
			}
			component.BlendPath = &path
		}
	}
	return analysis, nil
}

func resolveTouchRelative(root, relative string) (string, error) {
	root, _ = filepath.Abs(root)
	absolute, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil || platform.SamePathFold(root, absolute) || !platform.SameOrChildPath(root, absolute) {
		return "", infra.WithCause(infra.ContractError(fmt.Sprintf("Path is outside mod root: %s", relative)), err)
	}
	return absolute, nil
}

func remapTouchPath(path, sourceRoot, targetRoot string) (string, error) {
	absolute, _ := filepath.Abs(path)
	relative, err := filepath.Rel(sourceRoot, absolute)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relative) {
		return "", infra.WithCause(infra.ContractError(fmt.Sprintf("Path is outside mod root: %s", path)), err)
	}
	return filepath.Join(targetRoot, relative), nil
}

func assertTouchSourceUnchanged(analysis TouchModAnalysis, sourceRoot string) error {
	root := filepath.Join(sourceRoot, analysis.ModRootRelativeToSource)
	meshPaths := []string{}
	for _, component := range analysis.Components {
		path, err := resolveTouchRelative(root, component.PositionRelativePath)
		if err != nil {
			return err
		}
		meshPaths = append(meshPaths, path)
		for _, relative := range component.IndexRelativePaths {
			path, err = resolveTouchRelative(root, relative)
			if err != nil {
				return err
			}
			meshPaths = append(meshPaths, path)
		}
		if len(component.IndexRelativePaths) == 0 && component.IndexRelativePath != nil {
			path, err = resolveTouchRelative(root, *component.IndexRelativePath)
			if err != nil {
				return err
			}
			meshPaths = append(meshPaths, path)
		}
		if component.BlendRelativePath != nil {
			path, err = resolveTouchRelative(root, *component.BlendRelativePath)
			if err != nil {
				return err
			}
			meshPaths = append(meshPaths, path)
		}
	}
	sourcePaths := analysis.SourceFilesRelativePaths
	if len(sourcePaths) == 0 {
		sourcePaths = []string{analysis.INIRelativePath}
	}
	iniPaths := []string{}
	for _, relative := range sourcePaths {
		path, err := resolveTouchRelative(root, relative)
		if err != nil {
			return err
		}
		iniPaths = append(iniPaths, path)
	}
	meshHash, err := hashTouchFiles(meshPaths, root)
	if err != nil {
		return err
	}
	iniHash, err := hashTouchFiles(iniPaths, root)
	if err != nil {
		return err
	}
	if meshHash != analysis.MeshHash || iniHash != analysis.INIHash {
		return infra.ContractError("Touch source mod changed since analysis; analyze the mod again")
	}
	return nil
}

func pathIsDirectory(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
