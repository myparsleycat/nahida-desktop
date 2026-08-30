package mod

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"nahida.live/desktop/internal/platform"
)

type PathSelectorResult struct {
	Mode     string  `json:"mode"`
	Path     *string `json:"path"`
	FileName *string `json:"fileName,omitempty"`
}

type pendingPathSelection struct {
	suggestedName      string
	selectFile         bool
	downloadSource     string
	downloadTargetName *string
	downloadImporter   *string
	suggestedNames     []string
	done               chan pathSelectionOutcome
}

type pathSelectionOutcome struct {
	result PathSelectorResult
	err    error
}

type folderPathSelectionError struct {
	stage string
	path  string
	err   error
}

func (e *folderPathSelectionError) Error() string { return e.err.Error() }
func (e *folderPathSelectionError) Unwrap() error { return e.err }

type folderPathSelectionErrorLog struct {
	SelectionID string `json:"selectionId"`
	Stage       string `json:"stage"`
	Path        string `json:"path,omitempty"`
	Error       string `json:"error"`
}

type pathSelectionDialog interface {
	SaveFile(platform.SaveFileOptions) (platform.DialogResult, error)
	SelectDirectory() (platform.DialogResult, error)
}

type pathSelector struct {
	mu         sync.Mutex
	pending    map[string]*pendingPathSelection
	dialog     pathSelectionDialog
	fs         *platform.FS
	emit       func(string, ...any)
	focus      func()
	waitReady  func(context.Context) (bool, error)
	readyDelay func(context.Context) error
}

const pathSelectorColdStartDelay = 500 * time.Millisecond

