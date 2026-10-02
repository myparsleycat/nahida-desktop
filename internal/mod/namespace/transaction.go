package namespace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/mod/metadata"
	"nahida.live/desktop/internal/platform"
)

var (
	ErrConflict = errors.New("namespace transaction conflict")
	ErrRecovery = errors.New("namespace transaction recovery failed")
	ErrJournal  = errors.New("invalid namespace transaction journal")
)

// errUnprepared marks a transaction directory whose journal was never saved.
// Replacements start only after every participant's journal is durable, so such a
// directory is the residue of an interrupted preparation and changed no mod file.
var errUnprepared = errors.New("namespace transaction was never prepared")

const transactionDirectory = ".nhd-namespace/transactions"

// Change contains complete byte snapshots, including metadata with unknown fields.
// Info should come from the same open file's Stat as Before: Windows os.Stat may
// defer loading its file identity until a later SameFile call.
type Change struct {
	ModPath      string
	RelativePath string
	Before       []byte
	After        []byte
	Exists       bool
	Info         os.FileInfo
}

// Recovery reports an unfinished transaction found in one physical mod directory.
type Recovery struct {
	ModPath string
	Err     error
}

type transactionFile struct {
	Path       string      `json:"path"`
	Exists     bool        `json:"exists"`
	Mode       os.FileMode `json:"mode"`
	BeforeHash string      `json:"beforeHash"`
	AfterHash  string      `json:"afterHash"`
	BeforeID   string      `json:"beforeId"`
	AfterID    string      `json:"afterId"`
	RestoreID  string      `json:"restoreId,omitempty"`
}

type transactionJournal struct {
	Version      int               `json:"version"`
	ID           string            `json:"id"`
	Status       string            `json:"status"`
	RootID       string            `json:"rootId"`
	Participants []string          `json:"participants"`
	Files        []transactionFile `json:"files"`
}

type localTransaction struct {
	root    string
	dir     string
	journal transactionJournal
	changes []Change
	created bool
}

// transactionFault is a test seam at durable IO boundaries. Tests using it must not run in parallel.
var transactionFault = func(stage, path string) error { return nil }

// Apply commits complete files across mods. validate runs under metadata reservations and
// must not call metadata operations on these directories. Backups remain after completion.
func Apply(ctx context.Context, changes []Change, validate func() error) error {
	return ApplyVerified(ctx, changes, validate, nil)
}

