package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/drive"
)

// The kinds of backup target.
const (
	TargetKindGame   = "game"
	TargetKindCustom = "custom"
)

// Target is one folder the backup covers or could cover.
type Target struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Game     *string `json:"game"`
	Label    string  `json:"label"`
	Path     string  `json:"path"`
	Excluded bool    `json:"excluded"`
	Missing  bool    `json:"missing"`
}

// scannedFile is one file a scan found and the filter let through.
type scannedFile struct {
	target   int
	relPath  string
	fullPath string
	size     int64
	modified time.Time
	sha256   string
}

// scanResult is every file of the included targets, and what was left out by
// reason.
type scanResult struct {
	files   []scannedFile
	skipped map[string]int32
}

// gameTargetID names the target of a registered game.
func gameTargetID(game string) string {
	return "game:" + game
}

// listTargets answers the mod folders of the registered games followed by the
// folders the user added, each marked excluded or missing.
func listTargets(games []db.GamePathRow, custom []db.BackupCustomPathRow, excluded []string) []Target {
	targets := make([]Target, 0, len(games)+len(custom))
	for _, row := range games {
		game := row.Game
		targets = append(targets, Target{
			ID:       gameTargetID(game),
			Kind:     TargetKindGame,
			Game:     &game,
			Label:    game,
			Path:     row.ModFolderPath,
			Excluded: slices.Contains(excluded, game),
			Missing:  !isDirectory(row.ModFolderPath),
		})
	}
	for _, row := range custom {
		targets = append(targets, Target{
			ID:      row.ID,
			Kind:    TargetKindCustom,
			Label:   row.Label,
			Path:    row.Path,
			Missing: !isDirectory(row.Path),
		})
	}
	return targets
}

// includedTargets keeps the targets a run backs up: not excluded, present on
// disk, and not inside another included target, whose walk already covers it.
func includedTargets(targets []Target) []Target {
	candidates := lo.Filter(targets, func(target Target, _ int) bool {
		return !target.Excluded && !target.Missing
	})
	return lo.Filter(candidates, func(target Target, index int) bool {
		for other, candidate := range candidates {
			if other == index {
				continue
			}
			if isWithin(target.Path, candidate.Path) && (!samePath(target.Path, candidate.Path) || other < index) {
				return false
			}
		}
		return true
	})
}

func isDirectory(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func normalizedPath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

func samePath(a, b string) bool {
	return normalizedPath(a) == normalizedPath(b)
}

// isWithin reports whether path is root or lies below it.
func isWithin(path, root string) bool {
	path, root = normalizedPath(path), normalizedPath(root)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, strings.TrimRight(root, `\/`)+string(filepath.Separator))
}

// scanTargets walks every target. Links and junctions are not followed, so a
// folder linked into a mod folder is neither counted twice nor looped through.
// filter answers why a file is left out, or "" when it is kept.
func scanTargets(
	ctx context.Context,
	targets []Target,
	filter func(name string, size int64) string,
) (scanResult, error) {
	result := scanResult{skipped: map[string]int32{}}
	for index, target := range targets {
		// The walk starts from the folder with a trailing separator, which makes
		// its first Lstat follow a junction or link: a mod folder that is itself
		// linked elsewhere is walked. Links below it are still not followed.
		root := strings.TrimRight(target.Path, `\/`) + string(filepath.Separator)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				if path == root {
					return err
				}
				return fmt.Errorf("%w: %s: %w", ErrUnreadable, path, err)
			}
			if path != root && entry.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				result.skipped["link"]++
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return fmt.Errorf("%w: %s: %w", ErrUnreadable, path, infoErr)
			}
			if reason := filter(entry.Name(), info.Size()); reason != "" {
				result.skipped[reason]++
				return nil
			}
			relPath, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return fmt.Errorf("%w: %s: %w", ErrUnreadable, path, relErr)
			}
			result.files = append(result.files, scannedFile{
				target:   index,
				relPath:  filepath.ToSlash(relPath),
				fullPath: path,
				size:     info.Size(),
				modified: info.ModTime().UTC(),
			})
			return nil
		})
		if err != nil {
			return scanResult{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
		}
	}
	return result, nil
}

