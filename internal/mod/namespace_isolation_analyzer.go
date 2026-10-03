package mod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/mod/namespace"
	"nahida.live/desktop/internal/xxmi"
)

type namespaceIsolationFile struct {
	path     string
	relative string
	modPath  string
	content  []byte
	info     os.FileInfo
	document namespace.Document
	implicit bool
	disabled bool
}

type namespaceIsolationInventory struct {
	importer      xxmi.EnabledImporter
	modsRoot      string
	roots         []string
	links         []string
	linked        []string
	mods          []string
	files         []namespaceIsolationFile
	issues        []NamespaceIsolationConflict
	userSnapshots map[string]namespaceIsolationSnapshot
}

type namespaceIsolationSnapshot struct {
	content []byte
	info    os.FileInfo
}

type namespaceIsolationPlan struct {
	conflict NamespaceIsolationConflict
	files    []namespaceIsolationFile
	mods     []string
}

// inventories scans every importer when key is empty. With a key, the other
// importers contribute only the trusted roots that shared-ownership checks need.
func (c *namespaceIsolationCoordinator) inventories(
	ctx context.Context,
	key string,
) ([]namespaceIsolationInventory, error) {
	return c.collectInventories(ctx, key, true)
}

// collectInventories can restrict launch checks to directory boundaries and journals,
// without opening INIs or following their references while automatic isolation is off.
func (c *namespaceIsolationCoordinator) collectInventories(
	ctx context.Context,
	key string,
	scanINIs bool,
) ([]namespaceIsolationInventory, error) {
	if c.owner.xxmi == nil {
		return []namespaceIsolationInventory{}, nil
	}
	importers, err := c.owner.xxmi.GetEnabledImporters(ctx)
	if err != nil {
		return nil, err
	}
	games := []GameConfig{}
	manual := manualSubGroups{}
	if c.owner.client != nil {
		games, err = c.owner.GetGames(ctx)
		if err != nil {
			return nil, err
		}
		manual, err = c.owner.loadManualSubGroups(ctx)
		if err != nil {
			return nil, err
		}
	}
	slices.SortFunc(importers, func(a, b xxmi.EnabledImporter) int { return strings.Compare(a.Key, b.Key) })
	inventories := make([]namespaceIsolationInventory, 0, len(importers))
	for _, importer := range importers {
		if strings.EqualFold(importer.Key, "NTE") {
			continue
		}
		inventory := namespaceIsolationInventory{
			importer:      importer,
			mods:          []string{},
			files:         []namespaceIsolationFile{},
			issues:        []NamespaceIsolationConflict{},
			userSnapshots: map[string]namespaceIsolationSnapshot{},
		}

		// Match the resolved game roots before walking or deriving implicit namespaces.
		// Windows TEMP paths may use 8.3 aliases for the same physical directory.
		root, err := filepath.Abs(importer.ImporterFolder)
		if err == nil {
			root, err = namespacePhysicalPath(root)
		}
		if err != nil {
			inventory.issue("inventory_error", importer.ImporterFolder, err)
		} else {
			importer.ImporterFolder = root
			inventory.importer = importer
		}

		scanned := key == "" || strings.EqualFold(key, importer.Key)
		for _, name := range []string{"d3dx_user.ini", "d3dx.ini"} {
			if !scanned || !scanINIs {
				continue
			}
			content, info, readErr := readNamespaceFile(filepath.Join(importer.ImporterFolder, name))
			if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
				inventory.issue("inventory_error", filepath.Join(importer.ImporterFolder, name), readErr)
			}
			inventory.userSnapshots[name] = namespaceIsolationSnapshot{content: content, info: info}
		}
		trusted := []string{}
		root, err = resolveCompressionRoot(importer.ImporterFolder)
		if err == nil {
			root, err = namespacePhysicalPath(root)
		}
		if err == nil {
			inventory.modsRoot = root
			trusted = append(trusted, root)
		} else {
			inventory.issue("inventory_error", importer.ImporterFolder, err)
		}
		for _, game := range games {
			if game.Importer == nil || !strings.EqualFold(*game.Importer, importer.Key) {
				continue
			}
			root, err := namespacePhysicalPath(game.ModFolderPath)
			if err != nil {
				inventory.issue("inventory_error", game.ModFolderPath, err)
				continue
			}
			trusted = append(trusted, root)
			if !scanned {
				continue
			}
			if err := inventory.boundaries(root, game.Game, manual[game.Game]); err != nil {
				inventory.issue("inventory_error", root, err)
			}
		}
		inventory.mods = namespacePhysicalPaths(inventory.mods)
		inventory.roots = namespacePhysicalPaths(trusted)
		if !scanned || !scanINIs {
			inventories = append(inventories, inventory)
			continue
		}

		// Core, d3dx.ini, and other importer INIs are part of reference inventory.
		scanRoots := namespacePhysicalPaths(append(slices.Clone(inventory.roots), importer.ImporterFolder))
		seen := map[[2]int64][]os.FileInfo{}
		followed := []os.FileInfo{}
		for _, root := range scanRoots {
			if info, err := os.Stat(root); err == nil {
				followed = append(followed, info)
			}
			err := inventory.walk(ctx, root, seen)
			if err != nil {
				inventory.issue("inventory_error", root, err)
			}
		}

		// Links are followed after every root, so a physical path wins over its alias
		// and mod ownership survives. Each directory is entered once, which ends loops.
		for len(inventory.links) > 0 {
			link := inventory.links[0]
			inventory.links = inventory.links[1:]
			info, err := os.Stat(link)
			if err != nil {
				inventory.issue("inventory_error", link, err)
				continue
			}
			if slices.ContainsFunc(followed, func(old os.FileInfo) bool { return os.SameFile(old, info) }) {
				continue
			}
			followed = append(followed, info)
			if target, err := namespacePhysicalPath(link); err == nil {
				inventory.linked = append(inventory.linked, target)
			}
			// WalkDir descends into a linked root only through a trailing separator.
			if err := inventory.walk(ctx, link+string(os.PathSeparator), seen); err != nil {
				inventory.issue("inventory_error", link, err)
			}
		}
		slices.SortFunc(
			inventory.files,
			func(a, b namespaceIsolationFile) int { return strings.Compare(a.path, b.path) },
		)
		inventories = append(inventories, inventory)
	}
	// A single importer gate cannot protect another game consuming the same
	// physical library. Diagnose shared ownership until a multi-gate plan exists.
	for index := range inventories {
		for other := range inventories {
			if index == other || strings.EqualFold(inventories[index].importer.Key, inventories[other].importer.Key) {
				continue
			}
			for _, modPath := range inventories[index].mods {
				shared := slices.ContainsFunc(
					inventories[other].roots,
					func(root string) bool { return pathWithin(root, modPath) },
				)
				if shared {
					inventories[index].issue(
						"shared_importer_mods",
						modPath,
						fmt.Errorf("physical mod is also consumed by importer %s", inventories[other].importer.Key),
					)
				}
			}
		}
	}
	return inventories, nil
}

