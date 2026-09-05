package mod

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
)

const (
	nteExecutableName = "HTGame.exe"
	nteModsRelative   = "Content/Paks/Mods"
)

var nteDefaultSubfolders = []string{"Character", "UI", "Enemy", "NPC"}

type NtePathResolution struct {
	GameRootPath        string `json:"gameRootPath"`
	ExecutablePath      string `json:"executablePath"`
	ModFolderPath       string `json:"modFolderPath"`
	LinkedModFolderPath string `json:"linkedModFolderPath"`
	RequiresElevation   bool   `json:"requiresElevation"`
}

type nteRoots struct {
	modRoot    string
	linkedRoot string
}

type nteModEntry struct {
	path            string
	name            string
	previewFallback string
}

func (m *Mod) ResolveNteInstallPath(_ context.Context, inputPath string) (*NtePathResolution, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, nil
	}
	info, err := os.Stat(inputPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	executable := ""
	if !info.IsDir() && strings.EqualFold(filepath.Base(inputPath), nteExecutableName) {
		executable, err = filepath.Abs(inputPath)
	} else if info.IsDir() {
		common := filepath.Join(inputPath, filepath.FromSlash(
			"Neverness To Everness/Client/WindowsNoEditor/HT/Binaries/Win64/"+nteExecutableName,
		))
		if fileExists(common) {
			executable = common
		} else {
			executable, err = findNteExecutable(inputPath)
		}
	}
	if err != nil || executable == "" {
		return nil, err
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	htRoot := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", ".."))
	linked := filepath.Join(htRoot, filepath.FromSlash(nteModsRelative))
	modRoot := linked
	requiresElevation := false
	if target, linkErr := resolveNteLinkTarget(linked); linkErr == nil && !samePath(target, linked) {
		modRoot = target
	} else if !directoryWritableOrCreatable(linked) {
		if m == nil || m.appData == nil {
			return nil, errors.New("mod service has no app data store")
		}
		modRoot, err = m.appData.Resolve(appdata.NTEModsDir)
		if err != nil {
			return nil, err
		}
		requiresElevation = true
	}
	return &NtePathResolution{
		GameRootPath:   filepath.Clean(filepath.Join(htRoot, "..", "..")),
		ExecutablePath: executable, ModFolderPath: modRoot, LinkedModFolderPath: linked,
		RequiresElevation: requiresElevation,
	}, nil
}

func findNteExecutable(root string) (string, error) {
	best := ""
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && strings.EqualFold(entry.Name(), nteExecutableName) &&
			(best == "" || len(path) < len(best)) {
			best = path
		}
		return nil
	})
	return best, err
}

