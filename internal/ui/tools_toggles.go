package ui

import (
	"context"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
)

const (
	persistLogsEvent       = "setting:xxmi:persistLogs"
	toggleViewerLogsEvent  = "tools:toggle-viewer-logs"
	toggleViewerStateEvent = "tools:toggle-viewer-state"
	defaultToggleHotkey    = "ctrl H"
)

type persistTool struct {
	logs query[[]string]
}

type toggleViewerTool struct {
	logs  query[[]string]
	state query[tools.ToggleViewerState]
	// batch is the batch the screen started and is waiting for; stopping is a cancel request in flight.
	batch    string
	stopping bool
	// hotkey is the hotkey being recorded, nil while its dialog is closed.
	hotkey *keyRecording
}

func (s *Shell) registerToolEvents(on func(string, func([]any))) {
	tl := &s.tools
	on(persistLogsEvent, func(data []any) {
		if logs, ok := first(data).([]string); ok {
			tl.persist.logs.set(logs)
		}
	})
	on(toggleViewerLogsEvent, func(data []any) {
		if logs, ok := first(data).([]string); ok {
			tl.toggleViewer.logs.set(logs)
		}
	})
	on(toggleViewerStateEvent, func(data []any) {
		if state, ok := first(data).(tools.ToggleViewerState); ok {
			tl.toggleViewer.state.set(state)
		}
	})
	on(textureUpscaleProgressEvent, func(data []any) {
		if event, ok := first(data).(tools.TextureUpscaleProgressEvent); ok && tl.textureRun != nil {
			tl.textureRun.upscale = &event
		}
	})
	on(textureResizeProgressEvent, func(data []any) {
		if event, ok := first(data).(tools.TextureResizeProgressEvent); ok {
			tl.textureProgress = &event
		}
	})
	on(fixer4001ProgressEvent, func(data []any) {
		if event, ok := first(data).(tools.Fixer4001ProgressEvent); ok {
			s.on4001Progress(event)
		}
	})
}

// toolError logs a failed tool action the UI reports only through a toast.
func (s *Shell) toolError(err error, operation string, fields map[string]any) error {
	return infra.ReportError(s.deps.Log, err, "Tools", infra.Diagnostic{Operation: operation, Fields: fields})
}

// xxmiConfigured reports whether the XXMI path is set, and whether that is known yet.
func (s *Shell) xxmiConfigured() (configured, known bool) {
	data := fetch(s, &s.xxmi.data, s.deps.XXMI.GetXXMIData)
	if !data.loaded {
		return false, data.err != nil
	}
	return data.value.XXMIPath != nil && *data.value.XXMIPath != "", true
}

func (s *Shell) persistToolPage() *fairy.El {
	configured, known := s.xxmiConfigured()
	if !known {
		return fairy.Box()
	}
	if !configured {
		return s.toolXXMIMissing(T("page.setting.xxmi.persistToggles"), T("page.setting.xxmi.persistNotFoundXXMI"))
	}
	loaded := s.settingsLoaded(setting.KeyXXMIPersistToggles)
	logs := fetch(s, &s.tools.persist.logs, func(context.Context) ([]string, error) {
		return s.deps.Tools.GetPersistLogs(), nil
	})
	title := T("page.setting.xxmi.persistToggles")
	return toolPage(
		toolHeading(title, T("page.setting.xxmi.persistTogglesDescription"),
			kit.Switch(s.boolSetting(setting.KeyXXMIPersistToggles),
				func(on bool) { s.setSetting(setting.KeyXXMIPersistToggles, on) },
				kit.Disabled(!loaded || s.settingSaving(setting.KeyXXMIPersistToggles))).Label(title)),
		toolSection(T("page.setting.xxmi.persistLogs"),
			toolLogList(logs.value, T("page.setting.xxmi.persistLogsEmpty"))),
	)
}

