//go:build windows

package xxmi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/elevated"
)

// gameFilePublisher carries edits into a game folder this process cannot write to.
type gameFilePublisher struct {
	// apply changes files with administrator rights.
	apply func(ctx context.Context, ops []elevated.FileOp) error
	// reportCleanup receives a failure to remove the private copy of an edit; nil drops it.
	reportCleanup func(err error)
}

// editGameFolder runs edit on folder. A game installed under a protected location such as Program
// Files rejects writes from this process, so edit then runs on a private copy of the files owns
// selects, and publish applies whatever it changed there.
func editGameFolder(
	ctx context.Context,
	folder string,
	owns func(name string) bool,
	publish *gameFilePublisher,
	edit func(folder string) error,
) error {
	if publish == nil || !writeDenied(folder) {
		return edit(folder)
	}

	staging, err := os.MkdirTemp("", "nahida-game-files-")
	if err != nil {
		return err
	}

	// A private copy that cannot be removed is not a failed edit: by then the game folder already
	// holds the result.
	defer func() {
		if err := os.RemoveAll(staging); err != nil && publish.reportCleanup != nil {
			publish.reportCleanup(err)
		}
	}()
	entries, err := os.ReadDir(folder)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Names are compared without case, like the file system does: an edit that rewrites a file
	// under different casing must not look like one new file and one removed file.
	type gameFile struct{ name, digest string }
	original := make(map[string]gameFile)
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !owns(entry.Name()) {
			continue
		}
		digest, err := stageGameFile(filepath.Join(folder, entry.Name()), filepath.Join(staging, entry.Name()))
		if err != nil {
			return err
		}
		original[strings.ToLower(entry.Name())] = gameFile{name: entry.Name(), digest: digest}
	}

	if err := edit(staging); err != nil {
		return err
	}

	// Copies go first, so a failure part-way never leaves the game folder without a file that was
	// only waiting to be replaced.
	staged, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	var ops []elevated.FileOp
	for _, entry := range staged {
		if !entry.Type().IsRegular() {
			continue
		}
		file, err := verifiedLaunchFile(filepath.Join(staging, entry.Name()))
		if err != nil {
			return err
		}
		previous, existed := original[strings.ToLower(entry.Name())]
		delete(original, strings.ToLower(entry.Name()))
		if existed && previous.digest == file.SHA256 {
			continue
		}
		ops = append(ops, elevated.FileOp{
			Kind: elevated.FileOpCopy, Source: file.Path, Target: filepath.Join(folder, entry.Name()),
			SHA256: file.SHA256,
		})
	}
	for _, key := range slices.Sorted(maps.Keys(original)) {
		ops = append(ops, elevated.FileOp{
			Kind: elevated.FileOpRemove, Target: filepath.Join(folder, original[key].name),
		})
	}
	if len(ops) == 0 {
		return nil
	}
	return publish.apply(ctx, ops)
}

// writeDenied reports whether this process lacks the permission to create files in folder, or in
// its nearest existing ancestor when folder does not exist yet.
func writeDenied(folder string) bool {
	existing := folder
	for {
		if info, err := os.Stat(existing); err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return false
		}
		existing = parent
	}
	probe, err := os.CreateTemp(existing, ".nahida-write-")
	if err != nil {
		return errors.Is(err, os.ErrPermission)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return false
}

// stageGameFile copies source to target and returns its SHA-256. The modification time is kept
// because edits pick the newest of several candidate files by it.
func stageGameFile(source, target string) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(output, digest), input)
	if err := errors.Join(copyErr, output.Close()); err != nil {
		return "", err
	}
	if err := os.Chtimes(target, info.ModTime(), info.ModTime()); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