// ApplyVerified additionally verifies the applied graph before committing. Both
// callbacks run under directory reservations and must not call metadata operations.
func ApplyVerified(ctx context.Context, changes []Change, validate, verify func() error) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	locals, paths, err := groupChanges(changes)
	if err != nil {
		return err
	}
	release, err := metadata.ReserveTransaction(paths...)
	if err != nil {
		return err
	}
	defer release()
	writesStarted := false
	defer func() {
		if !writesStarted {
			for _, local := range locals {
				if local.created {
					result = errors.Join(result, discardPreparation(local))
				}
			}
		}
	}()

	for _, local := range locals {
		if err := checkChanges(local); err != nil {
			return err
		}
		if err := requireFinished(local.root); err != nil {
			return err
		}
	}
	for _, local := range locals {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := prepareTransaction(local); err != nil {
			return err
		}
	}
	// Recheck the entire plan after preparing durable journals, then the caller's
	// domain validation immediately before the first replacement.
	for _, local := range locals {
		if err := checkChanges(local); err != nil {
			return err
		}
	}
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	writesStarted = true
	for _, local := range locals {
		for i := range local.journal.Files {
			if err := applyFile(ctx, local, i); err != nil {
				return rollbackTransactions(locals, err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return rollbackTransactions(locals, err)
	}
	if verify != nil {
		if err := verify(); err != nil {
			return rollbackTransactions(locals, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return rollbackTransactions(locals, err)
	}
	for _, local := range locals {
		if err := ctx.Err(); err != nil {
			return rollbackTransactions(locals, err)
		}
		local.journal.Status = "committed"
		if err := saveJournal(local); err != nil {
			return rollbackTransactions(locals, err)
		}
		if err := ctx.Err(); err != nil {
			return rollbackTransactions(locals, err)
		}
	}
	return nil
}

// HasIncompleteTransactions lets indexers exclude unresolved physical mods. An
// invalid or unreadable journal is an error, never evidence that the mod is safe.
// This read-only discovery helper does not reserve metadata directories.
func HasIncompleteTransactions(modPath string) (bool, error) {
	root, err := filepath.Abs(modPath)
	if err != nil {
		return true, err
	}
	err = requireFinished(root)
	if errors.Is(err, ErrRecovery) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return false, nil
}

// Recover restores pending local journals without looking up other participants.
// A copied directory establishes a new identity baseline, but still requires exact
// known bytes. In the original directory file identities must match the journal.
// When multiple supplied mods share a transaction, any unfinished participant
// makes recovery roll back all supplied participants, including committed ones.
func Recover(ctx context.Context, modPaths []string) ([]Recovery, error) {
	return RecoverVerified(ctx, modPaths, nil)
}

// RecoverVerified checks validate after acquiring all metadata reservations and
// again before each physical mod's recovery. The callback must not call metadata
// operations on reserved directories. A validation failure leaves remaining
// journals untouched and returns the original error alongside prior results.
func RecoverVerified(ctx context.Context, modPaths []string, validate func() error) ([]Recovery, error) {
	results := make([]Recovery, 0)
	roots := make([]string, 0, len(modPaths))
	seen := make(map[string]bool)
	for _, path := range modPaths {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		root, err := filepath.Abs(path)
		if err != nil {
			return results, err
		}
		key := strings.ToLower(root)
		if seen[key] {
			continue
		}
		seen[key] = true
		roots = append(roots, root)
	}
	release, err := metadata.ReserveTransaction(roots...)
	if err != nil {
		return results, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return results, err
	}
	if validate != nil {
		if err := validate(); err != nil {
			return results, err
		}
	}
	// A caller supplying multiple physical participants can resolve a partially
	// recorded commit as one rollback. Standalone recovery cannot infer absent peers.
	unfinished := make(map[string]bool)
	for _, root := range roots {
		dir, err := checkedPath(root, transactionDirectory)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			local, err := loadJournal(root, entry.Name())
			if errors.Is(err, errUnprepared) {
				continue
			}
			if err != nil || local.journal.Status == "pending" {
				unfinished[entry.Name()] = true
			}
		}
	}
	var resultErr error
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return results, errors.Join(resultErr, err)
		}
		if validate != nil {
			if err := validate(); err != nil {
				return results, errors.Join(resultErr, err)
			}
		}
		found, err := recoverMod(ctx, root, unfinished)
		results = append(results, found...)
		resultErr = errors.Join(resultErr, err)
	}
	return results, resultErr
}

func groupChanges(changes []Change) ([]*localTransaction, []string, error) {
	locals := make([]*localTransaction, 0)
	paths := make([]string, 0)
	byRoot := make(map[string]*localTransaction)
	seen := make(map[string]bool)
	id := uuid.NewString()
	for _, change := range changes {
		root, err := filepath.Abs(change.ModPath)
		if err != nil {
			return nil, nil, err
		}
		if err := safeRelative(change.RelativePath); err != nil {
			return nil, nil, err
		}
		if !change.Exists && (len(change.Before) != 0 || change.Info != nil) {
			return nil, nil, fmt.Errorf("%w: invalid missing-file snapshot", ErrConflict)
		}
		if strings.EqualFold(strings.Split(filepath.ToSlash(change.RelativePath), "/")[0], ".nhd-namespace") {
			return nil, nil, fmt.Errorf("%w: reserved path", ErrConflict)
		}
		key := strings.ToLower(root)
		fileKey := key + "/" + strings.ToLower(filepath.Clean(change.RelativePath))
		if seen[fileKey] {
			return nil, nil, fmt.Errorf("%w: duplicate %s", ErrConflict, fileKey)
		}
		seen[fileKey] = true
		local := byRoot[key]
		if local == nil {
			local = &localTransaction{root: root, dir: filepath.Join(root, transactionDirectory, id),
				journal: transactionJournal{Version: 1, ID: id, Status: "pending", Files: []transactionFile{}}}
			byRoot[key] = local
			locals = append(locals, local)
			paths = append(paths, root)
		}
		change.ModPath = root
		change.Before = bytes.Clone(change.Before)
		change.After = bytes.Clone(change.After)
		local.changes = append(local.changes, change)
	}
	participants := make([]string, len(locals))
	for i := range participants {
		participants[i] = uuid.NewString()
	}
	for _, local := range locals {
		local.journal.Participants = participants
	}
	return locals, paths, nil
}

func checkChanges(local *localTransaction) error {
	for _, change := range local.changes {
		path, err := checkedPath(local.root, change.RelativePath)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if !change.Exists && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%w: stat %s: %w", ErrConflict, path, err)
		}
		if !change.Exists || change.Info == nil || !info.Mode().IsRegular() || !sameSnapshotInfo(change.Info, info) {
			return fmt.Errorf("%w: identity %s", ErrConflict, path)
		}
		if info.Mode().Perm()&0o222 == 0 {
			return fmt.Errorf("%w: read-only file %s", ErrConflict, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, change.Before) || info.Mode().Perm() != change.Info.Mode().Perm() {
			return fmt.Errorf("%w: contents or mode %s", ErrConflict, path)
		}
	}
	return nil
}

func prepareTransaction(local *localTransaction) error {
	rootID, err := fileIdentity(local.root)
	if err != nil {
		return err
	}
	local.journal.RootID = rootID
	for _, relative := range []string{".nhd-namespace", transactionDirectory, filepath.Join(transactionDirectory, local.journal.ID)} {
		path, err := checkedPath(local.root, relative)
		if err != nil {
			return err
		}
		if err := transactionFault("mkdir", path); err != nil {
			return err
		}
		mkdirErr := os.Mkdir(path, 0o700)
		if path == local.dir {
			if mkdirErr != nil {
				return mkdirErr
			}
			local.created = true
		} else if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return mkdirErr
		}
		if err := hideTransactionFile(path); err != nil {
			return err
		}
	}
	for i, change := range local.changes {
		entry := transactionFile{Path: filepath.ToSlash(change.RelativePath), Exists: change.Exists,
			Mode: 0o644, BeforeHash: contentHash(change.Before), AfterHash: contentHash(change.After)}
		if change.Exists {
			entry.Mode = change.Info.Mode().Perm()
			entry.BeforeID, err = fileIdentity(filepath.Join(local.root, change.RelativePath))
			if err != nil {
				return err
			}
		}
		if err := durableFile(local.backup(i, "before"), change.Before, 0o600); err != nil {
			return err
		}
		if err := durableFile(local.backup(i, "after"), change.After, 0o600); err != nil {
			return err
		}
		local.journal.Files = append(local.journal.Files, entry)
	}
	return saveJournal(local)
}

