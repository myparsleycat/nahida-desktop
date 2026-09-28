//go:build windows

package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/sys/windows/registry"
)

type GameFolderCandidate struct {
	Path    string `json:"path"`
	ExePath string `json:"exePath"`
	ModTime int64  `json:"modTime"`
}

var detectedWindowsPath = regexp.MustCompile(`(?i)[A-Z]:[/\\][^\r\n"']+`)
var hoyoplayInstallPath = regexp.MustCompile(`(?i)"(?:persistentInstallPath|installPath)"\s*:\s*"([^"]+)"`)

func (x *XXMI) DetectGameFolders(ctx context.Context, importer string) ([]GameFolderCandidate, error) {
	spec, ok := lookupImporterPackage(importer)
	if !ok {
		return nil, errors.New("unknown XXMI importer")
	}
	paths := make(map[string]struct{})
	add := func(path string) {
		path = filepath.Clean(strings.TrimSpace(strings.ReplaceAll(path, `\\`, `\`)))
		if filepath.IsAbs(path) && filepath.VolumeName(path) != "" && !strings.HasPrefix(path, `\\`) {
			paths[path] = struct{}{}
		}
	}
	for _, source := range []struct {
		hive registry.Key
		path string
	}{
		{registry.CLASSES_ROOT, `Local Settings\Software\Microsoft\Windows\Shell\MuiCache`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\FeatureUsage\AppSwitched`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Explorer\FeatureUsage\ShowJumpView`},
	} {
		key, err := registry.OpenKey(source.hive, source.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		names, err := key.ReadValueNames(0)
		_ = key.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			for _, exe := range append(append([]string(nil), spec.gameExeNames...), spec.processNames...) {
				if index := strings.Index(strings.ToLower(name), strings.ToLower(exe)); index >= 0 {
					add(name[:index])
				}
			}
		}
	}
	appdata := os.Getenv("APPDATA")
	if appdata != "" {
		localLow := filepath.Join(filepath.Dir(appdata), "LocalLow")
		for _, relative := range gameDetectionLogs(importer) {
			readGamePathHints(filepath.Join(localLow, relative), importer, add)
		}
		for _, source := range []struct{ folder, name string }{
			{filepath.Join(appdata, "Cognosphere", "HYP"), "gamedata.dat"},
			{filepath.Join(appdata, "KRLauncher"), "kr_starter_game.json"},
		} {
			_ = filepath.WalkDir(source.folder, func(path string, entry fs.DirEntry, walkErr error) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if walkErr != nil {
					return walkErr
				}
				if entry == nil {
					return nil
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if !entry.IsDir() && entry.Name() == source.name {
					readGamePathHints(path, importer, add)
				}
				return nil
			})
		}
	}
	if importer == "EFMI" {
		add(`C:\Program Files\GRYPHLINK\games\EndField Game`)
		add(`D:\GRYPHLINK\games\EndField Game`)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]GameFolderCandidate, 0, len(paths))
	seen := make(map[string]struct{})
	for path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if importer == "WWMI" {
			path = normalizeWWGameFolder(path)
		}
		executable := configuredGameExecutable(path, spec.gameExeNames)
		if executable == "" {
			continue
		}
		info, err := os.Stat(executable)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		path = filepath.Dir(executable)
		id := strings.ToLower(path)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		results = append(results, GameFolderCandidate{Path: path, ExePath: executable, ModTime: info.ModTime().Unix()})
	}
	slices.SortFunc(results, func(a, b GameFolderCandidate) int { return int(b.ModTime - a.ModTime) })
	return results, nil
}

func gameDetectionLogs(importer string) []string {
	switch importer {
	case "GIMI":
		return []string{`miHoYo\Genshin Impact\output_log.txt`, `miHoYo\Genshin Impact\output_log.txt.last`}
	case "SRMI":
		return []string{
			`Cognosphere\Star Rail\Player.log`,
			`Cognosphere\Star Rail\Player-prev.log`,
			`Cognosphere\Star Rail\output_log.txt`,
		}
	case "ZZMI":
		return []string{
			`miHoYo\ZenlessZoneZero\Player.log`,
			`miHoYo\ZenlessZoneZero\Player-prev.log`,
			`miHoYo\ZenlessZoneZero\output_log.txt`,
		}
	case "HIMI":
		return []string{`miHoYo\Honkai Impact 3rd\output_log.txt`}
	case "EFMI":
		return []string{`Gryphline\Endfield\Player.log`}
	default:
		return nil
	}
}

func readGamePathHints(path, importer string, add func(string)) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if filepath.Base(path) == "kr_starter_game.json" {
		var launcher struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(data, &launcher) == nil {
			add(launcher.Path)
		}
		return
	}
	for _, match := range hoyoplayInstallPath.FindAllSubmatch(data, -1) {
		var decoded string
		if json.Unmarshal(append(append([]byte{'"'}, match[1]...), '"'), &decoded) == nil {
			add(decoded)
		}
	}
	for _, match := range detectedWindowsPath.FindAllString(string(data), -1) {
		candidate := strings.ReplaceAll(match, `\\`, `\`)
		for _, child := range []string{"GenshinImpact_Data", "StarRail_Data", "ZenlessZoneZero_Data", "BH3_Data", "Endfield_Data", "Client"} {
			if index := strings.Index(strings.ToLower(candidate), strings.ToLower(child)); index >= 0 {
				candidate = candidate[:index]
			}
		}
		add(candidate)
	}
}

func normalizeWWGameFolder(path string) string {
	for current := filepath.Clean(path); filepath.IsAbs(current); current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "Wuthering Waves.exe")); err == nil {
			return current
		}
		if parent := filepath.Dir(current); parent == current {
			break
		}
	}
	return path
}