func ensureNteFolders(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for _, name := range nteDefaultSubfolders {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func nteRootsFor(game GameConfig) nteRoots {
	root := nteRoots{modRoot: game.ModFolderPath}
	if game.LinkedModFolderPath != nil {
		root.linkedRoot = *game.LinkedModFolderPath
	}
	return root
}

func nteRelative(roots nteRoots, target string) string {
	if pathWithin(roots.modRoot, target) {
		relative, _ := filepath.Rel(roots.modRoot, target)
		return relative
	}
	if roots.linkedRoot != "" && pathWithin(roots.linkedRoot, target) {
		relative, _ := filepath.Rel(roots.linkedRoot, target)
		return relative
	}
	return ""
}

func nteListGroups(roots nteRoots, groupPath string, searchPreview bool, reports ...func(error)) []FolderGroup {
	relative := nteRelative(roots, groupPath)
	groupDir := filepath.Join(roots.modRoot, relative)
	entries, err := os.ReadDir(groupDir)
	if err != nil {
		reportScanFailure(err, reports)
		return []FolderGroup{}
	}
	result := []FolderGroup{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(groupDir, entry.Name())
		if hasDirectPak(path, reports...) || isNtePakWrapper(roots, path, reports...) {
			continue
		}
		mods := collectNteModEntries(roots, path, reports...)
		enabled := 0
		for _, info := range mods {
			if isNteModEnabled(info.path, reports...) {
				enabled++
			}
		}
		result = append(result, FolderGroup{
			Name: entry.Name(), Path: path, Mods: []ModInfo{}, Preview: findPreview(path, searchPreview, reports...),
			ModCount: len(mods), EnabledModCount: enabled, HasSubGroups: hasNteSubGroups(roots, path, reports...),
		})
	}
	less := newLocaleLess()
	sort.SliceStable(result, func(i, j int) bool { return less(result[i].Name, result[j].Name) })
	return result
}

func nteScanGroup(roots nteRoots, groupPath string, searchPreview bool, reports ...func(error)) FolderGroup {
	return nteScanGroupWith(roots, groupPath, searchPreview, func(entry nteModEntry) ModInfo { return nteModInfo(entry, reports...) }, reports...)
}

func nteScanGroupLight(roots nteRoots, groupPath string, searchPreview bool, reports ...func(error)) FolderGroup {
	return nteScanGroupWith(roots, groupPath, searchPreview, func(entry nteModEntry) ModInfo { return nteModInfoLight(entry, reports...) }, reports...)
}

func nteScanGroupWith(roots nteRoots, groupPath string, searchPreview bool, scan func(nteModEntry) ModInfo, reports ...func(error)) FolderGroup {
	relative := nteRelative(roots, groupPath)
	groupDir := filepath.Join(roots.modRoot, relative)
	result := FolderGroup{
		Name: filepath.Base(groupPath), Path: groupDir, Mods: []ModInfo{},
	}
	entries := collectNteModEntries(roots, groupDir, reports...)
	var preview *string
	var mods []ModInfo
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		preview = findPreview(groupDir, searchPreview, reports...)
	}()
	go func() {
		defer wg.Done()
		mods = mapParallel(entries, scan)
		less := newLocaleLess()
		sort.SliceStable(mods, func(i, j int) bool { return less(mods[i].Name, mods[j].Name) })
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

func nteModInfo(entry nteModEntry, reports ...func(error)) ModInfo {
	info := ModInfo{
		ID: entry.path, Name: entry.name, Path: entry.path,
		IsEnabled: isNteModEnabled(entry.path, reports...), Inis: []IniResult{},
	}
	var buckets previewBuckets
	_ = filepath.WalkDir(entry.path, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			reportScanFailure(walkErr, reports)
			return fs.SkipDir
		}
		if !dirEntry.Type().IsRegular() {
			return nil
		}
		metadata, err := dirEntry.Info()
		reportScanFailure(err, reports)
		if err == nil {
			info.Size += float64(metadata.Size())
			if mtime := fileMtimeMS(metadata); mtime > info.Mtime {
				info.Mtime = mtime
			}
		}
		buckets.consider(entry.path, path, dirEntry.Name(), mediaExtensions)
		return nil
	})
	info.Preview = buckets.bestPath()
	if info.Preview == nil && entry.previewFallback != "" {
		info.Preview = findPreview(entry.previewFallback, false, reports...)
	}
	return info
}

func nteModInfoLight(entry nteModEntry, reports ...func(error)) ModInfo {
	info := ModInfo{
		ID: entry.path, Name: entry.name, Path: entry.path,
		IsEnabled: isNteModEnabled(entry.path, reports...), Inis: []IniResult{},
	}
	if preview := findPreviewWalk(entry.path, previewSearchDepth, reports...); preview != nil {
		info.Preview = stringPointer(preview.path)
	}
	if info.Preview == nil && entry.previewFallback != "" {
		info.Preview = findPreview(entry.previewFallback, false, reports...)
	}
	return info
}

func listNteModPaths(roots nteRoots, groupDir string) []string {
	entries := collectNteModEntries(roots, groupDir)
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.path)
	}
	return paths
}

func nteListingGroupPath(roots nteRoots, modPath string) string {
	parent := filepath.Dir(modPath)
	if isNtePakWrapper(roots, parent) {
		return filepath.Dir(parent)
	}
	return parent
}

func collectNteModEntries(roots nteRoots, groupDir string, reports ...func(error)) []nteModEntry {
	entries, err := os.ReadDir(groupDir)
	if err != nil {
		reportScanFailure(err, reports)
		return nil
	}
	result := []nteModEntry{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(groupDir, entry.Name())
		if hasDirectPak(child, reports...) {
			result = append(result, nteModEntry{path: child, name: entry.Name()})
			continue
		}
		if !isNtePakWrapper(roots, child, reports...) {
			continue
		}
		inner, readErr := os.ReadDir(child)
		reportScanFailure(readErr, reports)
		for _, item := range inner {
			innerPath := filepath.Join(child, item.Name())
			if item.IsDir() && hasDirectPak(innerPath, reports...) {
				result = append(result, nteModEntry{
					path: innerPath, name: entry.Name() + " / " + item.Name(), previewFallback: child,
				})
			}
		}
	}
	return result
}

func hasNteSubGroups(roots nteRoots, groupDir string, reports ...func(error)) bool {
	entries, readErr := os.ReadDir(groupDir)
	reportScanFailure(readErr, reports)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(groupDir, entry.Name())
		if !hasDirectPak(path, reports...) && !isNtePakWrapper(roots, path, reports...) {
			return true
		}
	}
	return false
}

