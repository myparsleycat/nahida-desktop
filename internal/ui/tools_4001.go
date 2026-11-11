package ui

import (
	"context"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/tools"
	fixer4001 "nahida.live/desktop/internal/tools/fixer_4001"
)

const fixer4001ProgressEvent = "tools:4001FixerProgress"

var fixer4001Providers = []string{"SpectrumQT", "myparsleycat"}

// fixer4001Tool is the 4001 fixer. task is the running task ("build-dll", "diversify-dll", "restore-dll"), "" when
// idle; progress is a backend progress code, a locale key, or an error text.
type fixer4001Tool struct {
	started bool
	tab     string

	provider    string
	versions    []string
	versionsErr bool
	versionsSeq int
	version     string

	importer, importerKey string
	checked               string
	backupPath            string
	requiresElevation     bool

	task         string
	progress     string
	errorMessage string
	// received is set by the first progress event, which is newer than any state read in the background.
	received bool

	buildTools buildToolsPath
}

// buildToolsPath is the Visual Studio Build Tools folder field. saved is the stored path, which locks the field
// until cleared.
type buildToolsPath struct {
	loaded bool
	draft  string
	saved  string
	err    string
	busy   bool
}

func (s *Shell) start4001Fixer() {
	f := &s.tools.fixer4001
	if f.started {
		return
	}
	f.started, f.tab, f.provider = true, "build", fixer4001Providers[0]
	s.load4001Versions()
	go func() {
		state := s.deps.Tools.FourThousandOneFixerGetState()
		fairy.Go(func() {
			if !f.received {
				f.task, f.progress, f.errorMessage = deref(state.ActiveTask), state.Progress, state.ErrorMessage
			}
		})
	}()
	s.background(func(ctx context.Context) {
		path, err := s.deps.Tools.FourThousandOneFixerGetBuildToolsPath(ctx)
		if err != nil {
			err = s.toolError(err, "4001-fixer.build-tools-path.load", nil)
		}
		fairy.Go(func() {
			b := &f.buildTools
			b.loaded = true
			if err != nil {
				b.err = err.Error()
				return
			}
			if saved := strings.TrimSpace(path); saved != "" {
				b.saved, b.draft = saved, saved
			}
		})
	})
}

func (s *Shell) on4001Progress(event tools.Fixer4001ProgressEvent) {
	f := &s.tools.fixer4001
	f.received = true
	f.progress, f.errorMessage = event.Code, event.ErrorMessage
	if event.Task != nil {
		f.task = *event.Task
	}
	if fixer4001.IsTerminalProgressCode(event.Code) {
		f.task = ""
	}
}

func (s *Shell) load4001Versions() {
	f := &s.tools.fixer4001
	f.versionsSeq++
	seq, provider := f.versionsSeq, f.provider
	f.versions, f.version, f.versionsErr = nil, "", false
	s.background(func(ctx context.Context) {
		versions, err := s.deps.Tools.FourThousandOneFixerGetProviderReleases(ctx, provider)
		if err != nil {
			err = s.toolError(err, "4001-fixer.releases", map[string]any{"provider": provider})
		}
		fairy.Go(func() {
			if seq != f.versionsSeq {
				return
			}
			if err != nil {
				f.versionsErr = true
				return
			}
			f.versions = append([]string{}, versions...)
			if len(versions) > 0 {
				f.version = versions[0]
			}
		})
	})
}

// check4001Importer reads the selected importer's diversification backup and whether writing to it needs
// elevation, once per selection.
func (s *Shell) check4001Importer() {
	f := &s.tools.fixer4001
	if f.importer == "" || f.checked == f.importer {
		return
	}
	f.checked = f.importer
	importer := f.importer
	go func() {
		input := tools.Fixer4001PathInput{ImporterPath: &importer}
		state, err := s.deps.Tools.FourThousandOneFixerGetDiversificationState(input)
		access := s.deps.Tools.FourThousandOneFixerCheckImporterWriteAccess(input)
		fairy.Go(func() {
			if f.importer != importer {
				return
			}
			if err == nil {
				f.backupPath = deref(state.BackupPath)
			}
			f.requiresElevation = access.RequiresElevation
		})
	}()
}

