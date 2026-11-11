package app

import (
	"errors"
	"strings"
	"sync"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/dialog"
	"github.com/myparsleycat/fairy/tray"

	"nahida.live/desktop/internal/platform"
)

// uiHost is what the backend needs from the UI framework. A zero field leaves the capability unavailable.
type uiHost struct {
	// emit sends a renderer event to every window.
	emit   func(name string, data ...any)
	shell  platform.ShellOptions
	dialog platform.DialogBackend
	// notify shows a system notification; id replaces an earlier notification with the same id.
	notify func(id, title, body string) error
}

// newGUIHost builds the host on the fairy framework. Run sets emit once the UI event bus exists.
func newGUIHost(owner func() *fairy.Window, notices *trayNotices) uiHost {
	return uiHost{
		shell: platform.ShellOptions{
			WindowBackdrop: func() bool { return fairy.BackdropSupported(fairy.BackdropMica) },
		},
		dialog: platform.DialogBackend{
			SaveFile: func(opts platform.SaveFileOptions) (string, error) {
				path, err := dialog.SaveFile(owner(), dialog.FileOptions{
					FileName: opts.SuggestedName,
					Filters:  fileFilters(opts.Filters),
					Role:     opts.Role,
				})
				return path, dialogError(err)
			},
			SelectDirectory: func(role string) (string, error) {
				paths, err := dialog.OpenFolders(owner(), dialog.FileOptions{Role: role})
				if err != nil {
					return "", dialogError(err)
				}
				return paths[0], nil
			},
			OpenDialog: func(opts platform.OpenDialogOptions) ([]string, error) {
				options := dialog.FileOptions{
					Title:    opts.Title,
					Folder:   opts.DefaultPath,
					Filters:  fileFilters(opts.Filters),
					Role:     opts.Role,
					Multiple: hasDialogProperty(opts.Properties, "multiSelections"),
				}
				if hasDialogProperty(opts.Properties, "openDirectory") {
					paths, err := dialog.OpenFolders(owner(), options)
					return paths, dialogError(err)
				}
				paths, err := dialog.OpenFiles(owner(), options)
				return paths, dialogError(err)
			},
			ShowModal: func(opts platform.ModalOptions) (platform.ModalResult, error) {
				kind := dialog.MessageInfo
				switch strings.ToLower(opts.Type) {
				case "error":
					kind = dialog.MessageError
				case "warning":
					kind = dialog.MessageWarning
				case "question":
					kind = dialog.MessageQuestion
				}
				dialog.Message(owner(), opts.Title, opts.Message, kind, dialog.ButtonsOK)
				return platform.ModalResult{Response: 0}, nil
			},
			OwnerWindow: func() (uintptr, error) {
				if window := owner(); window != nil {
					return window.HWND(), nil
				}
				return 0, platform.ErrMainWindowNotFound
			},
		},
		notify: notices.notify,
	}
}

// dialogError keeps the cancel outcome recognizable to platform.Dialog, which cannot see the dialog sentinel.
func dialogError(err error) error {
	if errors.Is(err, dialog.ErrCanceled) {
		return errors.Join(platform.ErrDialogCancelled, err)
	}
	return err
}

func fileFilters(filters []platform.FileFilter) []dialog.Filter {
	result := make([]dialog.Filter, 0, len(filters))
	for _, filter := range filters {
		var patterns []string
		for _, ext := range filter.Extensions {
			ext = strings.TrimSpace(ext)
			switch {
			case ext == "":
			case strings.HasPrefix(ext, "*."):
				patterns = append(patterns, ext)
			default:
				patterns = append(patterns, "*."+strings.TrimPrefix(ext, "."))
			}
		}
		if len(patterns) > 0 {
			result = append(result, dialog.Filter{Name: filter.Name, Patterns: patterns})
		}
	}
	return result
}

func hasDialogProperty(properties []string, name string) bool {
	for _, property := range properties {
		if property == name {
			return true
		}
	}
	return false
}

// trayNotices shows notifications from the tray icon once it exists; earlier ones are dropped.
type trayNotices struct {
	mu   sync.Mutex
	tray *tray.Tray
}

func (n *trayNotices) use(icon *tray.Tray) {
	n.mu.Lock()
	n.tray = icon
	n.mu.Unlock()
}

func (n *trayNotices) notify(id, title, body string) error {
	n.mu.Lock()
	icon := n.tray
	n.mu.Unlock()
	if icon != nil {
		icon.NotifyID(id, title, body, tray.NoticeInfo)
	}
	return nil
}