func isNtePakWrapper(roots nteRoots, path string, reports ...func(error)) bool {
	relative := nteRelative(roots, path)
	segments := strings.FieldsFunc(filepath.ToSlash(relative), func(r rune) bool { return r == '/' })
	if len(segments) == 1 || (len(segments) == 2 && strings.EqualFold(segments[0], "Character")) {
		return false
	}
	if hasDirectPak(path, reports...) {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		reportScanFailure(err, reports)
		return false
	}
	anyPak := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(path, entry.Name())
		pak := hasDirectPak(child, reports...)
		anyPak = anyPak || pak
		if !pak && hasChildDirectory(child, reports...) {
			return false
		}
	}
	return anyPak
}

func isNteModEnabled(path string, reports ...func(error)) bool {
	if isDisabled(filepath.Base(path)) {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		reportScanFailure(err, reports)
		return false
	}
	foundPak := false
	for _, entry := range entries {
		if entry.Type().IsRegular() && isPakModFile(entry.Name()) {
			foundPak = true
			if !strings.HasSuffix(strings.ToLower(entry.Name()), ".disabled") {
				return true
			}
		}
	}
	return !foundPak
}

func (m *Mod) setNteModEnabled(ctx context.Context, path string, enabled bool) (string, error) {
	if isNteModEnabled(path) == enabled {
		return path, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	type renamePair struct{ from, to string }
	renamed := []renamePair{}
	rollback := func() error {
		var failures []error
		for i := len(renamed) - 1; i >= 0; i-- {
			failures = append(failures, os.Rename(renamed[i].to, renamed[i].from))
		}
		return infra.AnnotateError(errors.Join(failures...), infra.Diagnostic{Stage: "rollback", Fields: map[string]any{"path": path}})
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !isPakModFile(entry.Name()) {
			continue
		}
		from := filepath.Join(path, entry.Name())
		to := from
		if enabled && strings.HasSuffix(strings.ToLower(entry.Name()), ".disabled") {
			to = from[:len(from)-len(".disabled")]
		} else if !enabled && !strings.HasSuffix(strings.ToLower(entry.Name()), ".disabled") {
			to = from + ".disabled"
		}
		if to != from {
			if err := os.Rename(from, to); err != nil {
				return "", infra.WithCause(m.lockedFolderError(err, path), rollback())
			}
			renamed = append(renamed, renamePair{from: from, to: to})
		}
	}
	var result string
	if enabled {
		result, err = m.enable(path)
	} else {
		result, err = m.disable(ctx, path)
	}
	if err != nil {
		return "", infra.WithCause(err, rollback())
	}
	return result, nil
}

func hasDirectPak(path string, reports ...func(error)) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		reportScanFailure(err, reports)
		return false
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && isPakModFile(entry.Name()) {
			return true
		}
	}
	return false
}

func isPakModFile(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".disabled")
	return strings.HasSuffix(name, ".pak")
}

func hasChildDirectory(path string, reports ...func(error)) bool {
	entries, err := os.ReadDir(path)
	reportScanFailure(err, reports)
	for _, entry := range entries {
		if entry.IsDir() {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func directoryWritableOrCreatable(path string) bool {
	testRoot := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		testRoot = filepath.Dir(path)
	}
	info, err := os.Stat(testRoot)
	if err != nil || !info.IsDir() {
		return false
	}
	test, err := os.MkdirTemp(testRoot, ".nahida-write-test-")
	if err != nil {
		return false
	}
	return os.Remove(test) == nil
}

func configureNteModFolder(modRoot string, linkedRoot *string) error {
	if linkedRoot == nil || samePath(modRoot, *linkedRoot) {
		if err := unlinkNteModsFolder(modRoot); err != nil {
			return err
		}
		return ensureNteFolders(modRoot)
	}
	if pathWithin(*linkedRoot, modRoot) || pathWithin(modRoot, *linkedRoot) {
		return errors.New("NTE_CUSTOM_MOD_FOLDER_INSIDE_LINK_PATH")
	}
	if err := ensureNteFolders(modRoot); err != nil {
		return err
	}
	if err := reconcileNteJunction(modRoot, *linkedRoot); err != nil {
		return err
	}
	return ensureNteFolders(modRoot)
}

func cleanupNteModFolder(modRoot string, linkedRoot *string) error {
	if linkedRoot != nil && !samePath(modRoot, *linkedRoot) {
		return unlinkNteModsFolder(*linkedRoot)
	}
	return unlinkNteModsFolder(modRoot)
}

func resolveNteLinkTarget(linkPath string) (string, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	return filepath.Clean(target), nil
}

func hasNtePathChanges(existing db.GamePathRow, updates GameUpdates) bool {
	if !samePath(existing.ModFolderPath, updates.ModFolderPath) {
		return true
	}
	if existing.LinkedModFolderPath == nil && updates.LinkedModFolderPath == nil {
		return false
	}
	if existing.LinkedModFolderPath == nil || updates.LinkedModFolderPath == nil {
		return true
	}
	return !samePath(*existing.LinkedModFolderPath, *updates.LinkedModFolderPath)
}