func (local *localTransaction) backup(index int, kind string) string {
	return filepath.Join(local.dir, strconv.Itoa(index)+"."+kind+".bin")
}

func applyFile(ctx context.Context, local *localTransaction, index int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry := &local.journal.Files[index]
	path, err := checkedPath(local.root, entry.Path)
	if err != nil {
		return err
	}
	if err := transactionFault("apply", path); err != nil {
		return err
	}
	// Each replacement also checks its snapshot: another writer may have edited a
	// later file after the whole-plan validation.
	if err := checkChanges(&localTransaction{root: local.root, changes: []Change{local.changes[index]}}); err != nil {
		return err
	}
	stage, err := stageFile(local, index, local.changes[index].After)
	if err != nil {
		return err
	}
	entry.AfterID, err = fileIdentity(stage)
	if err != nil {
		return err
	}
	if err := saveJournal(local); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkChanges(&localTransaction{root: local.root, changes: []Change{local.changes[index]}}); err != nil {
		return err
	}
	if err := transactionFault("replace", path); err != nil {
		return err
	}
	return mutateSnapshot(local.root, entry.Path, local.changes[index].Before, entry.BeforeID, entry.Exists, stage)
}

func stageFile(local *localTransaction, index int, data []byte) (string, error) {
	path := filepath.Join(local.dir, uuid.NewString()+".stage")
	if _, err := checkedPath(local.root, mustRelative(local.root, path)); err != nil {
		return "", err
	}
	if err := durableFile(path, data, local.journal.Files[index].Mode); err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Base(local.journal.Files[index].Path), "nhd.json") {
		if err := hideTransactionFile(path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func rollbackTransactions(locals []*localTransaction, cause error) error {
	var rollbackErr error
	for i := len(locals) - 1; i >= 0; i-- {
		local := locals[i]
		local.journal.Status = "pending"
		if err := saveJournal(local); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
		if err := restoreTransaction(local); err != nil {
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	if rollbackErr != nil {
		return errors.Join(cause, fmt.Errorf("%w: %w", ErrRecovery, rollbackErr))
	}
	return cause
}

func restoreTransaction(local *localTransaction) error {
	rootID, err := fileIdentity(local.root)
	if err != nil {
		return err
	}
	copied := rootID != local.journal.RootID
	var result error
	for i := len(local.journal.Files) - 1; i >= 0; i-- {
		if err := restoreFile(local, i, copied); err != nil {
			result = errors.Join(result, err)
		}
	}
	if result != nil {
		return result
	}
	local.journal.Status = "rolledback"
	return saveJournal(local)
}

func restoreFile(local *localTransaction, index int, copied bool) error {
	entry := &local.journal.Files[index]
	path, err := checkedPath(local.root, entry.Path)
	if err != nil {
		return err
	}
	before, err := readBackup(local, index, "before", entry.BeforeHash)
	if err != nil {
		return err
	}
	after, err := readBackup(local, index, "after", entry.AfterHash)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !entry.Exists {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: read %s: %w", ErrConflict, path, err)
	}
	id, err := fileIdentity(path)
	if err != nil {
		return err
	}
	knownID := copied || id == entry.BeforeID || id == entry.AfterID || id == entry.RestoreID
	if !knownID || (!bytes.Equal(data, before) && !bytes.Equal(data, after)) {
		return fmt.Errorf("%w: externally edited %s", ErrConflict, path)
	}
	if entry.Exists && bytes.Equal(data, before) {
		return nil
	}
	if err := transactionFault("rollback", path); err != nil {
		return err
	}
	if !entry.Exists {
		return mutateSnapshot(local.root, entry.Path, data, id, true, "")
	}
	stage, err := stageFile(local, index, before)
	if err != nil {
		return err
	}
	entry.RestoreID, err = fileIdentity(stage)
	if err != nil {
		return err
	}
	if err := saveJournal(local); err != nil {
		return err
	}
	// Repeat the comparison after staging and journal IO; never restore over edits.
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	currentID, err := fileIdentity(path)
	if err != nil {
		return err
	}
	if currentID != id || !bytes.Equal(current, data) {
		return fmt.Errorf("%w: %s", ErrConflict, path)
	}
	return mutateSnapshot(local.root, entry.Path, data, id, true, stage)
}

func readBackup(local *localTransaction, index int, kind, hash string) ([]byte, error) {
	path := local.backup(index, kind)
	if _, err := checkedPath(local.root, mustRelative(local.root, path)); err != nil {
		return nil, err
	}
	if _, err := fileIdentity(path); err != nil {
		return nil, fmt.Errorf("%w: backup identity %s: %w", ErrJournal, path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: backup %s: %w", ErrJournal, path, err)
	}
	if contentHash(data) != hash {
		return nil, fmt.Errorf("%w: backup hash %s", ErrJournal, path)
	}
	return data, nil
}

func recoverMod(ctx context.Context, root string, unfinished map[string]bool) ([]Recovery, error) {
	results := make([]Recovery, 0)
	dir, err := checkedPath(root, transactionDirectory)
	if err != nil {
		return []Recovery{{ModPath: root, Err: err}}, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return results, nil
	}
	if err != nil {
		return []Recovery{{ModPath: root, Err: err}}, err
	}
	var resultErr error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return results, errors.Join(resultErr, err)
		}
		local, err := loadJournal(root, entry.Name())
		if errors.Is(err, errUnprepared) {
			continue
		}
		if err == nil &&
			(local.journal.Status == "rolledback" || (local.journal.Status != "pending" && !unfinished[entry.Name()])) {
			continue
		}
		if err == nil {
			if local.journal.Status == "committed" {
				local.journal.Status = "pending"
				err = saveJournal(local)
			}
		}
		if err == nil {
			err = restoreTransaction(local)
		}
		if err != nil {
			err = fmt.Errorf("%w: %s: %w", ErrRecovery, entry.Name(), err)
		}
		results = append(results, Recovery{ModPath: root, Err: err})
		resultErr = errors.Join(resultErr, err)
	}
	return results, resultErr
}

func requireFinished(root string) error {
	dir, err := checkedPath(root, transactionDirectory)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		local, err := loadJournal(root, entry.Name())
		if errors.Is(err, errUnprepared) {
			continue
		}
		if err != nil {
			return err
		}
		if local.journal.Status == "pending" {
			return fmt.Errorf("%w: recover %s first", ErrRecovery, root)
		}
	}
	return nil
}

func loadJournal(root, id string) (*localTransaction, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("%w: transaction id", ErrJournal)
	}
	relative := filepath.Join(transactionDirectory, id, "journal.json")
	path, err := checkedPath(root, relative)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errUnprepared
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read journal: %w", ErrJournal, err)
	}
	local := &localTransaction{root: root, dir: filepath.Dir(path)}
	if err := json.Unmarshal(data, &local.journal); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJournal, err)
	}
	j := &local.journal
	if j.Version != 1 || j.ID != id || j.RootID == "" || len(j.Files) == 0 || len(j.Participants) == 0 {
		return nil, fmt.Errorf("%w: header or unsupported version", ErrJournal)
	}
	if j.Status != "pending" && j.Status != "committed" && j.Status != "rolledback" {
		return nil, fmt.Errorf("%w: status", ErrJournal)
	}
	if !validIdentity(j.RootID) {
		return nil, fmt.Errorf("%w: root identity", ErrJournal)
	}
	participants := make(map[string]bool)
	for _, participant := range j.Participants {
		if _, err := uuid.Parse(participant); err != nil || participants[participant] {
			return nil, fmt.Errorf("%w: participants", ErrJournal)
		}
		participants[participant] = true
	}
	seen := make(map[string]bool)
	for _, entry := range j.Files {
		if err := safeRelative(entry.Path); err != nil {
			return nil, err
		}
		key := strings.ToLower(filepath.Clean(entry.Path))
		if strings.EqualFold(strings.Split(filepath.ToSlash(entry.Path), "/")[0], ".nhd-namespace") || seen[key] {
			return nil, fmt.Errorf("%w: reserved or duplicate path", ErrJournal)
		}
		seen[key] = true
		if entry.Mode != entry.Mode.Perm() || len(entry.BeforeHash) != 64 || len(entry.AfterHash) != 64 {
			return nil, fmt.Errorf("%w: file record", ErrJournal)
		}
		for _, hash := range []string{entry.BeforeHash, entry.AfterHash} {
			if _, err := hex.DecodeString(hash); err != nil {
				return nil, fmt.Errorf("%w: hash", ErrJournal)
			}
		}
		if entry.Exists != (entry.BeforeID != "") {
			return nil, fmt.Errorf("%w: original identity", ErrJournal)
		}
		for _, id := range []string{entry.BeforeID, entry.AfterID, entry.RestoreID} {
			if id != "" && !validIdentity(id) {
				return nil, fmt.Errorf("%w: file identity", ErrJournal)
			}
		}
	}
	return local, nil
}