// namespacePhysicalPath resolves symbolic links and junctions. filepath.EvalSymlinks
// leaves a Windows junction in place, WalkDir does not descend into one, and journaled
// writes reject every reparse point ancestor.
func namespacePhysicalPath(path string) (string, error) {
	for range 32 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}

		// Substitute the junction nearest the volume root, then resolve the result again.
		link, target := "", ""
		for current := resolved; ; current = filepath.Dir(current) {
			info, err := os.Lstat(current)
			if err != nil {
				return "", err
			}
			// Reparse points without a link target, such as cloud placeholders, are physical.
			if isReparsePoint(info) {
				if next, err := os.Readlink(current); err == nil {
					link, target = current, next
				}
			}
			if filepath.Dir(current) == current {
				break
			}
		}
		if link == "" {
			return resolved, nil
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(link), target)
		}
		relative, err := filepath.Rel(link, resolved)
		if err != nil {
			return "", err
		}
		path = filepath.Join(target, relative)
	}
	return "", fmt.Errorf("too many links in %q", path)
}

func namespacePhysicalPaths(paths []string) []string {
	result := []string{}
	infos := []os.FileInfo{}
	for _, path := range paths {
		path, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if slices.ContainsFunc(infos, func(old os.FileInfo) bool { return os.SameFile(old, info) }) {
			continue
		}
		infos, result = append(infos, info), append(result, filepath.Clean(path))
	}
	slices.Sort(result)
	return result
}

