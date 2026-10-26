package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"

	"nahida.live/desktop/internal/platform"
)

func newWailsShellOptions() platform.ShellOptions {
	return platform.ShellOptions{
		OpenURL: func(target string) error {
			if app := application.Get(); app != nil {
				return app.Browser.OpenURL(target)
			}
			return platform.NewShell(platform.ShellOptions{}).OpenExternal(target)
		},
		WindowBackdrop: w32.SupportsBackdropTypes,
	}
}

func newWailsDialogBackend() platform.DialogBackend {
	return platform.DialogBackend{
		SaveFile: func(opts platform.SaveFileOptions) (string, error) {
			path, err := wailsSaveFile(opts)
			return path, dialogError(err)
		},
		SelectDirectory: func(role string) (string, error) {
			path, err := wailsSelectDirectory(role)
			return path, dialogError(err)
		},
		OpenDialog: func(opts platform.OpenDialogOptions) ([]string, error) {
			paths, err := wailsOpenDialog(opts)
			return paths, dialogError(err)
		},
		ShowModal: wailsShowModal,
		OwnerWindow: func() (uintptr, error) {
			win, err := requireMainWindow()
			if err != nil {
				return 0, err
			}
			return uintptr(win.NativeWindow()), nil
		},
	}
}

// dialogError keeps the cancel outcome recognizable to platform.Dialog, which cannot see the Wails sentinel.
func dialogError(err error) error {
	if errors.Is(err, application.ErrDialogCancelled) {
		return errors.Join(platform.ErrDialogCancelled, err)
	}
	return err
}

func wailsSaveFile(opts platform.SaveFileOptions) (string, error) {
	win, err := requireMainWindow()
	if err != nil {
		return "", err
	}
	dlg := application.Get().Dialog.SaveFile()
	dlg.AttachToWindow(win)
	dlg.SetRole(opts.Role)
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

func wailsSelectDirectory(role string) (string, error) {
	win, err := requireMainWindow()
	if err != nil {
		return "", err
	}
	dlg := application.Get().Dialog.OpenFile()
	dlg.AttachToWindow(win)
	dlg.SetRole(role)
	dlg.CanChooseDirectories(true)
	dlg.CanChooseFiles(false)
	dlg.CanCreateDirectories(true)
	return dlg.PromptForSingleSelection()
}

func wailsOpenDialog(opts platform.OpenDialogOptions) ([]string, error) {
	win, err := requireMainWindow()
	if err != nil {
		return nil, err
	}
	dlg := application.Get().Dialog.OpenFile()
	dlg.AttachToWindow(win)
	dlg.SetRole(opts.Role)
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

func wailsShowModal(opts platform.ModalOptions) (platform.ModalResult, error) {
	win, err := requireMainWindow()
	if err != nil {
		return platform.ModalResult{}, err
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
	return platform.ModalResult{Response: 0}, nil
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
		return nil, platform.ErrMainWindowNotFound
	}
	win := app.Window.Current()
	if win == nil {
		return nil, platform.ErrMainWindowNotFound
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