func validIdentity(id string) bool {
	if len(id) != 25 || id[8] != ':' {
		return false
	}
	_, err := hex.DecodeString(id[:8] + id[9:])
	return err == nil
}

func saveJournal(local *localTransaction) error {
	path, err := checkedPath(local.root, mustRelative(local.root, filepath.Join(local.dir, "journal.json")))
	if err != nil {
		return err
	}
	if err := transactionFault("journal-"+local.journal.Status, path); err != nil {
		return err
	}
	data, err := json.Marshal(local.journal)
	if err != nil {
		return err
	}
	temp := filepath.Join(local.dir, uuid.NewString()+".journal")
	if err := durableFile(temp, data, 0o600); err != nil {
		return err
	}
	return platform.ReplaceAtomic(temp, path)
}

func durableFile(path string, data []byte, mode os.FileMode) error {
	if err := transactionFault("write", path); err != nil {
		return err
	}
	release, err := pinDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer release()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	modeErr := transactionFault("mode", path)
	if modeErr == nil {
		modeErr = file.Chmod(mode)
	}
	syncErr := transactionFault("sync", path)
	if syncErr == nil {
		syncErr = file.Sync()
	}
	closeErr := errors.Join(file.Close(), transactionFault("close", path))
	if err := errors.Join(writeErr, modeErr, syncErr, closeErr); err != nil {
		return err
	}
	return nil
}