func newPathSelector(dialog pathSelectionDialog, fs *platform.FS, emit func(string, ...any), focus func()) *pathSelector {
	return &pathSelector{
		pending: map[string]*pendingPathSelection{},
		dialog:  dialog,
		fs:      fs,
		emit:    emit,
		focus:   focus,
		readyDelay: func(ctx context.Context) error {
			timer := time.NewTimer(pathSelectorColdStartDelay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (p *pathSelector) getSelectedPathWithModeModal(
	ctx context.Context,
	suggestedName string,
	downloadTargetName, downloadImporterKey *string,
	downloadSource string,
	suggestedNames []string,
	selectFile bool,
) (PathSelectorResult, error) {
	if p == nil {
		return PathSelectorResult{}, errors.New("path selector is not configured")
	}
	if downloadSource == "" {
		downloadSource = "nahidaLive"
	}
	id := uuid.NewString()
	pending := &pendingPathSelection{
		suggestedName:      suggestedName,
		selectFile:         selectFile,
		downloadSource:     downloadSource,
		downloadTargetName: downloadTargetName,
		downloadImporter:   downloadImporterKey,
		suggestedNames:     suggestedNames,
		done:               make(chan pathSelectionOutcome, 1),
	}
	p.mu.Lock()
	p.pending[id] = pending
	p.mu.Unlock()
	if p.focus != nil {
		p.focus()
	}
	if p.waitReady != nil {
		alreadyReady, err := p.waitReady(ctx)
		if err == nil && !alreadyReady {
			err = p.readyDelay(ctx)
		}
		if err != nil {
			p.mu.Lock()
			delete(p.pending, id)
			p.mu.Unlock()
			return PathSelectorResult{}, err
		}
	}
	if p.emit != nil {
		p.emit("pathSelector:modeSelect", map[string]any{
			"selectionId":         id,
			"suggestedName":       suggestedName,
			"suggestedNames":      suggestedNames,
			"downloadTargetName":  downloadTargetName,
			"downloadImporterKey": downloadImporterKey,
			"downloadSource":      downloadSource,
		})
	}
	select {
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return PathSelectorResult{}, ctx.Err()
	case outcome := <-pending.done:
		return outcome.result, outcome.err
	}
}

func (m *Mod) SelectFolderPath(ctx context.Context, selectionID string) (bool, error) {
	if m == nil {
		return false, newFolderPathSelectionError("lookup", "", errors.New("pending selection not found"))
	}
	if m.paths == nil {
		err := newFolderPathSelectionError("lookup", "", errors.New("pending selection not found"))
		m.logFolderPathSelectionError(selectionID, err)
		return false, err
	}
	selected, err := m.paths.selectFolderPath(ctx, selectionID)
	if err != nil {
		m.logFolderPathSelectionError(selectionID, err)
	}
	return selected, err
}

func (m *Mod) SelectModManagerPath(_ context.Context, selectionID, path string, fileName *string) error {
	if m == nil || m.paths == nil {
		return errors.New("pending selection not found")
	}
	return m.paths.selectModManagerPath(selectionID, path, fileName)
}

func (m *Mod) CancelPathSelection(selectionID string) error {
	if m == nil || m.paths == nil {
		return nil
	}
	m.paths.cancel(selectionID)
	return nil
}

func (p *pathSelector) selectFolderPath(ctx context.Context, selectionID string) (bool, error) {
	p.mu.Lock()
	pending := p.pending[selectionID]
	p.mu.Unlock()
	if pending == nil {
		return false, newFolderPathSelectionError("lookup", "", errors.New("pending selection not found"))
	}
	selectionStage := "select_directory"
	if pending.selectFile {
		selectionStage = "select_file"
	}
	if p.dialog == nil {
		err := errors.New("main window not found")
		p.settle(selectionID, PathSelectorResult{}, err)
		return false, newFolderPathSelectionError(selectionStage, "", err)
	}
	if pending.selectFile {
		result, err := p.dialog.SaveFile(platform.SaveFileOptions{SuggestedName: pending.suggestedName})
		if err != nil {
			p.settle(selectionID, PathSelectorResult{}, err)
			return false, newFolderPathSelectionError(selectionStage, "", err)
		}
		if result.Canceled {
			if !p.hasPending(selectionID) {
				return false, newFolderPathSelectionError("resume_after_cancel", "", errors.New("pending selection not found"))
			}
			return false, nil
		}
		dir := filepath.Dir(result.FilePath)
		name := filepath.Base(result.FilePath)
		if p.fs != nil && !p.fs.IsPathWritable(dir) {
			err = errors.New("path is not writable")
			p.settle(selectionID, PathSelectorResult{}, err)
			return false, newFolderPathSelectionError("validate_file_path", result.FilePath, err)
		}
		if !p.settle(selectionID, PathSelectorResult{Mode: "folder", Path: &dir, FileName: &name}, nil) {
			return false, newFolderPathSelectionError("settle_file_path", result.FilePath, errors.New("pending selection not found"))
		}
		return true, nil
	}
	result, err := p.dialog.SelectDirectory()
	if err != nil {
		p.settle(selectionID, PathSelectorResult{}, err)
		return false, newFolderPathSelectionError(selectionStage, "", err)
	}
	if result.Canceled {
		if !p.hasPending(selectionID) {
			return false, newFolderPathSelectionError("resume_after_cancel", "", errors.New("pending selection not found"))
		}
		return false, nil
	}
	if p.fs != nil && !p.fs.IsPathWritable(result.FilePath) {
		err = errors.New("path is not writable")
		p.settle(selectionID, PathSelectorResult{}, err)
		return false, newFolderPathSelectionError("validate_directory_path", result.FilePath, err)
	}
	if !p.settle(selectionID, PathSelectorResult{Mode: "folder", Path: &result.FilePath}, nil) {
		return false, newFolderPathSelectionError("settle_directory_path", result.FilePath, errors.New("pending selection not found"))
	}
	return true, nil
}

func (m *Mod) logFolderPathSelectionError(selectionID string, err error) {
	if m == nil || m.log == nil || err == nil {
		return
	}
	entry := folderPathSelectionErrorLog{SelectionID: selectionID, Stage: "unknown", Error: err.Error()}
	var selectionErr *folderPathSelectionError
	if errors.As(err, &selectionErr) {
		entry.Stage = selectionErr.stage
		entry.Path = selectionErr.path
		entry.Error = selectionErr.err.Error()
	}
	m.log.Error(entry, "Mod:SelectFolderPath")
}

func newFolderPathSelectionError(stage, path string, err error) error {
	return &folderPathSelectionError{stage: stage, path: path, err: err}
}

func (p *pathSelector) hasPending(selectionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pending[selectionID] != nil
}

func (p *pathSelector) selectModManagerPath(selectionID, path string, fileName *string) error {
	if !p.settle(selectionID, PathSelectorResult{Mode: "modManager", Path: &path, FileName: fileName}, nil) {
		return errors.New("pending selection not found")
	}
	return nil
}

func (p *pathSelector) cancel(selectionID string) {
	p.settle(selectionID, PathSelectorResult{Mode: "folder", Path: nil}, nil)
}

func (p *pathSelector) settle(selectionID string, result PathSelectorResult, err error) bool {
	p.mu.Lock()
	pending := p.pending[selectionID]
	delete(p.pending, selectionID)
	p.mu.Unlock()
	if pending == nil {
		return false
	}
	pending.done <- pathSelectionOutcome{result: result, err: err}
	return true
}