func (s *Shell) fixer4001ToolPage(cx *fairy.Cx) *fairy.El {
	f := &s.tools.fixer4001
	t := kit.T()
	const prefix = "page.tools.4001_fixer."
	s.start4001Fixer()

	var importers []string
	keys := map[string]string{}
	if data := fetch(s, &s.xxmi.data, s.deps.XXMI.GetXXMIData); data.loaded {
		for _, importer := range data.value.EnabledImporters {
			if strings.EqualFold(importer.Key, "GIMI") {
				importers = append(importers, importer.ImporterFolder)
				keys[importer.ImporterFolder] = importer.Key
			}
		}
	}
	if f.importer == "" && len(importers) > 0 {
		f.importer, f.importerKey = importers[0], keys[importers[0]]
	}
	s.check4001Importer()
	running := f.task != ""

	importerRow := fairy.Row().Gap(8).Wrap().RowGap(8)
	if len(importers) == 0 {
		importerRow.Child(
			fairy.Text(T(prefix+"no_importer")).FontSize(12).Color(t.MutedForeground).WFull().PadXY(12, 6).
				Radius(4).Border(1, t.Border).BorderDashed().TextAlign(fairy.TextCenter),
		)
	}
	for _, folder := range importers {
		importerRow.Child(fixerChip(keys[folder], f.importer == folder, running, func() {
			if f.importer == folder {
				return
			}
			f.importer, f.importerKey, f.backupPath, f.requiresElevation = folder, keys[folder], "", false
		}).Tooltip(folder))
	}

	page := toolPage(
		toolHeading(T(prefix+"title"), T(prefix+"description"), nil),
		fairy.Col(fixerLabel(T(prefix+"target_importer")), importerRow).Gap(8).
			PadEdges(4, 14, 4, 30).BorderEdges(0, 0, 0, 1, t.Foreground.Alpha(0.2)),
	)
	if f.requiresElevation {
		page.Child(xxmiAlert(lucide.ShieldAlert, false, T(prefix+"system_folder_warning_title"),
			T(prefix+"system_folder_warning_description")))
	}
	tabs := fairy.Col(
		kit.Tabs(f.tab, []kit.Item{
			{Value: "build", Label: T(prefix + "build_tab"), Icon: lucide.Hammer},
			{Value: "diversify", Label: T(prefix + "obfuscate_tab"), Icon: lucide.ShieldCheck},
		}, func(tab string) { f.tab = tab }),
	).Gap(16)
	if f.tab == "diversify" {
		tabs.Child(s.fixer4001Diversify(running))
	} else {
		width, _ := cx.Size()
		tabs.Child(s.fixer4001Build(running, width >= toolsSidebarBreakpoint))
	}
	page.Child(tabs)
	if status := s.fixer4001Status(running); status != nil {
		page.Child(status)
	}
	return page
}