func hideTransactionFile(path string) error {
	if err := transactionFault("hide", path); err != nil {
		return err
	}
	return platform.HideFile(path)
}

func discardPreparation(local *localTransaction) error {
	if _, err := checkedPath(local.root, mustRelative(local.root, local.dir)); err != nil {
		return err
	}
	entries, err := os.ReadDir(local.dir)
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		path, err := checkedPath(local.root, mustRelative(local.root, filepath.Join(local.dir, entry.Name())))
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if entry.IsDir() {
			result = errors.Join(result, fmt.Errorf("%w: unexpected preparation directory", ErrJournal))
			continue
		}
		result = errors.Join(result, os.Remove(path))
	}
	if result != nil {
		return result
	}
	return os.Remove(local.dir)
}

// Hold a read handle denying write sharing while comparing. Missing
// destinations use a non-replacing write-through move, so a newly created file wins.
func mutateSnapshot(root, relative string, expected []byte, expectedID string, exists bool, stage string) error {
	path, err := checkedPath(root, relative)
	if err != nil {
		return err
	}
	release, err := pinDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer release()
	if !exists {
		source, err := windows.UTF16PtrFromString(stage)
		if err != nil {
			return err
		}
		destination, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		if err := windows.MoveFileEx(source, destination, windows.MOVEFILE_WRITE_THROUGH); err != nil {
			return fmt.Errorf("%w: create %s: %w", ErrConflict, path, err)
		}
		return nil
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("%w: reserve file %s: %w", ErrConflict, path, err)
	}
	file := os.NewFile(uintptr(handle), path)
	defer file.Close() //nolint:errcheck // Read-only snapshot handle.
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	id, err := fileIdentity(path)
	if err != nil {
		return err
	}
	if id != expectedID || !bytes.Equal(data, expected) {
		return fmt.Errorf("%w: changed snapshot %s", ErrConflict, path)
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: not a regular file %s", ErrConflict, path)
	}
	if info.Mode().Perm()&0o222 == 0 {
		return fmt.Errorf("%w: read-only file %s", ErrConflict, path)
	}
	// MoveFileEx needs the destination handle closed on Windows even with delete
	// sharing. Finish the byte comparison before releasing the snapshot handle.
	if err := file.Close(); err != nil {
		return err
	}
	if stage == "" {
		err = os.Remove(path)
	} else {
		err = platform.ReplaceAtomic(stage, path)
	}
	return err
}

