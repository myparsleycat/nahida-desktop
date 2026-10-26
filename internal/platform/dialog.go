package platform

import (
	"errors"
)

// ErrMainWindowNotFound matches the Electron dialog guard.
var ErrMainWindowNotFound = errors.New("main window not found")

// ErrDialogCancelled is what a DialogBackend returns when the user dismisses a native dialog.
var ErrDialogCancelled = errors.New("dialog cancelled")

// FileFilter matches Electron's dialog.FileFilter.
type FileFilter struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// SaveFileOptions is the Electron saveFileDialog argument.
type SaveFileOptions struct {
	SuggestedName string       `json:"suggestedName"`
	Filters       []FileFilter `json:"filters"`
	// Role keeps the last folder Windows remembers for this kind of dialog
	// apart from dialogs with another role. Dialogs without a role share one.
	Role string `json:"role,omitempty"`
}

// DialogResult is the Electron save/select return value.
type DialogResult struct {
	Canceled bool   `json:"canceled"`
	FilePath string `json:"filePath,omitempty"`
}

// OpenDialogOptions is the Electron util:showOpenDialog argument.
type OpenDialogOptions struct {
	Title       string       `json:"title"`
	DefaultPath string       `json:"defaultPath"`
	Filters     []FileFilter `json:"filters"`
	Properties  []string     `json:"properties"`
	// Role has the same meaning as SaveFileOptions.Role.
	Role string `json:"role,omitempty"`
}

// OpenDialogResult is the Electron showOpenDialog return value.
type OpenDialogResult struct {
	Canceled  bool     `json:"canceled"`
	FilePaths []string `json:"filePaths"`
}

// ModalOptions is the Electron util:showModal argument.
type ModalOptions struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// ModalResult is Electron's MessageBoxReturnValue.
type ModalResult struct {
	Response        int  `json:"response"`
	CheckboxChecked bool `json:"checkboxChecked"`
}

// DirectoryConflictChoice is the result of the Drive directory download
// conflict prompt.
type DirectoryConflictChoice string

const (
	DirectoryConflictOverwrite DirectoryConflictChoice = "overwrite"
	DirectoryConflictRename    DirectoryConflictChoice = "rename"
	DirectoryConflictCancel    DirectoryConflictChoice = "cancel"
)

// DirectoryConflictOptions describes Electron's three-button directory
// download conflict prompt.
type DirectoryConflictOptions struct {
	Name string
}

type Dialog struct {
	saveFile          func(SaveFileOptions) (string, error)
	selectDirectory   func(role string) (string, error)
	openDialog        func(OpenDialogOptions) ([]string, error)
	showModal         func(ModalOptions) (ModalResult, error)
	directoryConflict func(DirectoryConflictOptions) (DirectoryConflictChoice, error)
}

// DialogBackend shows the native dialogs. It lives outside this package so the elevated helper, which
// imports platform, does not link the UI framework. A missing function reports ErrMainWindowNotFound.
type DialogBackend struct {
	SaveFile        func(SaveFileOptions) (string, error)
	SelectDirectory func(role string) (string, error)
	OpenDialog      func(OpenDialogOptions) ([]string, error)
	ShowModal       func(ModalOptions) (ModalResult, error)
	// OwnerWindow returns the native handle of the window the directory conflict prompt belongs to.
	OwnerWindow func() (uintptr, error)
}

func NewDialog(backend DialogBackend) *Dialog {
	d := &Dialog{
		saveFile:        backend.SaveFile,
		selectDirectory: backend.SelectDirectory,
		openDialog:      backend.OpenDialog,
		showModal:       backend.ShowModal,
		directoryConflict: func(opts DirectoryConflictOptions) (DirectoryConflictChoice, error) {
			return showDirectoryConflict(backend.OwnerWindow, opts)
		},
	}
	if d.saveFile == nil {
		d.saveFile = func(SaveFileOptions) (string, error) { return "", ErrMainWindowNotFound }
	}
	if d.selectDirectory == nil {
		d.selectDirectory = func(string) (string, error) { return "", ErrMainWindowNotFound }
	}
	if d.openDialog == nil {
		d.openDialog = func(OpenDialogOptions) ([]string, error) { return nil, ErrMainWindowNotFound }
	}
	if d.showModal == nil {
		d.showModal = func(ModalOptions) (ModalResult, error) { return ModalResult{}, ErrMainWindowNotFound }
	}
	return d
}

func (d *Dialog) SaveFile(opts SaveFileOptions) (DialogResult, error) {
	path, err := d.saveFile(opts)
	if errors.Is(err, ErrDialogCancelled) {
		return DialogResult{Canceled: true}, nil
	}
	if err != nil {
		return DialogResult{}, err
	}
	if path == "" {
		return DialogResult{Canceled: true}, nil
	}
	return DialogResult{FilePath: path}, nil
}

// SelectDirectory shows a folder picker. role has the same meaning as
// SaveFileOptions.Role.
func (d *Dialog) SelectDirectory(role string) (DialogResult, error) {
	path, err := d.selectDirectory(role)
	if errors.Is(err, ErrDialogCancelled) {
		return DialogResult{Canceled: true}, nil
	}
	if err != nil {
		return DialogResult{}, err
	}
	if path == "" {
		return DialogResult{Canceled: true}, nil
	}
	return DialogResult{FilePath: path}, nil
}

func (d *Dialog) ShowOpenDialog(opts OpenDialogOptions) (OpenDialogResult, error) {
	paths, err := d.openDialog(opts)
	if errors.Is(err, ErrDialogCancelled) {
		return OpenDialogResult{Canceled: true, FilePaths: []string{}}, nil
	}
	if err != nil {
		return OpenDialogResult{}, err
	}
	if len(paths) == 0 {
		return OpenDialogResult{Canceled: true, FilePaths: []string{}}, nil
	}
	return OpenDialogResult{FilePaths: paths}, nil
}

func (d *Dialog) ShowModal(opts ModalOptions) (ModalResult, error) {
	return d.showModal(opts)
}

// ResolveDirectoryConflict displays Electron's overwrite/new-name/cancel
// prompt for a directory download.
func (d *Dialog) ResolveDirectoryConflict(opts DirectoryConflictOptions) (DirectoryConflictChoice, error) {
	return d.directoryConflict(opts)
}

// UseDirectoryConflictResolver replaces the native resolver for tests and
// embedders.
//
//wails:ignore
func (d *Dialog) UseDirectoryConflictResolver(fn func(DirectoryConflictOptions) (DirectoryConflictChoice, error)) {
	if fn != nil {
		d.directoryConflict = fn
	}
}
