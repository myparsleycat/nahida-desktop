package togglepersist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/mod/namespace"
)

var globalDeclarationRE = regexp.MustCompile(`(?i)^global\s+(persist\s+)?\$([^\s=\\]+)(?:\s*=\s*.*)?$`)

type persistTarget struct {
	iniPath     string
	varName     string
	key         string
	importer    string
	info        os.FileInfo
	fingerprint string
	persistent  bool
	blocked     string
}

type persistTargetIndex map[string][]persistTarget

// Index declarations rather than interpreting namespaces as filesystem paths.
// 3DMigoto loads nothing below a DISABLED name, so a disabled copy neither owns
// a persisted value nor makes the enabled copy of its namespace ambiguous.
func indexPersistTargets(importerFolder string) (persistTargetIndex, error) {
	root, err := filepath.Abs(importerFolder)
	if err != nil {
		return nil, fmt.Errorf("resolve persist importer %q: %w", importerFolder, err)
	}
	index := persistTargetIndex{}
	blockedRoots := map[string]string{}
	mods := filepath.Join(root, "Mods")
	linkedModsRoot := ""
	visit := func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") ||
				strings.HasPrefix(strings.ToLower(entry.Name()), "disabled")) {
				return fs.SkipDir
			}
			if _, err := os.Lstat(filepath.Join(path, ".nhd-namespace")); !errors.Is(err, os.ErrNotExist) {
				journalPath := path
				if linkedModsRoot != "" {
					// Resolve only the trusted Mods boundary; journal validation still
					// rejects reparse points inside each physical mod directory.
					relative, err := filepath.Rel(mods, path)
					if err != nil {
						return err
					}
					journalPath = filepath.Join(linkedModsRoot, relative)
				}
				// The isolation coordinator journals under symlink-resolved roots. This
				// walk follows no link below its roots, so resolving here maps only the
				// symlinked ancestors that journal validation would otherwise reject.
				if resolved, err := filepath.EvalSymlinks(journalPath); err == nil {
					journalPath = resolved
				}
				pending, journalErr := namespace.HasIncompleteTransactions(journalPath)
				if pending || journalErr != nil {
					blockedRoots[path] = "unfinished namespace transaction"
				}
			}
			return nil
		}
		lowerName := strings.ToLower(entry.Name())
		if !entry.Type().IsRegular() || filepath.Ext(lowerName) != ".ini" ||
			strings.HasPrefix(lowerName, "disabled") || lowerName == "d3dx_user.ini" {
			return nil
		}
		content, info, err := readPersistINI(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		namespace := strings.ReplaceAll(relative, string(os.PathSeparator), `\`)
		if strings.EqualFold(relative, "d3dx.ini") {
			namespace = ""
		}
		targets := parsePersistTargets(string(content), namespace)
		fingerprint := ""
		if len(targets) > 0 {
			fingerprint = fingerprintTogglePersistINI(string(content))
		}
		for _, target := range targets {
			target.iniPath = path
			target.importer = root
			target.info = info
			target.fingerprint = fingerprint
			for parent, reason := range blockedRoots {
				if relative, err := filepath.Rel(parent, path); err == nil && relative != ".." &&
					!strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
					target.blocked = reason
					break
				}
			}
			index[target.key] = append(index[target.key], target)
		}
		return nil
	}
	if err := filepath.WalkDir(root, visit); err != nil {
		return nil, fmt.Errorf("index persist declarations in %q: %w", root, err)
	}

	// XXMI may link its Mods directory to the user's external mod collection.
	// Windows junctions have ModeIrregular rather than ModeSymlink. Directory
	// reparse points already visited by the initial walk must not be indexed twice.
	// Follow only this explicit root; nested links and linked INI files are not
	// traversed, so a namespace cannot redirect writes through an arbitrary link.
	if info, err := os.Lstat(mods); err == nil && !info.IsDir() &&
		info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		linkedModsRoot, err = os.Readlink(mods)
		if err != nil {
			return nil, fmt.Errorf("resolve linked persist mods %q: %w", mods, err)
		}
		if !filepath.IsAbs(linkedModsRoot) {
			linkedModsRoot = filepath.Join(root, linkedModsRoot)
		}
		if err := filepath.WalkDir(mods+string(os.PathSeparator), visit); err != nil {
			return nil, fmt.Errorf("index linked persist mods %q: %w", mods, err)
		}
	}
	return index, nil
}

func parsePersistTargets(content, namespace string) []persistTarget {
	targets := []persistTarget{}
	preamble := true
	inConstants := false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			preamble = false
			inConstants = strings.EqualFold(trimmed, "[Constants]")
			continue
		}
		if preamble {
			key, value, found := strings.Cut(trimmed, "=")
			if found && strings.EqualFold(strings.TrimSpace(key), "namespace") {
				if comment := strings.IndexAny(value, ";#"); comment >= 0 {
					value = value[:comment]
				}
				namespace = strings.TrimSpace(value)
			}
			continue
		}
		if !inConstants {
			continue
		}
		match := globalDeclarationRE.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		key := "$" + match[2]
		if namespace != "" {
			key = `$\` + namespace + `\` + match[2]
		}
		targets = append(targets, persistTarget{
			key: strings.ToLower(key), varName: match[2], persistent: match[1] != "",
		})
	}
	return targets
}

func (index persistTargetIndex) resolve(key string) (*persistTarget, error) {
	targets := index[strings.ToLower(key)]
	for _, target := range targets {
		if target.blocked != "" {
			return nil, fmt.Errorf("persist variable %s blocked in %s: %s", key, target.iniPath, target.blocked)
		}
	}
	if len(targets) > 1 {
		paths := make([]string, len(targets))
		for i, target := range targets {
			paths[i] = target.iniPath
		}
		return nil, fmt.Errorf("ambiguous persist variable %s declared in %s", key, strings.Join(paths, ", "))
	}
	if len(targets) == 0 || !targets[0].persistent {
		return nil, nil
	}
	target := targets[0]
	return &target, nil
}

func readPersistINI(path string) ([]byte, os.FileInfo, error) {
	// The walk that indexes declarations is sequential, but it overlaps scans and
	// uploads on the same disk. A background context never fails the wait.
	release, _ := diskio.Acquire(context.Background(), path)
	defer release()

	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	// File.Stat captures Windows file identity from the open handle. os.Stat
	// defers that lookup until SameFile, when the path may belong to another mod.
	info, statErr := file.Stat()
	content, readErr := io.ReadAll(file)
	if err := errors.Join(statErr, readErr, file.Close()); err != nil {
		return nil, nil, fmt.Errorf("read persist ini %q: %w", path, err)
	}
	return content, info, nil
}