func (s *Shell) toggleViewerToolPage(cx *fairy.Cx) *fairy.El {
	tv := &s.tools.toggleViewer
	const prefix = "page.tools.toggle_viewer_generator."
	configured, known := s.xxmiConfigured()
	if !known {
		return fairy.Box()
	}
	if !configured {
		return s.toolXXMIMissing(T(prefix+"title"), T(prefix+"not_configured"))
	}

	loaded := s.settingsLoaded(setting.KeyXXMIToggleViewerAutoGenerate, setting.KeyXXMIToggleViewerHotkey)
	logs := fetch(
		s,
		&tv.logs,
		func(context.Context) ([]string, error) { return s.deps.Tools.ToggleViewerGetLogs(), nil },
	)
	state := fetch(s, &tv.state, func(context.Context) (tools.ToggleViewerState, error) {
		return s.deps.Tools.ToggleViewerGetState(), nil
	}).value
	mode := ""
	if state.IsRunning && state.Mode != nil {
		mode = *state.Mode
	}
	deleting := mode == "delete" || tv.batch == "delete"
	batchRunning := mode == "generate" || mode == "delete" || tv.batch != ""
	enabledSaving := s.settingSaving(setting.KeyXXMIToggleViewerAutoGenerate)
	busy := state.IsRunning || tv.batch != "" || tv.stopping || enabledSaving
	status := T(prefix + "status.idle")
	if mode == "generate" || mode == "delete" || mode == "scan" {
		status = T(prefix + "status." + mode)
	}

	hotkey := s.stringSetting(setting.KeyXXMIToggleViewerHotkey)
	if hotkey == "" {
		hotkey = defaultToggleHotkey
	}
	trigger := fairy.Row(fairy.Text(T(prefix + "dialog.trigger")).FontSize(12).LineHeight(16)).
		Gap(4).
		Align(fairy.Center)
	for _, label := range keyLabelsOf(hotkey) {
		trigger.Child(kit.Kbd(label))
	}
	hotkeyButton := fairy.Row(trigger).Key("tools.toggle-viewer.hotkey").H(32).PadXY(10, 0).Align(fairy.Center).
		Radius(6).Border(1, kit.T().Border).HoverFill(hoverFill(kit.T())).Cursor(fairy.CursorPointer).Focusable().
		Role(fairy.RoleButton).Label(T(prefix + "hotkey"))
	if loaded && !batchRunning && !s.settingSaving(setting.KeyXXMIToggleViewerHotkey) {
		hotkeyButton.OnClick(func() { tv.hotkey = &keyRecording{value: hotkey} })
	} else {
		hotkeyButton.Opacity(0.5)
	}
	s.toggleViewerHotkeyDialog(cx)

	title := T(prefix + "auto_generate")
	return toolPage(
		toolHeading(title, T(prefix+"auto_generate_desc"),
			kit.Switch(s.boolSetting(setting.KeyXXMIToggleViewerAutoGenerate), func(on bool) {
				s.setSetting(setting.KeyXXMIToggleViewerAutoGenerate, on)
				tv.state.invalidate()
			}, kit.Disabled(!loaded || deleting || enabledSaving)).Label(title)),
		settingsGroup("",
			settingRow(T(prefix+"hotkey"), "", hotkeyButton),
			fairy.Row(
				kit.Button(T(prefix+"batch_generate"), func() { s.runToggleViewerBatch("generate") }, kit.Outline,
					kit.Disabled(busy)),
				kit.Button(T(prefix+"batch_delete"), s.confirmToggleViewerDelete, kit.Outline, kit.Disabled(busy)),
				kit.Button(T(prefix+"stop"), s.stopToggleViewer, kit.Destructive,
					kit.Disabled(!state.IsRunning || tv.stopping)),
				fairy.Text(status).FontSize(12).LineHeight(16).Color(kit.T().MutedForeground),
			).Gap(8).Wrap().Align(fairy.Center).PadXY(0, 12),
		),
		toolSection(T(prefix+"logs"), toolLogList(logs.value, T(prefix+"no_logs"))),
	)
}

func (s *Shell) toggleViewerHotkeyDialog(cx *fairy.Cx) {
	tv := &s.tools.toggleViewer
	r := tv.hotkey
	if r == nil {
		return
	}
	const prefix = "page.tools.toggle_viewer_generator.dialog."
	dismiss := func() { tv.hotkey = nil }
	save := func() {
		if r.value != "" {
			s.setSetting(setting.KeyXXMIToggleViewerHotkey, r.value)
		}
		tv.hotkey = nil
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "title"), Description: T(prefix + "description"),
		Body: keyRecorder(r, T(prefix+"title"), "tools.toggle-viewer.hotkey-manual", save),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), dismiss, kit.Outline),
			kit.Button(T("g.save"), save),
		},
		OnClose: dismiss,
		Key:     "tools.toggle-viewer.hotkey-dialog",
	})
}

func (s *Shell) confirmToggleViewerDelete() {
	const prefix = "page.tools.toggle_viewer_generator."
	s.Confirm(Confirmation{
		Title:        T(prefix + "delete_confirm.title"),
		Description:  T(prefix + "delete_confirm.description"),
		ConfirmLabel: T(prefix + "batch_delete"),
		Danger:       true,
		OnResult: func(confirmed bool) {
			if confirmed {
				s.runToggleViewerBatch("delete")
			}
		},
	})
}

func (s *Shell) runToggleViewerBatch(mode string) {
	tv := &s.tools.toggleViewer
	if tv.batch != "" {
		return
	}
	tv.batch = mode
	run, done := s.deps.Tools.ToggleViewerRunBatchGenerate, "batch_generate_done"
	if mode == "delete" {
		run, done = s.deps.Tools.ToggleViewerRunBatchDelete, "batch_delete_done"
	}
	s.background(func(ctx context.Context) {
		result, err := run(ctx)
		if err != nil {
			err = s.toolError(err, "toggle-viewer."+mode, nil)
		}
		fairy.Go(func() {
			tv.batch = ""
			tv.state.invalidate()
			const prefix = "page.tools.toggle_viewer_generator."
			switch {
			case err != nil:
				s.toasts.Show(kit.ToastError, err.Error(), "")
			case result.Cancelled:
				s.toasts.Show(kit.ToastInfo, T(prefix+"batch_cancelled"), "")
			default:
				s.toasts.Show(kit.ToastSuccess, T(prefix+done, "count", result.Count), "")
			}
		})
	})
}

func (s *Shell) stopToggleViewer() {
	tv := &s.tools.toggleViewer
	tv.stopping = true
	go func() {
		s.deps.Tools.ToggleViewerCancelCurrentWork()
		fairy.Go(func() {
			tv.stopping = false
			tv.state.invalidate()
		})
	}()
}