// hashFiles fills in the content hash of every file. A file whose size and
// modification time match the cache keeps its cached hash; the others are read
// and the cache learns them. A file that vanished or cannot be read since the
// scan is answered by its index instead of failing the run.
func hashFiles(
	ctx context.Context,
	cache db.BackupFileCacheStore,
	files []scannedFile,
	concurrency int,
	onProgress func(done int),
) ([]int, error) {
	cached, err := cache.GetMany(ctx, lo.Map(files, func(file scannedFile, _ int) string { return file.fullPath }))
	if err != nil {
		return nil, err
	}

	pending := make([]drive.UploadFile, 0)
	pendingIndex := make(map[string]int)
	for index := range files {
		file := &files[index]
		if row, ok := cached[file.fullPath]; ok && row.Size == file.size && row.Mtime == file.modified.UnixNano() {
			file.sha256 = row.SHA256
			continue
		}
		fid := strconv.Itoa(index)
		pendingIndex[fid] = index
		pending = append(
			pending,
			drive.UploadFile{FID: fid, Name: filepath.Base(file.fullPath), Size: file.size, FullPath: file.fullPath},
		)
	}
	reused := len(files) - len(pending)
	if onProgress != nil {
		onProgress(reused)
	}
	if len(pending) == 0 {
		return nil, nil
	}

	hashed, err := drive.HashUploadFiles(ctx, pending, concurrency, func(done int) {
		if onProgress != nil {
			onProgress(reused + done)
		}
	})
	var unreadable []int
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// One unreadable file stops the batch, so the files are hashed one by
		// one to tell which of them cannot be read.
		hashed = hashed[:0]
		for done, file := range pending {
			one, oneErr := drive.HashUploadFiles(ctx, []drive.UploadFile{file}, 1, nil)
			switch {
			case ctx.Err() != nil:
				return nil, ctx.Err()
			case oneErr != nil:
				unreadable = append(unreadable, pendingIndex[file.FID])
			default:
				hashed = append(hashed, one...)
			}
			if onProgress != nil {
				onProgress(reused + done + 1)
			}
		}
	}

	rows := make([]db.BackupFileCacheRow, 0, len(hashed))
	for _, result := range hashed {
		if result.SHA256 == "" {
			continue
		}
		file := &files[pendingIndex[result.FID]]
		file.sha256 = result.SHA256
		rows = append(rows, db.BackupFileCacheRow{
			Path: file.fullPath, Size: file.size, Mtime: file.modified.UnixNano(), SHA256: file.sha256,
		})
	}
	return unreadable, cache.UpsertMany(ctx, rows)
}

// manifestDigest names the content of a scan: which target holds which path
// with which bytes. Two scans with the same digest back up the same thing, so
// the server skips the second.
func manifestDigest(targets []Target, files []scannedFile) string {
	lines := make([]string, 0, len(files)+len(targets))
	for _, target := range targets {
		lines = append(lines, "target\x00"+targetKey(target)+"\x00"+target.Label+"\x00"+target.Path)
	}
	for _, file := range files {
		lines = append(lines, "file\x00"+targetKey(targets[file.target])+"\x00"+file.relPath+"\x00"+
			file.sha256+"\x00"+strconv.FormatInt(file.size, 10)+"\x00"+strconv.FormatInt(file.modified.UnixNano(), 10))
	}
	slices.Sort(lines)

	hash := sha256.New()
	for _, line := range lines {
		hash.Write([]byte(line))
		hash.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func targetKey(target Target) string {
	if target.Game != nil {
		return TargetKindGame + ":" + *target.Game
	}
	return TargetKindCustom + ":" + normalizedPath(target.Path)
}
