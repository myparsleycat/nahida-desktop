package mod

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"nahida.live/desktop/internal/infra"
)

var mediaExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".avif": true, ".avifs": true, ".mp4": true, ".webm": true,
	".avi": true, ".mkv": true, ".mov": true, ".ogg": true,
}

// Electron's native ordinary-mod scanner excludes OGG even though its NTE
// TypeScript preview finder and paste-preview flow accept it.
var scannerMediaExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".bmp": true, ".avif": true, ".avifs": true, ".mp4": true, ".webm": true,
	".avi": true, ".mkv": true, ".mov": true,
}

var excludedPreviewFragments = []string{"normal", "light", "material", "diffuse"}

type walkedMod struct {
	info     *ModInfo
	iniPaths []string
}

func listGroups(root string, fallback bool, reports ...func(error)) []FolderGroup {
	entries, err := os.ReadDir(root)
	if err != nil {
		reportScanFailure(err, reports)
		return []FolderGroup{}
	}
	groups := make([]FolderGroup, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		count, enabled := countChildMods(path, reports...)
		groups = append(groups, FolderGroup{
			Name: entry.Name(), Path: path, Mods: []ModInfo{}, Preview: findPreview(path, fallback, reports...),
			ModCount: count, EnabledModCount: enabled,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return naturalLess(groups[i].Name, groups[j].Name) })
	return groups
}

func countChildMods(root string, reports ...func(error)) (int, int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		reportScanFailure(err, reports)
		return 0, 0
	}
	total, enabled := 0, 0
	for _, entry := range entries {
		if !entry.IsDir() || !hasAnyFile(filepath.Join(root, entry.Name()), reports...) {
			continue
		}
		total++
		if !isDisabled(entry.Name()) {
			enabled++
		}
	}
	return total, enabled
}

func hasAnyFile(root string, reports ...func(error)) bool {
	found := false
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			reportScanFailure(err, reports)
			return err
		}
		if entry.Type().IsRegular() {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func scanWorkers() int {
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		return 1
	}
	return workers
}

func mapParallel[T, R any](items []T, fn func(T) R) []R {
	out := make([]R, len(items))
	if len(items) == 0 {
		return out
	}
	workers := scanWorkers()
	if workers > len(items) {
		workers = len(items)
	}
	indexes := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range indexes {
				out[i] = fn(items[i])
			}
		}()
	}
	for i := range items {
		indexes <- i
	}
	close(indexes)
	wg.Wait()
	return out
}

func scanGroup(groupPath string, reports ...func(error)) FolderGroup {
	groupPath = filepath.Clean(groupPath)
	result := FolderGroup{
		Name: filepath.Base(groupPath), Path: groupPath, Mods: []ModInfo{},
	}
	entries, err := os.ReadDir(groupPath)
	if err != nil {
		reportScanFailure(err, reports)
		return result
	}
	modDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			modDirs = append(modDirs, filepath.Join(groupPath, entry.Name()))
		}
	}

	var preview *string
	var walked []*walkedMod
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		preview = findScannerGroupPreview(groupPath, previewSearchDepth, reports...)
	}()
	go func() {
		defer wg.Done()
		walked = mapParallel(modDirs, func(modPath string) *walkedMod {
			return walkMod(groupPath, modPath, reports...)
		})
	}()
	wg.Wait()

	result.Preview = preview
	result.Mods = collectWalkedMods(walked, reports...)
	result.ModCount = len(result.Mods)
	for _, info := range result.Mods {
		if info.IsEnabled {
			result.EnabledModCount++
		}
	}
	return result
}

func scanGroupLight(groupPath string, reports ...func(error)) FolderGroup {
	groupPath = filepath.Clean(groupPath)
	result := FolderGroup{
		Name: filepath.Base(groupPath), Path: groupPath, Mods: []ModInfo{},
	}
	entries, err := os.ReadDir(groupPath)
	if err != nil {
		reportScanFailure(err, reports)
		return result
	}
	modDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			modDirs = append(modDirs, filepath.Join(groupPath, entry.Name()))
		}
	}

	var preview *string
	var mods []ModInfo
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		preview = findScannerGroupPreview(groupPath, previewSearchDepth, reports...)
	}()
	go func() {
		defer wg.Done()
		scanned := mapParallel(modDirs, func(modPath string) *ModInfo {
			return scanModLight(groupPath, modPath, reports...)
		})
		mods = make([]ModInfo, 0, len(scanned))
		for _, info := range scanned {
			if info != nil {
				mods = append(mods, *info)
			}
		}
		sort.Slice(mods, func(i, j int) bool {
			return naturalLess(mods[i].Name, mods[j].Name)
		})
	}()
	wg.Wait()

	result.Preview = preview
	result.Mods = mods
	result.ModCount = len(result.Mods)
	for _, info := range result.Mods {
		if info.IsEnabled {
			result.EnabledModCount++
		}
	}
	return result
}

func scanModLight(groupPath, modPath string, reports ...func(error)) *ModInfo {
	if !hasAnyFile(modPath, reports...) {
		return nil
	}
	name := filepath.Base(modPath)
	preview := findScannerPreviewWalk(modPath, previewSearchDepth, reports...)
	info := &ModInfo{
		ID: stableID(groupPath, modPath), Name: name, Path: modPath,
		IsEnabled: !isDisabled(name), Inis: []IniResult{},
	}
	if preview != nil {
		info.Preview = stringPointer(preview.path)
	}
	return info
}

