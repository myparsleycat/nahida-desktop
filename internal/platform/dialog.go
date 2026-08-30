package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ErrMainWindowNotFound matches the Electron dialog guard.
var ErrMainWindowNotFound = errors.New("main window not found")

// FileFilter matches Electron's dialog.FileFilter.
type FileFilter struct {
	Name       string   `json:"name"`
	Extensions []string `json:"extensions"`
}

// SaveFileOptions is the Electron saveFileDialog argument.
type SaveFileOptions struct {
	SuggestedName string       `json:"suggestedName"`
	Filters       []FileFilter `json:"filters"`
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
	selectDirectory   func() (string, error)
	openDialog        func(OpenDialogOptions) ([]string, error)
	showModal         func(ModalOptions) (ModalResult, error)
	directoryConflict func(DirectoryConflictOptions) (DirectoryConflictChoice, error)
}

func NewDialog() *Dialog {
	return &Dialog{
		saveFile:          defaultSaveFile,
		selectDirectory:   defaultSelectDirectory,
		openDialog:        defaultOpenDialog,
		showModal:         defaultShowModal,
		directoryConflict: defaultDirectoryConflict,
	}
}

func (d *Dialog) SaveFile(opts SaveFileOptions) (DialogResult, error) {
	path, err := d.saveFile(opts)
	if errors.Is(err, application.ErrDialogCancelled) {
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

func (d *Dialog) SelectDirectory() (DialogResult, error) {
	path, err := d.selectDirectory()
	if errors.Is(err, application.ErrDialogCancelled) {
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
	if errors.Is(err, application.ErrDialogCancelled) {
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

func defaultSaveFile(opts SaveFileOptions) (string, error) {
	win, err := requireMainWindow()
	if err != nil {
		return "", err
	}
	dlg := application.Get().Dialog.SaveFile()
	dlg.AttachToWindow(win)
	if opts.SuggestedName != "" {
		dlg.SetFilename(opts.SuggestedName)
	}
	for _, filter := range opts.Filters {
		if pattern := fileFilterPattern(filter.Extensions); pattern != "" {
			dlg.AddFilter(filter.Name, pattern)
		}
	}
	return dlg.PromptForSingleSelection()
}

func defaultSelectDirectory() (string, error) {
	win, err := requireMainWindow()
	if err != nil {
		return "", err
	}
	dlg := application.Get().Dialog.OpenFile()
	dlg.AttachToWindow(win)
	dlg.CanChooseDirectories(true)
	dlg.CanChooseFiles(false)
	dlg.CanCreateDirectories(true)
	return dlg.PromptForSingleSelection()
}

func defaultOpenDialog(opts OpenDialogOptions) ([]string, error) {
	win, err := requireMainWindow()
	if err != nil {
		return nil, err
	}
	dlg := application.Get().Dialog.OpenFile()
	dlg.AttachToWindow(win)
	if opts.Title != "" {
		dlg.SetTitle(opts.Title)
	}
	if directory := openDialogDefaultDirectory(opts.DefaultPath); directory != "" {
		dlg.SetDirectory(directory)
	}
	openDirectory := hasDialogProperty(opts.Properties, "openDirectory")
	openFile := hasDialogProperty(opts.Properties, "openFile") || !openDirectory
	dlg.CanChooseDirectories(openDirectory)
	dlg.CanChooseFiles(openFile)
	if openDirectory {
		dlg.CanCreateDirectories(true)
	}
	for _, filter := range opts.Filters {
		if pattern := fileFilterPattern(filter.Extensions); pattern != "" {
			dlg.AddFilter(filter.Name, pattern)
		}
	}
	if hasDialogProperty(opts.Properties, "multiSelections") {
		return dlg.PromptForMultipleSelection()
	}
	path, err := dlg.PromptForSingleSelection()
	if err != nil || path == "" {
		return nil, err
	}
	return []string{path}, nil
}

func openDialogDefaultDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	parent := filepath.Dir(path)
	if info, err := os.Stat(parent); err == nil && info.IsDir() {
		return parent
	}
	return path
}

func defaultShowModal(opts ModalOptions) (ModalResult, error) {
	win, err := requireMainWindow()
	if err != nil {
		return ModalResult{}, err
	}
	// Wails message dialogs do not return Electron's button index. The
	// Electron showModal helper only ever shows type/title/message with the
	// default OK button (response 0). See PORTING-WIP.md.
	var dlg *application.MessageDialog
	switch strings.ToLower(opts.Type) {
	case "error":
		dlg = application.Get().Dialog.Error()
	case "warning":
		dlg = application.Get().Dialog.Warning()
	case "question":
		dlg = application.Get().Dialog.Question()
	default:
		dlg = application.Get().Dialog.Info()
	}
	dlg.AttachToWindow(win)
	if opts.Title != "" {
		dlg.SetTitle(opts.Title)
	}
	if opts.Message != "" {
		dlg.SetMessage(opts.Message)
	}
	done := make(chan struct{})
	button := dlg.AddButton("OK")
	button.OnClick(func() { close(done) })
	dlg.SetDefaultButton(button)
	dlg.Show()
	<-done
	return ModalResult{Response: 0}, nil
}

func hasDialogProperty(properties []string, name string) bool {
	for _, property := range properties {
		if property == name {
			return true
		}
	}
	return false
}

func requireMainWindow() (application.Window, error) {
	app := application.Get()
	if app == nil {
		return nil, ErrMainWindowNotFound
	}
	win := app.Window.Current()
	if win == nil {
		return nil, ErrMainWindowNotFound
	}
	return win, nil
}

func fileFilterPattern(exts []string) string {
	parts := make([]string, 0, len(exts))
	for _, ext := range exts {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if strings.HasPrefix(ext, "*.") {
			parts = append(parts, ext)
			continue
		}
		parts = append(parts, "*."+strings.TrimPrefix(ext, "."))
	}
	return strings.Join(parts, ";")
}