func sameSnapshotInfo(before, current os.FileInfo) bool {
	// Windows FileInfo resolves its file index lazily in SameFile. Creation time
	// from the original stat also protects snapshots whose path was replaced first.
	left, leftOK := before.Sys().(*syscall.Win32FileAttributeData)
	right, rightOK := current.Sys().(*syscall.Win32FileAttributeData)
	if !leftOK || !rightOK || left.CreationTime != right.CreationTime || left.LastWriteTime != right.LastWriteTime {
		return false
	}
	return os.SameFile(before, current)
}

// Deny directory deletion/renaming during an operation, and inspect opened
// handles rather than trusting a path check that a junction swap could invalidate.
func pinDirectory(path string) (func(), error) {
	handles := make([]windows.Handle, 0)
	release := func() {
		for i := len(handles) - 1; i >= 0; i-- {
			_ = windows.CloseHandle(handles[i])
		}
	}
	for current := path; ; current = filepath.Dir(current) {
		name, err := windows.UTF16PtrFromString(current)
		if err != nil {
			release()
			return nil, err
		}
		handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			release()
			return nil, err
		}
		handles = append(handles, handle)
		var info windows.ByHandleFileInformation
		if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
			release()
			return nil, err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
			info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			release()
			return nil, fmt.Errorf("%w: unsafe directory %s", ErrConflict, current)
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return release, nil
}

func contentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func safeRelative(path string) error {
	if !filepath.IsLocal(path) || path == "." || strings.ContainsAny(path, ":\x00*?\"<>|") {
		return fmt.Errorf("%w: unsafe relative path %q", ErrJournal, path)
	}
	for _, part := range strings.Split(strings.ReplaceAll(path, "\\", "/"), "/") {
		if part == ".." || part == "." || part == "" || strings.TrimRight(part, " .") != part {
			return fmt.Errorf("%w: unsafe component %q", ErrJournal, path)
		}
	}
	return nil
}

func mustRelative(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return ".."
	}
	return relative
}

// Check every ancestor, including those above the supplied mod root. Windows
// junctions and other reparse points are rejected even when Lstat looks like a directory.
func checkedPath(root, relative string) (string, error) {
	if err := safeRelative(relative); err != nil {
		return "", err
	}
	path := filepath.Join(root, relative)
	for current := path; ; current = filepath.Dir(current) {
		name, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return "", err
		}
		attrs, err := windows.GetFileAttributes(name)
		if err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) &&
			!errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return "", err
		}
		if err == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return "", fmt.Errorf("%w: reparse point %s", ErrConflict, current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return path, nil
}

func fileIdentity(path string) (string, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	handle, err := windows.CreateFile(
		name,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle) //nolint:errcheck // Read-only identity handle.
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return "", err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return "", fmt.Errorf("%w: reparse point", ErrConflict)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 && info.NumberOfLinks != 1 {
		return "", fmt.Errorf("%w: file with multiple hard links %s", ErrConflict, path)
	}
	return fmt.Sprintf("%08x:%08x%08x", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