func (i *namespaceIsolationInventory) boundaries(root, game string, manual []string) error {
	groups, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var visitGroup func(string) error
	visitGroup = func(group string) error {
		entries, err := os.ReadDir(group)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(group, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info) {
				i.issue("unsafe_path", path, errors.New("nested reparse point cannot establish a mod boundary"))
				continue
			}
			if !info.IsDir() || namespaceIgnoredPath(path) {
				continue
			}
			relative := gameRelativePath(root, path)
			parts := strings.Split(filepath.ToSlash(relative), "/")
			for index := range parts {
				parts[index] = stripDisabled(parts[index])
			}
			if slices.Contains(manual, manualRelativePath(strings.Join(parts, "/"))) {
				if err := visitGroup(path); err != nil {
					return err
				}
				continue
			}
			i.mods = append(i.mods, path)
		}
		return nil
	}
	for _, group := range groups {
		path := filepath.Join(root, group.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info) {
			i.issue("unsafe_path", path, fmt.Errorf("nested reparse point in registered game %s", game))
			continue
		}
		if info.IsDir() && !namespaceIgnoredPath(path) {
			if err := visitGroup(path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (i *namespaceIsolationInventory) walk(
	ctx context.Context,
	root string,
	seen map[[2]int64][]os.FileInfo,
) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if namespaceIgnoredPath(path) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if path != root && (info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info)) {
			target, err := os.Stat(path)
			switch {
			// Journaled writes reject reparse points, so a mod cannot own a link.
			case slices.ContainsFunc(i.mods, func(mod string) bool { return pathWithin(mod, path) }):
				i.issue("unsafe_path", path, errors.New("reparse point inside a mod is not followed"))
			case err != nil:
				i.issue("inventory_error", path, err)
			case target.IsDir():
				if !namespaceDisabledPath(root, path) {
					i.links = append(i.links, path)
				}
			default:
				info = target
			}
			if info != target {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		// desktop.ini is Windows folder metadata, usually in a legacy code page.
		if !info.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".ini") ||
			strings.EqualFold(filepath.Base(path), "d3dx_user.ini") ||
			strings.EqualFold(filepath.Base(path), "desktop.ini") ||
			strings.HasPrefix(strings.ToLower(filepath.Base(path)), "disabled") {
			return nil
		}
		content, snapshot, err := readNamespaceFile(path)
		if err != nil {
			return err
		}
		// Scan roots can nest, so one file may be reached twice. Bucketing by size and
		// modification time keeps the identity comparison off a quadratic path.
		stamp := [2]int64{snapshot.Size(), snapshot.ModTime().UnixNano()}
		if slices.ContainsFunc(seen[stamp], func(old os.FileInfo) bool { return os.SameFile(old, snapshot) }) {
			return nil
		}
		seen[stamp] = append(seen[stamp], snapshot)
		disabled := namespaceDisabledPath(root, path)
		document, err := namespace.Parse(content)
		if err != nil {
			if !disabled {
				i.issue("unsupported_ini", path, err)
			}
			return nil
		}
		for _, include := range append(slices.Clone(document.Includes), document.RecursiveIncludes...) {
			if err := i.validateInclude(filepath.Dir(path), include); err != nil && !disabled {
				i.issue("include_outside_inventory", path, err)
			}
		}
		file := namespaceIsolationFile{
			path: path, content: content, info: snapshot, document: document, disabled: disabled,
		}
		if document.Namespace == "" {
			file.implicit = true
			// A linked Mods root may live on another volume, where Rel to the importer fails.
			relative, err := filepath.Rel(i.importer.ImporterFolder, path)
			if i.modsRoot != "" && pathWithin(i.modsRoot, path) {
				relative, err = filepath.Join("Mods", filepath.FromSlash(gameRelativePath(i.modsRoot, path))), nil
			}
			if err != nil {
				return err
			}
			if !strings.EqualFold(relative, "d3dx.ini") {
				file.document.Namespace = strings.ReplaceAll(filepath.ToSlash(relative), "/", `\`)
			}
		}
		for _, modPath := range i.mods {
			if pathWithin(modPath, path) {
				file.modPath, file.relative = modPath, gameRelativePath(modPath, path)
				break
			}
		}
		i.files = append(i.files, file)
		return nil
	})
}

// namespaceDisabledPath reports whether a folder below root carries the DISABLED prefix.
// 3DMigoto loads nothing below such a folder, so its INIs stay inventoried for their
// mod's manifest but neither collide with nor reference what the game actually loads.
func namespaceDisabledPath(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(strings.Split(relative, string(os.PathSeparator)), func(part string) bool {
		return strings.HasPrefix(strings.ToLower(part), "disabled")
	})
}

func (i *namespaceIsolationInventory) issue(reason, path string, err error) {
	i.issues = append(i.issues, NamespaceIsolationConflict{
		ID: namespaceConflictID(i.importer.Key, reason, path), ImporterKey: i.importer.Key,
		ModPaths: []string{}, INIPaths: []string{path}, Status: "needs_review", Reason: reason, Detail: err.Error(),
	})
}

func namespaceConflictID(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.Join(parts, "\x00"))))
	return hex.EncodeToString(hash[:16])
}

func analyzeNamespaceIsolation(i namespaceIsolationInventory) []namespaceIsolationPlan {
	byKey := map[string][]namespaceIsolationFile{}
	for _, file := range i.files {
		if file.disabled || len(file.document.Persistent) == 0 {
			continue
		}
		for _, variable := range file.document.Persistent {
			key := strings.ToLower(file.document.Namespace + "\x00" + variable)
			byKey[key] = append(byKey[key], file)
		}
	}
	// Merge overlapping collisions so every participant receives one fresh ID
	// and no file is rewritten twice in the same transaction.
	components := []map[string]bool{}
	for _, files := range byKey {
		mods := map[string]bool{}
		for _, file := range files {
			path := file.modPath
			if path == "" {
				path = "@" + file.path
			}
			mods[path] = true
		}
		if len(mods) < 2 {
			continue
		}
		for index := 0; index < len(components); {
			if !namespaceSetsOverlap(mods, components[index]) {
				index++
				continue
			}
			for path := range components[index] {
				mods[path] = true
			}
			components = slices.Delete(components, index, index+1)
			index = 0
		}
		components = append(components, mods)
	}
	plans := []namespaceIsolationPlan{}
	for _, component := range components {
		plan := namespaceIsolationPlan{
			mods:  []string{},
			files: []namespaceIsolationFile{},
			conflict: NamespaceIsolationConflict{
				ImporterKey: i.importer.Key,
				ModPaths:    []string{},
				INIPaths:    []string{},
				Status:      "needs_review",
				Reason:      "namespace_collision",
			},
		}
		for path := range component {
			if strings.HasPrefix(path, "@") {
				plan.conflict.Reason = "unrecognized_mod_boundary"
				plan.conflict.Detail = "A colliding INI is outside a registered physical mod boundary"
				continue
			}
			plan.mods = append(plan.mods, path)
		}
		slices.Sort(plan.mods)
		plan.conflict.ModPaths = slices.Clone(plan.mods)
		for _, file := range i.files {
			if component[file.modPath] || component["@"+file.path] {
				plan.files = append(plan.files, file)
				plan.conflict.INIPaths = append(plan.conflict.INIPaths, file.path)
				if plan.conflict.Namespace == "" && len(file.document.Persistent) > 0 {
					plan.conflict.Namespace = file.document.Namespace
				}
			}
		}
		plan.conflict.ID = namespaceConflictID(i.importer.Key, strings.Join(plan.conflict.INIPaths, "\x00"))
		if plan.conflict.Detail == "" {
			plan.conflict.Detail = "Physical mods share persisted namespace keys"
			if err := namespaceCopiesEquivalent(plan); err != nil {
				plan.conflict.Reason = "different_content"
				plan.conflict.Detail = err.Error()
			}
		}
		for _, file := range plan.files {
			if file.implicit && len(file.document.Persistent) > 0 && plan.conflict.Reason == "namespace_collision" {
				plan.conflict.Reason = "implicit_namespace"
				plan.conflict.Detail = "A persisted collision involves an implicit importer-relative INI namespace"
			}
		}
		// Any unsupported inventory can hide an external reference. Fail closed.
		if len(i.issues) > 0 {
			plan.conflict.Reason = "incomplete_inventory"
			plan.conflict.Detail = "Importer inventory has unreadable or unsafe paths"
			if slices.ContainsFunc(
				i.issues,
				func(issue NamespaceIsolationConflict) bool { return issue.Reason == "shared_importer_mods" },
			) {
				plan.conflict.Reason = "shared_importer_mods"
				plan.conflict.Detail = "Physical mods are consumed by multiple importers"
			}
		}
		for _, file := range i.files {
			// A disabled INI matters only when its own mod is about to be rewritten.
			if file.disabled && !component[file.modPath] {
				continue
			}
			if file.document.Unsupported && (component[file.modPath] || len(file.document.References) > 0) {
				plan.conflict.Reason = "unsupported_reference"
				plan.conflict.Detail = "Unsupported INI syntax or references prevent safe isolation"
			}
			for _, reference := range file.document.References {
				local := false
				for _, candidate := range i.files {
					if file.modPath != "" && candidate.modPath == file.modPath &&
						namespaceReferenceMatches(reference, candidate.document.Namespace) {
						local = true
						break
					}
				}
				if local || plan.conflict.Reason == "unrecognized_mod_boundary" ||
					plan.conflict.Reason == "incomplete_inventory" ||
					plan.conflict.Reason == "shared_importer_mods" {
					continue
				}
				for _, participant := range plan.files {
					if namespaceReferenceMatches(reference, participant.document.Namespace) &&
						file.modPath != participant.modPath {
						plan.conflict.Reason = "external_reference"
						plan.conflict.Detail = "Namespace is referenced outside its physical mod: " + file.path
					}
				}
			}
		}
		plans = append(plans, plan)
	}
	slices.SortFunc(
		plans,
		func(a, b namespaceIsolationPlan) int { return strings.Compare(a.conflict.ID, b.conflict.ID) },
	)
	return plans
}

// Stat on the open handle captures the physical identity before reading. A
// pathname FileInfo can resolve its identity lazily after an atomic replacement.
func readNamespaceFile(path string) ([]byte, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.Join(errors.New("namespace file is not regular"), file.Close())
	}
	content, readErr := io.ReadAll(file)
	after, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(readErr, statErr, closeErr); err != nil {
		return nil, nil, err
	}
	if info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, nil, errors.New("namespace file changed while reading")
	}
	return content, info, nil
}

func namespaceSetsOverlap(a, b map[string]bool) bool {
	for key := range a {
		if b[key] {
			return true
		}
	}
	return false
}

func namespaceReferenceMatches(reference, name string) bool {
	reference = strings.ToLower(strings.Trim(reference, "\\"))
	name = strings.ToLower(strings.Trim(name, "\\"))
	return name != "" && (reference == name || strings.HasPrefix(reference, name+"\\"))
}

func namespaceCopiesEquivalent(plan namespaceIsolationPlan) error {
	var baseline map[string]string
	for _, modPath := range plan.mods {
		mapping, err := namespaceCanonicalMapping(modPath, plan.files)
		if err != nil {
			return err
		}
		for name, canonical := range mapping {
			mapping[name] = strings.ToLower(canonical)
		}
		current := map[string]string{}
		for _, file := range plan.files {
			if file.modPath != modPath {
				continue
			}
			if file.document.Unsupported {
				return fmt.Errorf("unsupported INI: %s", file.path)
			}
			normalized, err := namespace.Normalize(file.content, mapping)
			if err != nil {
				return fmt.Errorf("normalize %s: %w", file.path, err)
			}
			current[strings.ToLower(file.relative)] = normalized
		}
		if baseline == nil {
			baseline = current
			continue
		}
		if len(baseline) != len(current) {
			return errors.New("copies have different INI topology")
		}
		for path, content := range baseline {
			if current[path] != content {
				return fmt.Errorf("copies differ in normalized INI %s", path)
			}
		}
	}
	return nil
}

func (i *namespaceIsolationInventory) validateInclude(baseDir, include string) error {
	include = strings.TrimSpace(strings.Trim(include, "\""))
	if include == "" || strings.ContainsAny(include, "*?%") {
		return fmt.Errorf("unknown include path %q", include)
	}
	path := include
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	resolved, err := namespacePhysicalPath(path)
	if err != nil {
		return fmt.Errorf("resolve include %s: %w", path, err)
	}
	importerRoot, err := namespacePhysicalPath(i.importer.ImporterFolder)
	if err != nil {
		return err
	}
	allowed := pathWithin(importerRoot, resolved) ||
		slices.ContainsFunc(
			append(slices.Clone(i.roots), i.linked...),
			func(root string) bool { return pathWithin(root, resolved) },
		)
	if !allowed {
		return fmt.Errorf("include is outside importer inventory: %s", path)
	}
	return nil
}