func walkMod(groupPath, modPath string, reports ...func(error)) *walkedMod {
	name := filepath.Base(modPath)
	info := &ModInfo{
		ID: stableID(groupPath, modPath), Name: name, Path: modPath,
		IsEnabled: !isDisabled(name), Inis: []IniResult{},
	}
	iniPaths := []string{}
	found := false
	var buckets previewBuckets
	_ = filepath.WalkDir(modPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			reportScanFailure(err, reports)
			return fs.SkipDir
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		found = true
		metadata, statErr := entry.Info()
		reportScanFailure(statErr, reports)
		if statErr == nil {
			info.Size += float64(metadata.Size())
			if mtime := fileMtimeMS(metadata); mtime > info.Mtime {
				info.Mtime = mtime
			}
		}
		if strings.EqualFold(filepath.Ext(path), ".ini") &&
			!strings.HasPrefix(strings.ToLower(entry.Name()), "disabled") {
			iniPaths = append(iniPaths, path)
		} else {
			buckets.consider(modPath, path, entry.Name(), scannerMediaExtensions)
		}
		return nil
	})
	if !found {
		return nil
	}
	if info.Mtime == 0 {
		if metadata, err := os.Stat(modPath); err == nil {
			info.Mtime = fileMtimeMS(metadata)
		}
	}
	info.Preview = buckets.bestPath()
	return &walkedMod{info: info, iniPaths: iniPaths}
}

func scanMod(groupPath, modPath string, reports ...func(error)) *ModInfo {
	walked := walkMod(groupPath, modPath, reports...)
	if walked == nil {
		return nil
	}
	walked.info.Inis = parseAndSortINIs(walked.iniPaths, reports...)
	return walked.info
}

func collectWalkedMods(walked []*walkedMod, reports ...func(error)) []ModInfo {
	items := make([]*walkedMod, 0, len(walked))
	for _, item := range walked {
		if item == nil || item.info == nil {
			continue
		}
		items = append(items, item)
	}
	parsed := mapParallel(items, func(item *walkedMod) *ModInfo {
		item.info.Inis = parseAndSortINIs(item.iniPaths, reports...)
		return item.info
	})
	mods := make([]ModInfo, 0, len(parsed))
	for _, info := range parsed {
		if info != nil {
			mods = append(mods, *info)
		}
	}
	sort.Slice(mods, func(i, j int) bool {
		return naturalLess(mods[i].Name, mods[j].Name)
	})
	return mods
}

func parseAndSortINIs(paths []string, reports ...func(error)) []IniResult {
	inis := make([]IniResult, len(paths))
	for i, path := range paths {
		inis[i] = parseINI(path, reports...)
	}
	return sortINIs(inis)
}

func sortINIs(inis []IniResult) []IniResult {
	sort.Slice(inis, func(i, j int) bool {
		if inis[i].HasToggleKey != inis[j].HasToggleKey {
			return inis[i].HasToggleKey
		}
		return naturalLess(inis[i].Name, inis[j].Name)
	})
	return inis
}

func parseINI(path string, reports ...func(error)) IniResult {
	result := IniResult{Name: filepath.Base(path), Path: path, ToggleKeys: []ToggleKey{}}
	file, err := os.Open(path)
	if err != nil {
		reportScanFailure(err, reports)
		return result
	}
	defer func() { _ = file.Close() }()
	section := ""
	values := map[string]string{}
	flush := func() {
		if key := sectionToggle(section, filepath.Base(path), values); key != nil {
			result.ToggleKeys = append(result.ToggleKeys, *key)
			if key.Key != nil {
				result.HasToggleKey = true
			}
		}
		values = map[string]string{}
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section = line[1 : len(line)-1]
			continue
		}
		if section != "" {
			if at := strings.IndexByte(line, '='); at >= 0 {
				values[strings.ToLower(strings.TrimSpace(line[:at]))] = strings.TrimSpace(line[at+1:])
			}
		}
	}
	reportScanFailure(infra.AnnotateError(scanner.Err(), infra.Diagnostic{Stage: "read-ini", Fields: map[string]any{"path": path}}), reports)
	flush()
	sort.SliceStable(result.ToggleKeys, func(i, j int) bool {
		return result.ToggleKeys[i].Key != nil && result.ToggleKeys[j].Key == nil
	})
	return result
}

func sectionToggle(section, fileName string, data map[string]string) *ToggleKey {
	if !strings.HasPrefix(strings.ToLower(section), "key") {
		return nil
	}
	typeValue := optionalMapValue(data, "type")
	isHold := typeValue != nil && strings.EqualFold(*typeValue, "hold")
	variables := make([]string, 0)
	for key := range data {
		if strings.HasPrefix(key, "$") {
			variables = append(variables, key)
		}
	}
	sort.Strings(variables)
	for _, variable := range variables {
		parts := strings.Split(data[variable], ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) < 2 && !isHold {
			continue
		}
		current := ""
		if len(parts) > 0 {
			current = parts[0]
		}
		return &ToggleKey{
			SectionName: section, IniFileName: fileName, Key: optionalMapValue(data, "key"),
			Back: optionalMapValue(data, "back"), Type: typeValue, Variable: variable,
			Values: parts, CurrentValue: stringPointer(current),
		}
	}
	return nil
}

func optionalMapValue(data map[string]string, key string) *string {
	value := strings.TrimSpace(data[key])
	if value == "" {
		return nil
	}
	return stringPointer(value)
}

func reportScanFailure(err error, reports []func(error)) {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return
	}
	for _, report := range reports {
		if report != nil {
			report(err)
		}
	}
}