func (s *Shell) fixer4001Build(running, twoColumns bool) *fairy.El {
	f := &s.tools.fixer4001
	t := kit.T()
	const prefix = "page.tools.4001_fixer."

	providers := fairy.Row().Gap(8).Wrap().RowGap(8)
	for _, provider := range fixer4001Providers {
		providers.Child(fixerChip(provider, f.provider == provider, running, func() {
			if f.provider != provider {
				f.provider = provider
				s.load4001Versions()
			}
		}))
	}
	var versions *fairy.El
	switch {
	case f.versionsErr:
		versions = fairy.Row(fairy.Icon(lucide.CircleX).Size(12, 12), fairy.Text(T(prefix+"load_failed"))).Gap(8).
			Align(fairy.Center).FontSize(12).Color(t.Destructive).PadXY(12, 6)
	case f.versions == nil:
		versions = fairy.Row(fairy.Spinner().Size(12, 12), fairy.Text(T(prefix+"loading"))).Gap(8).Align(fairy.Center).
			FontSize(12).Color(t.MutedForeground).PadXY(12, 6)
	case len(f.versions) == 0:
		versions = fairy.Text(T(prefix+"no_versions")).FontSize(12).Color(t.MutedForeground).PadXY(12, 6)
	default:
		items := make([]kit.Item, 0, len(f.versions))
		for _, version := range f.versions {
			items = append(items, kit.Item{Value: version, Label: version})
		}
		versions = kit.Select("tools.4001.version", f.version, items, func(version string) { f.version = version },
			kit.Label(T(prefix+"version")), kit.Placeholder(T(prefix+"version")), kit.MaxHeight(256)).WFull().MaxW(144)
	}

	providerField := fairy.Col(fixerLabel(T(prefix+"provider")), providers).Gap(8).MinW(0)
	versionField := fairy.Col(
		fixerLabel(T(prefix+"version")),
		versions,
		fairy.Text(T(prefix+"version_hint")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
	).Gap(8).MinW(0)
	fields := fairy.Col(providerField, versionField)
	if twoColumns {
		fields = fairy.Row(providerField.Basis(0).Grow(1), versionField.Basis(0).Grow(1))
	}
	fields.Gap(16).PadEdges(4, 14, 4, 30).BorderEdges(0, 0, 0, 1, t.Foreground.Alpha(0.2))

	label := T(prefix + "start_build")
	if f.task == "build-dll" {
		label = T(prefix + "building")
	}
	return fairy.Col(
		s.buildToolsPathField(running),
		fields,
		kit.Button(label, s.run4001Build, kit.Outline,
			kit.Disabled(running || f.importer == "" || f.versionsErr || len(f.versions) == 0)).AlignSelf(fairy.Start),
	).Gap(16)
}

func (s *Shell) fixer4001Diversify(running bool) *fairy.El {
	f := &s.tools.fixer4001
	t := kit.T()
	const prefix = "page.tools.4001_fixer."
	description, label, icon := T(prefix+"obfuscate_description"), T(prefix+"start_obfuscate"), (*svg.Vector)(nil)
	if f.backupPath != "" {
		description, label, icon = T(prefix+"restore_description"), T(prefix+"start_restore"), lucide.RotateCcw
	}
	switch f.task {
	case "restore-dll":
		label, icon = T(prefix+"restoring"), lucide.RotateCcw
	case "diversify-dll":
		label, icon = T(prefix+"obfuscating"), nil
	}
	opts := []kit.Option{kit.Outline, kit.Disabled(running || f.importer == "")}
	if icon != nil {
		opts = append(opts, kit.Icon(icon))
	}
	action := s.run4001Diversify
	if f.backupPath != "" {
		action = s.run4001Restore
	}
	return fairy.Col(
		fairy.Text(description).FontSize(14).LineHeight(20).Color(t.MutedForeground).Wrap(),
		kit.Button(label, action, opts...).AlignSelf(fairy.Start),
	).Gap(16)
}

// fixer4001ProgressText translates a progress code that is a locale key or a known XXMI code.
func fixer4001ProgressText(code string) string {
	switch {
	case strings.HasPrefix(code, "page.tools."):
		return T(code)
	case strings.HasPrefix(code, "XXMI_"):
		if key := "page.tools.4001_fixer.progress." + code; catalog.Has(key) {
			return T(key)
		}
	}
	return code
}

func (s *Shell) fixer4001Status(running bool) *fairy.El {
	f := &s.tools.fixer4001
	if f.progress == "" {
		return nil
	}
	t := kit.T()
	text := fixer4001ProgressText(f.progress)
	failed := strings.Contains(f.progress, "ERR") || strings.Contains(f.progress, "Error")
	ink := t.MutedForeground
	if failed {
		ink = t.Destructive
	}
	row := fairy.Row().Gap(8).Align(fairy.Start).FontSize(14).Medium().Color(ink)
	switch {
	case running:
		row.Child(fairy.Spinner().Size(20, 20).Shrink(0))
	case failed:
		row.Child(fairy.Icon(lucide.CircleX).Size(20, 20).Shrink(0))
	case strings.Contains(f.progress, "SUCCESS") || strings.Contains(f.progress, "ALREADY"):
		row.Child(fairy.Icon(lucide.CircleCheck).Size(20, 20).Shrink(0))
	}
	details := fairy.Col(fairy.Text(text).Wrap()).Gap(4).Grow(1).MinW(0)
	if f.backupPath != "" {
		details.Child(fairy.Text(T("page.tools.4001_fixer.backup_path", "path", f.backupPath)).FontSize(12).
			Weight(400).Color(t.MutedForeground).Wrap())
	}
	if f.errorMessage != "" {
		details.Child(
			fairy.Col(fairy.Text(f.errorMessage).Wrap()).MaxH(160).ScrollY().Pad(8).Radius(4).Border(1, t.Border).
				Fill(t.Muted.Alpha(0.4)).
				Font(monoFont).FontSize(12).Weight(400).Color(t.Destructive).MarginEdges(4, 0, 0, 0),
		)
	}
	return row.Child(details).Key("tools.4001.status").Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5)).
		Enter(fairy.Fade().With(motion.Bezier(0.15, 0.25, 0.1, 0.25, 1)))
}

func (s *Shell) buildToolsPathField(disabled bool) *fairy.El {
	b := &s.tools.fixer4001.buildTools
	t := kit.T()
	const prefix = "page.tools.4001_fixer."
	locked := b.saved != ""
	save := func() {
		candidate := strings.TrimSpace(b.draft)
		if disabled || locked || b.busy || candidate == "" {
			return
		}
		b.busy, b.err = true, ""
		s.background(func(ctx context.Context) {
			result, err := s.deps.Tools.FourThousandOneFixerSetBuildToolsPath(ctx, candidate)
			if err != nil {
				err = s.toolError(err, "4001-fixer.build-tools-path.save", map[string]any{"path": candidate})
			}
			fairy.Go(func() {
				b.busy = false
				switch {
				case err != nil:
					b.err = err.Error()
				case result.Found:
					b.saved, b.draft = result.Path, result.Path
				default:
					b.err = T(prefix + "build_tools_path_not_found")
				}
			})
		})
	}
	clearPath := func() {
		if disabled || !locked || b.busy {
			return
		}
		b.busy, b.err = true, ""
		s.background(func(ctx context.Context) {
			err := s.deps.Tools.FourThousandOneFixerClearBuildToolsPath(ctx)
			if err != nil {
				err = s.toolError(err, "4001-fixer.build-tools-path.clear", nil)
			}
			fairy.Go(func() {
				b.busy = false
				if err != nil {
					b.err = err.Error()
					return
				}
				b.saved, b.draft = "", ""
			})
		})
	}

	field := kit.TextField(b.draft, func(value string) { b.draft, b.err = value, "" },
		kit.Placeholder(T(prefix+"build_tools_path_placeholder")), kit.Label(T(prefix+"build_tools_path")),
		kit.Disabled(locked || disabled || b.busy)).Key("tools.4001.build-tools").Grow(1).MinW(0).Font(monoFont).
		OnSubmit(func(string) { save() })
	var action *fairy.El
	if locked {
		action = kit.IconButton(lucide.X, clearPath, kit.Outline, kit.Loading(b.busy),
			kit.Label(T(prefix+"build_tools_path_clear")), kit.Disabled(disabled || b.busy))
	} else {
		action = kit.Button(T(prefix+"build_tools_path_save"), save, kit.Outline, kit.Loading(b.busy),
			kit.Disabled(disabled || b.busy || strings.TrimSpace(b.draft) == ""))
	}
	hint := fairy.Text(T(prefix + "build_tools_path_hint")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap()
	if b.err != "" {
		hint = fairy.Text(b.err).FontSize(12).LineHeight(16).Color(t.Destructive).Wrap()
	}
	return fairy.Col(
		fixerLabel(T(prefix+"build_tools_path")),
		fairy.Row(field, action.Shrink(0)).Gap(8).Align(fairy.Center),
		hint,
	).Gap(8).PadEdges(4, 14, 4, 30).BorderEdges(0, 0, 0, 1, t.Foreground.Alpha(0.2))
}

// begin4001 marks task as running, or shows the importer prompt when none is selected.
func (s *Shell) begin4001(task, progress string) bool {
	f := &s.tools.fixer4001
	if f.task != "" {
		return false
	}
	if f.importer == "" {
		f.progress, f.errorMessage = "page.tools.4001_fixer.select_importer_msg", ""
		return false
	}
	f.task, f.progress, f.errorMessage = task, progress, ""
	return true
}

func (s *Shell) run4001Build() {
	f := &s.tools.fixer4001
	if !s.begin4001("build-dll", "XXMI_INIT") {
		return
	}
	f.backupPath = ""
	importer := f.importer
	input := tools.Fixer4001BuildInput{
		Provider: f.provider, Version: f.version, ImporterKey: f.importerKey, ImporterPath: &importer,
	}
	s.background(func(ctx context.Context) {
		result := s.deps.Tools.FourThousandOneFixerBuildDll(ctx, input)
		fairy.Go(func() { s.finish4001(result, true) })
	})
}

func (s *Shell) run4001Diversify() {
	f := &s.tools.fixer4001
	if !s.begin4001("diversify-dll", "XXMI_OBFUSCATE_INIT") {
		return
	}
	f.backupPath = ""
	importer := f.importer
	input := tools.Fixer4001ImporterInput{ImporterKey: f.importerKey, ImporterPath: &importer}
	s.background(func(ctx context.Context) {
		result := s.deps.Tools.FourThousandOneFixerDiversifyDllPadding(ctx, input)
		fairy.Go(func() {
			if result.BackupPath != nil && *result.BackupPath != "" {
				f.backupPath = *result.BackupPath
			}
			s.finish4001(result, false)
		})
	})
}

func (s *Shell) run4001Restore() {
	if !s.begin4001("restore-dll", "XXMI_RESTORE_INIT") {
		return
	}
	importer := s.tools.fixer4001.importer
	s.background(func(ctx context.Context) {
		result := s.deps.Tools.FourThousandOneFixerRestoreDiversifiedDll(ctx,
			tools.Fixer4001PathInput{ImporterPath: &importer})
		fairy.Go(func() { s.finish4001(result, true) })
	})
}

// finish4001 applies a task's result. The progress events end a successful task; a failed one ends here and
// reads the importer's backup again, since a failed task may have left it in place. clearBackup drops the
// backup path a successful build or restore removed.
func (s *Shell) finish4001(result tools.Fixer4001Result, clearBackup bool) {
	f := &s.tools.fixer4001
	if !result.Success {
		f.errorMessage, f.task, f.checked = deref(result.ErrorMessage), "", ""
		return
	}
	if clearBackup {
		f.backupPath = ""
	}
}

func fixerLabel(text string) *fairy.El {
	return fairy.Text(strings.ToUpper(text)).
		FontSize(12).
		LineHeight(16).
		LetterSpacing(1.2).
		Medium().
		Color(kit.T().MutedForeground)
}

// fixerChip is a toggle button of a 4001 fixer choice row.
func fixerChip(label string, selected, disabled bool, onClick func()) *fairy.El {
	t := kit.T()
	chip := fairy.Text(label).Font(monoFont).FontSize(12).LineHeight(16).PadXY(12, 6).Radius(4).Role(fairy.RoleButton).
		Label(label).Selected(selected).Disabled(disabled).Animate(motion.Fast)
	if selected {
		chip.Fill(t.Accent).Color(t.AccentForeground).Border(1, t.Accent)
	} else {
		chip.Fill(t.Secondary).Color(t.SecondaryForeground).Border(1, t.Border).HoverBorder(t.Accent.Alpha(0.5))
	}
	if disabled {
		return chip.Opacity(0.5)
	}
	return chip.Cursor(fairy.CursorPointer).Focusable().OnClick(onClick)
}
