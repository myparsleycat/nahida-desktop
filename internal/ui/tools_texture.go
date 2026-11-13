package ui

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/tools/texture"
)

const (
	textureUpscaleProgressEvent = "tools:textureUpscaleProgress"
	textureResizeProgressEvent  = "tools:textureResizeProgress"
)

var defaultTextureSettings = tools.TextureResizeSettings{
	Mode: "custom", Operation: "resize", Percent: 50, CustomWidth: 2048, CustomHeight: 2048, Backup: true,
	UpscaleScale: 2, UpscaleModel: "realesr-animevideov3",
}

// textureWorkspace lists the textures of one mod and processes the selected ones through a dialog. base is the
// stored resize settings the listing and the dialog start from.
type textureWorkspace struct {
	modPath, modName string
	started          bool
	base             tools.TextureResizeSettings

	loaded   string
	textures []tools.TextureResizeListItem
	selected map[string]bool
	listing  bool
	listSeq  int

	dialog *textureDialog
}

func newTextureWorkspace(modPath, modName string) *textureWorkspace {
	return &textureWorkspace{
		modPath: modPath, modName: modName, base: defaultTextureSettings, selected: map[string]bool{},
	}
}

// textureDialog is the process dialog over the selected textures. settings is the form: the shared settings when
// bulk applies, else those of the active texture, whose edits are kept in per.
type textureDialog struct {
	items    []tools.TextureResizeListItem
	bulk     bool
	active   string
	per      map[string]tools.TextureResizeSettings
	settings tools.TextureResizeSettings
	// width and height hold the custom size fields while they are edited.
	width, height *string

	running        bool
	runningFile    string
	current, total int
	upscale        *tools.TextureUpscaleProgressEvent
	// cancel stops the running batch; it is nil until the batch has started.
	cancel     context.CancelFunc
	cancelling bool
}

func (d *textureDialog) activeItem() *tools.TextureResizeListItem {
	for i := range d.items {
		if d.items[i].FilePath == d.active {
			return &d.items[i]
		}
	}
	if len(d.items) > 0 {
		return &d.items[0]
	}
	return nil
}

func (d *textureDialog) shared() bool { return d.bulk && len(d.items) > 1 }

// settingsFor are the settings item is processed with.
func (d *textureDialog) settingsFor(item tools.TextureResizeListItem) tools.TextureResizeSettings {
	if d.bulk || len(d.items) == 1 {
		return texture.AdaptSettings(d.settings, item)
	}
	if settings, ok := d.per[item.FilePath]; ok {
		return settings
	}
	return d.settings
}

func (d *textureDialog) setSettings(settings tools.TextureResizeSettings) {
	d.settings = settings
	if !d.bulk && d.active != "" {
		d.per[d.active] = settings
	}
}

func (s *Shell) startTextureWorkspace(w *textureWorkspace) {
	if w.started {
		return
	}
	w.started = true
	s.background(func(ctx context.Context) {
		settings, err := s.deps.Tools.GetTextureResizeSettings(ctx)
		if err != nil {
			err = s.toolError(err, "texture.settings.load", nil)
		}
		fairy.Go(func() {
			if err != nil {
				s.toasts.Show(kit.ToastError, T("page.tools.texture_resizer.toast.load_failed"), err.Error())
				return
			}
			w.base = settings
			s.loadTextures(w, settings)
		})
	})
}

// loadTextures lists the workspace's mod with settings. A newer listing drops the result of an earlier one.
func (s *Shell) loadTextures(w *textureWorkspace, settings tools.TextureResizeSettings) {
	target := w.modPath
	w.listSeq++
	seq := w.listSeq
	w.listing = true
	patch := texture.SettingsPatch(settings)
	s.background(func(ctx context.Context) {
		textures, err := s.deps.Tools.ListTextureMod(ctx, target, &patch)
		if err != nil {
			err = s.toolError(err, "texture.list", map[string]any{"path": target})
		}
		fairy.Go(func() {
			if seq != w.listSeq {
				return
			}
			w.listing = false
			if err != nil {
				s.toasts.Show(kit.ToastError, T("page.tools.texture_resizer.toast.load_failed"), err.Error())
				return
			}
			w.textures, w.loaded = textures, target
			available := map[string]bool{}
			for _, item := range textures {
				if item.CanProcess {
					available[item.FilePath] = true
				}
			}
			for path := range w.selected {
				if !available[path] {
					delete(w.selected, path)
				}
			}
		})
	})
}

func (s *Shell) textureWorkspaceView(cx *fairy.Cx, w *textureWorkspace, key string) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	s.startTextureWorkspace(w)
	busy := w.listing || (w.dialog != nil && w.dialog.running)

	view := fairy.Col().Gap(16).Grow(1).MinH(0).WFull()
	toolbar := fairy.Row().Gap(8).Wrap().RowGap(8).Align(fairy.Center)
	toolbar.Child(kit.Button(T(prefix+"refresh"), func() { s.loadTextures(w, w.base) }, kit.Outline,
		kit.Icon(lucide.RefreshCw), kit.Loading(w.listing), kit.Disabled(busy)))
	if len(w.textures) > 0 {
		toolbar.Child(
			fairy.Spacer(),
			fairy.Text(T(prefix+"selected_count", "count", len(w.selected))).
				FontSize(12).
				LineHeight(16).
				Color(t.MutedForeground),
			kit.Button(T(prefix+"process_selected"), func() { s.openTextureDialog(w) }, kit.Icon(lucide.Image),
				kit.Disabled(len(w.selected) == 0 || busy)),
		)
	}
	view.Child(toolbar)

	description := w.modName
	if description == "" {
		description = T(prefix + "texture_list_empty")
	}
	view.Child(fairy.Col(
		fairy.Col(
			fairy.Text(T(prefix+"texture_list_title")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
			fairy.Text(description).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
		).Gap(2).PadEdges(0, 4, 8, 4),
		s.textureList(w, key, busy).Grow(1).MinH(0),
	).Gap(4).Grow(1).MinH(0))
	return view
}

const textureRowHeight float32 = 40

func (s *Shell) textureList(w *textureWorkspace, key string, busy bool) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	if len(w.textures) == 0 {
		text := T(prefix + "texture_list_empty")
		if w.listing {
			text = T(prefix + "loading_textures")
		}
		return fairy.Col(fairy.Col(fairy.Text(text).FontSize(14).LineHeight(20).Color(t.MutedForeground).
			TextAlign(fairy.TextCenter).Wrap()).PadXY(16, 40).Center().Radius(6).Margin(16).
			Border(1, t.Border).BorderDashed().Fill(t.Background.Alpha(0.4))).WFull().MinH(0).ScrollY()
	}

	processable, selected := 0, 0
	for _, item := range w.textures {
		if item.CanProcess {
			processable++
			if w.selected[item.FilePath] {
				selected++
			}
		}
	}
	all := processable > 0 && selected == processable
	selectAll := kit.Checkbox(all, func(on bool) {
		clear(w.selected)
		if on {
			for _, item := range w.textures {
				if item.CanProcess {
					w.selected[item.FilePath] = true
				}
			}
		}
	}, kit.Disabled(busy || processable == 0), kit.Indeterminate(selected > 0 && !all)).Label(T(prefix + "select_all"))

	running := ""
	if w.dialog != nil {
		running = w.dialog.runningFile
	}
	rows := fairy.VirtualList(len(w.textures), textureRowHeight, func(i int) *fairy.El {
		return s.textureRow(w, w.textures[i], running == w.textures[i].FilePath, busy)
	}).Key(key + ".list").Grow(1).MinH(0)
	return fairy.Col(
		fairy.Row(fairy.Spacer(), selectAll).PadEdges(0, 24, 4, 0),
		rows,
	).Gap(4)
}

func (s *Shell) textureRow(w *textureWorkspace, item tools.TextureResizeListItem, running, busy bool) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	selected := w.selected[item.FilePath]
	canSelect := item.CanProcess && !busy
	toggle := func(on bool) {
		if on {
			w.selected[item.FilePath] = true
		} else {
			delete(w.selected, item.FilePath)
		}
	}
	muted := func(text string) *fairy.El {
		return fairy.Text(text).FontSize(12).LineHeight(16).Color(t.MutedForeground).NoWrap().Shrink(0)
	}
	check := fairy.Row().Gap(8).Align(fairy.Center).Shrink(0)
	if running {
		check.Child(fairy.Spinner().Size(16, 16))
	}
	check.Child(kit.Checkbox(selected, toggle, kit.Disabled(busy || !item.CanProcess)).Label(item.FileName))
	row := fairy.Row(
		fairy.Text(item.FileName).FontSize(14).LineHeight(20).Medium().Ellipsis().Grow(1).MinW(0).
			Cursor(fairy.CursorPointer).OnClick(func() { s.openExternal(item.FilePath) }),
		muted(texture.FormatLabel(item.Format)+" / "+T(prefix+"color_space."+item.ColorSpace)),
		muted(formatSize(item.FileSize)),
		muted(fmt.Sprintf("%dx%d", item.OriginalWidth, item.OriginalHeight)),
		check,
	).Key(item.FilePath).H(textureRowHeight).PadEdges(0, 24, 0, 12).Gap(16).Align(fairy.Center).
		HoverFill(t.Muted.Alpha(0.5)).Animate(motion.Fast).BorderEdges(0, 0, 1, 0, t.Foreground.Alpha(0.15))
	if selected {
		row.Fill(t.Muted.Alpha(0.5))
	}
	if canSelect {
		row.Cursor(fairy.CursorPointer).OnClick(func() { toggle(!selected) })
	}
	return row
}

func (s *Shell) openTextureDialog(w *textureWorkspace) {
	var items []tools.TextureResizeListItem
	for _, item := range w.textures {
		if w.selected[item.FilePath] {
			items = append(items, item)
		}
	}
	if len(items) == 0 {
		return
	}
	d := &textureDialog{
		items:  items,
		bulk:   len(items) > 1,
		active: items[0].FilePath,
		per:    map[string]tools.TextureResizeSettings{},
	}
	for _, item := range items {
		d.per[item.FilePath] = texture.ItemSettings(w.base, item)
	}
	if d.bulk {
		d.settings = texture.SharedSettings(w.base, items)
	} else {
		d.settings = d.per[items[0].FilePath]
	}
	w.dialog = d
}

func (s *Shell) textureProcessDialog(cx *fairy.Cx, w *textureWorkspace, key string) {
	d := w.dialog
	if d == nil {
		return
	}
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	active := d.activeItem()
	description := T(prefix + "dialog.description_fallback")
	switch {
	case d.shared():
		description = T(prefix+"dialog.description_multiple", "count", len(d.items))
	case active != nil:
		description = active.FileName
	}

	body := fairy.Col().Gap(12)
	if len(d.items) > 1 {
		body.Child(settingRow(T(prefix+"bulk_apply"), T(prefix+"bulk_apply_description"),
			kit.Switch(d.bulk, func(on bool) { s.setTextureBulk(d, on) }, kit.Disabled(d.running)).
				Label(T(prefix+"bulk_apply"))).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5)))
	}
	if !d.bulk && len(d.items) > 1 {
		chips := fairy.Row().Gap(4).Wrap().RowGap(4)
		for _, item := range d.items {
			variant := kit.Outline
			if item.FilePath == d.active {
				variant = kit.Default
			}
			chips.Child(kit.Button(item.FileName, func() { s.selectTextureDialogItem(d, item.FilePath) }, variant,
				kit.Small, kit.Disabled(d.running)))
		}
		body.Child(chips)
	}
	var formItem *tools.TextureResizeListItem
	if !d.shared() {
		formItem = active
	}
	if formItem != nil {
		target := fmt.Sprintf("%dx%d", formItem.TargetWidth, formItem.TargetHeight)
		if size, ok := texture.PreviewSize(*formItem, d.settings); ok {
			target = fmt.Sprintf("%dx%d", size.Width, size.Height)
		}
		output := d.settings.OutputFormat
		if output == "" {
			output = formItem.OutputFormatDefault
		}
		info := fairy.Col(
			fairy.Text(formItem.RelativePath).Wrap(),
			fairy.Text(fmt.Sprintf("%dx%d -> %s", formItem.OriginalWidth, formItem.OriginalHeight, target)),
			fairy.Text(texture.FormatLabel(formItem.Format)+" / "+T(prefix+"color_space."+formItem.ColorSpace)),
			fairy.Text(T(prefix+"current_output_format")+": "+texture.FormatLabel(output)),
		).Gap(4).FontSize(12).Color(t.MutedForeground)
		if formItem.FormatConversionMessage != nil {
			info.Child(fairy.Text(*formItem.FormatConversionMessage).Wrap())
		}
		if formItem.Message != nil && texture.IsUpscaleOperation(d.settings.Operation) {
			info.Child(fairy.Text(*formItem.Message).Wrap())
		}
		body.Child(info)
	}
	if d.running {
		name := ""
		for _, item := range d.items {
			if item.FilePath == d.runningFile {
				name = item.FileName + " "
			}
		}
		progress := fairy.Col(fairy.Text(name + T(prefix+"batch_progress", "current", d.current, "total", d.total)).
			FontSize(12).Color(t.MutedForeground)).Gap(8).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
		if u := d.upscale; u != nil {
			message := T(prefix + "upscale_progress.working")
			if u.Message != nil {
				message = *u.Message
			}
			var bar *fairy.El
			if u.Percent != nil {
				bar = kit.Progress(*u.Percent / 100)
			} else {
				bar = kit.Progress(0).Pulse(2, 0.5)
			}
			progress.Child(fairy.Text(message).FontSize(12).Color(t.MutedForeground), bar)
		}
		body.Child(progress)
	}

	form := textureFormProps{disabled: d.running, shared: d.shared()}
	if d.shared() {
		form.formats = texture.SharedOutputFormats(d.items)
		if len(form.formats) > 0 {
			form.currentFormat = form.formats[0]
		}
		form.colorSpace = texture.SharedColorSpace(d.items)
		form.conversionMessage = texture.SharedConversionMessage(d.items)
	} else if formItem != nil {
		form.formats = formItem.AvailableOutputFormats
		form.currentFormat = formItem.OutputFormatDefault
		form.colorSpace = formItem.ColorSpace
		form.conversionMessage = formItem.FormatConversionMessage
		form.source = &texture.Size{Width: formItem.OriginalWidth, Height: formItem.OriginalHeight}
	}
	body.Child(s.textureForm(d, form, key))

	canProcess := false
	for _, item := range d.items {
		if texture.CanRun(item, d.settingsFor(item)) {
			canProcess = true
		}
	}
	label := T(prefix + "process_single")
	switch {
	case d.running:
		label = T(prefix + "running")
	case len(d.items) > 1:
		label = T(prefix + "process_selected")
	}
	closeDialog := func() {
		if !d.running {
			w.dialog = nil
		}
	}
	cancelAction := closeDialog
	if d.running {
		cancelAction = func() {
			if d.cancel != nil && !d.cancelling {
				d.cancelling = true
				d.cancel()
			}
		}
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + "dialog.title"),
		Description: description,
		Body:        body,
		Width:       600,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), cancelAction, kit.Outline,
				kit.Disabled(d.running && (d.cancel == nil || d.cancelling))),
			kit.Button(label, func() { s.processTextures(w, d) }, kit.Icon(lucide.Image), kit.Loading(d.running),
				kit.Disabled(len(d.items) == 0 || d.running || !canProcess)),
		},
		OnClose: closeDialog,
		Key:     key + ".dialog",
	})
}

func (s *Shell) setTextureBulk(d *textureDialog, bulk bool) {
	if bulk {
		d.per[d.active] = d.settings
		d.settings = texture.SharedSettings(d.settings, d.items)
		d.bulk, d.width, d.height = true, nil, nil
		return
	}
	for _, item := range d.items {
		d.per[item.FilePath] = texture.ItemSettings(d.settings, item)
	}
	d.active = d.items[0].FilePath
	d.settings = d.per[d.active]
	d.bulk, d.width, d.height = false, nil, nil
}

func (s *Shell) selectTextureDialogItem(d *textureDialog, path string) {
	if path == d.active {
		return
	}
	d.per[d.active] = d.settings
	d.active = path
	d.settings = d.per[path]
	d.width, d.height = nil, nil
}

func (s *Shell) processTextures(w *textureWorkspace, d *textureDialog) {
	if d.running {
		return
	}
	d.per[d.active] = d.settings
	type job struct {
		item     tools.TextureResizeListItem
		settings tools.TextureResizeSettings
	}
	jobs := make([]job, 0, len(d.items))
	for _, item := range d.items {
		jobs = append(jobs, job{item, d.settingsFor(item)})
	}
	d.running, d.total, d.current, d.upscale = true, len(jobs), 0, nil
	d.cancel, d.cancelling = nil, false
	s.tools.textureRun = d

	s.background(func(parent context.Context) {
		ctx, cancel := context.WithCancel(parent)
		defer cancel()
		fairy.Go(func() { d.cancel = cancel })
		var result textureBatchResult
		result.selected = len(jobs)
		for i, j := range jobs {
			if ctx.Err() != nil {
				result.cancelled = true
				break
			}
			fairy.Go(func() { d.current, d.runningFile, d.upscale = i+1, "", nil })
			if !texture.CanRun(j.item, j.settings) {
				result.skipped++
				continue
			}
			fairy.Go(func() { d.runningFile = j.item.FilePath })
			result.saved = &j.settings
			out, err := s.deps.Tools.ResizeTextureFile(ctx, tools.TextureResizeFileRunInput{
				FilePath: j.item.FilePath, Settings: j.settings,
			})
			if infra.IsCancellationError(err) {
				result.cancelled = true
				break
			}
			if err != nil {
				err = s.toolError(err, "texture.process", map[string]any{
					"path": j.item.FilePath, "operation": j.settings.Operation,
				})
				result.failed++
				if result.err == "" {
					result.err = err.Error()
				}
				continue
			}
			var file *tools.TextureResizeFileResult
			if len(out.Files) > 0 {
				file = &out.Files[0]
			}
			result.last = file
			switch {
			case file != nil && file.Status == "updated":
				result.updated++
			case file == nil || file.Status == "failed":
				result.failed++
				if result.err == "" && file != nil && file.Message != nil {
					result.err = *file.Message
				}
			default:
				result.skipped++
			}
		}
		fairy.Go(func() {
			if s.tools.textureRun == d {
				s.tools.textureRun = nil
			}
			d.running, d.cancel = false, nil
			w.dialog = nil
			clear(w.selected)
			s.showTextureResult(result)
			if result.saved != nil {
				w.base = *result.saved
			}
			if w.loaded != "" {
				s.loadTextures(w, w.base)
			}
		})
	})
}

type textureBatchResult struct {
	selected, updated, failed, skipped int
	last                               *tools.TextureResizeFileResult
	err                                string
	cancelled                          bool
	// saved is the settings of the last texture processed, which the backend stored as the resize settings.
	saved *tools.TextureResizeSettings
}

func (s *Shell) showTextureResult(r textureBatchResult) {
	const prefix = "page.tools.texture_resizer.toast."
	description := T(prefix+"completed_description", "updated", r.updated, "failed", r.failed, "skipped", r.skipped)
	withError := description
	if r.err != "" {
		withError = description + "\n" + r.err
	}
	switch {
	case r.cancelled:
		s.toasts.Show(kit.ToastWarning, T(prefix+"cancelled"), withError)
	case r.selected == 1 && r.updated == 1:
		detail := T(prefix + "single_completed_fallback")
		if f := r.last; f != nil {
			detail = fmt.Sprintf("%dx%d -> %dx%d, %s -> %s", f.OriginalWidth, f.OriginalHeight, f.OutputWidth,
				f.OutputHeight, texture.FormatLabel(f.OriginalFormat), texture.FormatLabel(f.OutputFormat))
		}
		s.toasts.Show(kit.ToastSuccess, T(prefix+"single_completed"), detail)
	case r.selected == 1 && r.failed > 0:
		detail := description
		if r.err != "" {
			detail = r.err
		} else if r.last != nil && r.last.Message != nil {
			detail = *r.last.Message
		}
		s.toasts.Show(kit.ToastError, T(prefix+"failed"), detail)
	case r.updated == 0 && r.failed > 0:
		s.toasts.Show(kit.ToastError, T(prefix+"failed"), withError)
	case r.updated == 0:
		s.toasts.Show(kit.ToastWarning, T(prefix+"none_processed"), description)
	case r.failed > 0:
		s.toasts.Show(kit.ToastWarning, T(prefix+"completed"), withError)
	default:
		s.toasts.Show(kit.ToastSuccess, T(prefix+"completed"), description)
	}
}

// textureFormProps describe the texture or textures the form edits.
type textureFormProps struct {
	disabled          bool
	shared            bool
	formats           []string
	currentFormat     string
	colorSpace        string
	conversionMessage *string
	// source is the size of the single texture being edited, nil when the form edits several.
	source *texture.Size
}

func textureFieldLabel(text string) *fairy.El { return fixerLabel(text) }

func textureHint(text string) *fairy.El {
	return fairy.Text(text).FontSize(12).Color(kit.T().MutedForeground).Wrap()
}

func textureField(label string, control *fairy.El, hint string) *fairy.El {
	field := fairy.Col(textureFieldLabel(label), control).Gap(8).Basis(240).Grow(1).MinW(0)
	if hint != "" {
		field.Child(textureHint(hint))
	}
	return field
}

func (s *Shell) textureForm(d *textureDialog, p textureFormProps, key string) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	settings := d.settings
	update := func(change func(*tools.TextureResizeSettings)) {
		next := d.settings
		change(&next)
		d.setSettings(next)
	}
	disabled := kit.Disabled(p.disabled)

	operations := []string{"resize", "resize_and_convert", "convert", "upscale", "upscale_and_convert"}
	operationItems := make([]kit.Item, 0, len(operations))
	for _, op := range operations {
		operationItems = append(operationItems, kit.Item{Value: op, Label: T(prefix + "operation_options." + op)})
	}
	operationField := textureField(T(prefix+"operation"),
		kit.Select(key+".operation", settings.Operation, operationItems, func(op string) {
			update(func(s *tools.TextureResizeSettings) {
				s.Operation = op
				if texture.IsUpscaleOperation(op) {
					s.UpscaleScale = texture.ResolveUpscaleScale(s.UpscaleModel, s.UpscaleScale)
				}
				if p.source != nil {
					*s = texture.FitResize(*s, p.source.Width, p.source.Height)
				}
			})
		}, disabled, kit.Label(T(prefix+"operation"))).W(224),
		T(prefix+"operation_descriptions."+settings.Operation))

	backupField := settingRow(T(prefix+"backup"), T(prefix+"backup_description"),
		kit.Switch(settings.Backup, func(on bool) { update(func(s *tools.TextureResizeSettings) { s.Backup = on }) },
			disabled).Label(T(prefix+"backup"))).PadXY(12, 0).Radius(6).Fill(t.Muted.Alpha(0.5)).
		Basis(240).Grow(1).MinW(0)

	upscale := texture.IsUpscaleOperation(settings.Operation)
	resizeInputs := settings.Operation != "convert" && !upscale
	outputFormat := settings.Operation != "resize" && settings.Operation != "upscale"
	horizontalConvert := settings.Operation == "convert"

	colorSpaceEnabled := outputFormat && p.colorSpace == "unknown" && len(p.formats) > 0
	formatColor := settings.OutputFormat
	if formatColor == "" {
		formatColor = p.currentFormat
	}
	if formatColor == "" && len(p.formats) > 0 {
		formatColor = p.formats[0]
	}
	outputColor := "linear"
	if strings.HasSuffix(formatColor, "_SRGB") {
		outputColor = "srgb"
	}
	visible := p.formats
	if colorSpaceEnabled {
		visible = nil
		for _, format := range p.formats {
			if strings.HasSuffix(format, "_SRGB") == (outputColor == "srgb") {
				visible = append(visible, format)
			}
		}
	}
	formatValue := ""
	switch {
	case settings.OutputFormat != "" && containsString(visible, settings.OutputFormat):
		formatValue = settings.OutputFormat
	case p.currentFormat != "" && containsString(visible, p.currentFormat):
		formatValue = p.currentFormat
	case len(visible) > 0:
		formatValue = visible[0]
	}

	var colorSpaceField *fairy.El
	if colorSpaceEnabled {
		colorSpaceField = textureField(T(prefix+"output_color_space"),
			kit.Select(key+".color-space", outputColor, []kit.Item{
				{Value: "srgb", Label: T(prefix + "color_space.srgb")},
				{Value: "linear", Label: T(prefix + "color_space.linear")},
			}, func(color string) {
				update(func(s *tools.TextureResizeSettings) {
					s.OutputFormat = ""
					for _, format := range p.formats {
						if strings.HasSuffix(format, "_SRGB") == (color == "srgb") {
							s.OutputFormat = format
							break
						}
					}
				})
			}, disabled, kit.Label(T(prefix+"output_color_space"))),
			T(prefix+"output_color_space_description"))
	}
	formatItems := make([]kit.Item, 0, len(visible))
	for _, format := range visible {
		formatItems = append(formatItems, kit.Item{Value: format, Label: texture.FormatLabel(format)})
	}
	formatHint := T(prefix + "output_format_description")
	if p.conversionMessage != nil {
		formatHint = *p.conversionMessage
	}
	outputFormatField := textureField(T(prefix+"output_format"),
		kit.Select(key+".format", formatValue, formatItems, func(format string) {
			update(func(s *tools.TextureResizeSettings) { s.OutputFormat = format })
		}, kit.Disabled(p.disabled || len(visible) == 0), kit.Label(T(prefix+"output_format"))).W(208),
		formatHint)

	var formatFields []*fairy.El
	if outputFormat {
		if colorSpaceField != nil {
			formatFields = append(formatFields, colorSpaceField)
		}
		formatFields = append(formatFields, outputFormatField)
	}

	var grid, full []*fairy.El
	switch {
	case upscale:
		grid = append(append(grid, operationField), s.textureUpscaleFields(update, settings, key, disabled)...)
		if p.colorSpace == "linear" {
			grid = append(grid, textureHint(T(prefix+"upscale_normal_warning")).Pad(12).Radius(6).
				Fill(t.Muted.Alpha(0.5)).Basis(240).Grow(1))
		}
		grid = append(append(grid, formatFields...), backupField)
	case resizeInputs:
		modeField := textureHint(T(prefix + "custom_hint")).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5)).
			Basis(240).Grow(1)
		if p.shared {
			modeField = textureField(T(prefix+"mode"),
				kit.Select(key+".mode", settings.Mode, []kit.Item{
					{Value: "percent", Label: T(prefix + "mode_options.percent")},
					{Value: "custom", Label: T(prefix + "mode_options.custom")},
				}, func(mode string) { update(func(s *tools.TextureResizeSettings) { s.Mode = mode }) }, disabled,
					kit.Label(T(prefix+"mode"))).W(224),
				T(prefix+"mode_descriptions."+settings.Mode))
		}
		grid = append(append(grid, operationField, modeField), formatFields...)
		grid = append(grid, backupField)
		switch {
		case p.shared && settings.Mode == "percent":
			full = append(full, s.texturePercent(update, settings, disabled))
		case p.shared:
			full = append(full, s.textureCustomSize(d, update, key))
		default:
			full = append(full, s.textureResizeStep(update, settings, p.source, disabled))
		}
	case horizontalConvert && outputFormat:
		grid = append(append(grid, operationField), formatFields...)
		grid = append(grid, backupField)
	default:
		grid = append(grid, operationField, backupField)
		full = append(full, formatFields...)
	}
	return fairy.Col(append([]*fairy.El{fairy.Row(grid...).Gap(16).Wrap().RowGap(16)}, full...)...).Gap(16)
}

func (s *Shell) textureUpscaleFields(
	update func(func(*tools.TextureResizeSettings)),
	settings tools.TextureResizeSettings,
	key string,
	disabled kit.Option,
) []*fairy.El {
	const prefix = "page.tools.texture_resizer."
	var models []kit.Item
	for _, group := range texture.UpscaleModelGroups {
		models = append(models, kit.Item{Label: T(prefix + "upscale_engine_labels." + group.Engine), Heading: true})
		for _, model := range group.Models {
			models = append(models, kit.Item{Value: model, Label: T(prefix + "upscale_model_options." + model)})
		}
	}
	var scales []kit.Item
	for _, scale := range texture.UpscaleScales(settings.UpscaleModel) {
		scales = append(
			scales,
			kit.Item{Value: strconv.Itoa(scale), Label: T(prefix+"upscale_scale_option", "scale", scale)},
		)
	}
	selected := texture.ResolveUpscaleScale(settings.UpscaleModel, settings.UpscaleScale)
	return []*fairy.El{
		textureField(T(prefix+"upscale_model"),
			kit.Select(key+".upscale-model", settings.UpscaleModel, models, func(model string) {
				update(func(s *tools.TextureResizeSettings) {
					s.UpscaleModel = model
					s.UpscaleScale = texture.ResolveUpscaleScale(model, s.UpscaleScale)
				})
			}, disabled, kit.Label(T(prefix+"upscale_model"))),
			T(prefix+"upscale_model_descriptions."+settings.UpscaleModel)),
		textureField(T(prefix+"upscale_scale"),
			kit.Select(key+".upscale-scale", strconv.Itoa(selected), scales, func(value string) {
				scale, _ := strconv.Atoi(value)
				update(func(s *tools.TextureResizeSettings) {
					s.UpscaleScale = texture.ResolveUpscaleScale(s.UpscaleModel, scale)
				})
			}, disabled, kit.Label(T(prefix+"upscale_scale"))),
			T(prefix+"upscale_scale_description")),
	}
}

func (s *Shell) texturePercent(
	update func(func(*tools.TextureResizeSettings)),
	settings tools.TextureResizeSettings,
	disabled kit.Option,
) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	return fairy.Col(
		fairy.Row(
			fairy.Col(
				fairy.Text(T(prefix+"percent")).FontSize(14).Medium(),
				textureHint(T(prefix+"mode_descriptions.percent")),
			).Gap(2).Grow(1).MinW(0),
			fairy.Text(strconv.Itoa(settings.Percent)+"%").FontSize(14).Medium(),
		).Gap(12).Align(fairy.Start),
		kit.Slider(float64(settings.Percent), 1, 99, 1, func(value float64) {
			update(func(s *tools.TextureResizeSettings) { s.Percent, s.Mode = int(math.Round(value)), "percent" })
		}, disabled, kit.Label(T(prefix+"percent"))),
	).Gap(12).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
}

func (s *Shell) textureCustomSize(
	d *textureDialog,
	update func(func(*tools.TextureResizeSettings)),
	key string,
) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	field := func(label string, draft **string, value int, set func(*tools.TextureResizeSettings, int)) *fairy.El {
		text := strconv.Itoa(value)
		if *draft != nil {
			text = **draft
		}
		commit := func() {
			if *draft == nil {
				return
			}
			n, _ := strconv.Atoi(strings.TrimSpace(**draft))
			*draft = nil
			update(func(s *tools.TextureResizeSettings) { set(s, snapTextureDimension(n)); s.Mode = "custom" })
		}
		return textureField(label, kit.TextField(text, func(value string) { *draft = &value },
			kit.Label(label), kit.Disabled(d.running)).Key(key+"."+label).OnBlur(commit).
			OnSubmit(func(string) { commit() }), "")
	}
	return fairy.Col(
		fairy.Row(
			field(T(prefix+"custom_width"), &d.width, d.settings.CustomWidth,
				func(s *tools.TextureResizeSettings, n int) { s.CustomWidth = n }),
			field(T(prefix+"custom_height"), &d.height, d.settings.CustomHeight,
				func(s *tools.TextureResizeSettings, n int) { s.CustomHeight = n }),
		).Gap(16).Wrap().RowGap(16),
		textureHint(T(prefix+"custom_hint")),
	).Gap(12).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
}

func (s *Shell) textureResizeStep(
	update func(func(*tools.TextureResizeSettings)),
	settings tools.TextureResizeSettings,
	source *texture.Size,
	disabled kit.Option,
) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.texture_resizer."
	var candidates []texture.Size
	if source != nil {
		candidates = texture.ResizeCandidates(source.Width, source.Height)
	}
	if len(candidates) == 0 {
		return textureHint(T(prefix + "resize_step_unavailable")).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
	}
	selected, ok := texture.PickResizeCandidate(candidates, settings.CustomWidth, settings.CustomHeight)
	if !ok {
		selected = candidates[0]
	}
	index := 0
	for i, c := range candidates {
		if c == selected {
			index = i
		}
	}
	sizeLabel := func(c texture.Size) string {
		return resizePercent(c.Width, source.Width) + " / " + fmt.Sprintf("%dx%d", c.Width, c.Height)
	}
	step := fairy.Col(
		fairy.Row(
			fairy.Col(
				fairy.Text(T(prefix+"resize_step")).FontSize(14).Medium(),
				textureHint(T(prefix+"resize_step_description")),
			).Gap(2).Grow(1).MinW(0),
			fairy.Col(
				fairy.Text(resizePercent(selected.Width, source.Width)).FontSize(14).Medium(),
				fairy.Text(fmt.Sprintf("%dx%d", selected.Width, selected.Height)).FontSize(12).Color(t.MutedForeground),
			).Gap(2).Align(fairy.End),
		).Gap(12).Align(fairy.Start),
	).Gap(12).Pad(12).Radius(6).Fill(t.Muted.Alpha(0.5))
	if len(candidates) > 1 {
		step.Child(
			kit.Slider(float64(index), 0, float64(len(candidates)-1), 1, func(value float64) {
				c := candidates[min(max(int(math.Round(value)), 0), len(candidates)-1)]
				update(func(s *tools.TextureResizeSettings) {
					s.CustomWidth, s.CustomHeight, s.Mode = c.Width, c.Height, "custom"
				})
			}, disabled, kit.Label(T(prefix+"resize_step"))),
			fairy.Row(
				fairy.Text(sizeLabel(candidates[0])),
				fairy.Spacer(),
				fairy.Text(sizeLabel(candidates[len(candidates)-1])),
			).Gap(12).FontSize(12).Color(t.MutedForeground),
		)
	}
	return step
}

// snapTextureDimension rounds a custom size to the nearest multiple of 1024, at least 1024.
func snapTextureDimension(v int) int {
	v = max(1024, v)
	switch r := v % 1024; {
	case r == 0:
		return v
	case r < 512:
		return v - r
	default:
		return v + 1024 - r
	}
}

func resizePercent(width, original int) string {
	percent := float64(width) / float64(original) * 100
	if math.Abs(percent-math.Round(percent)) < 0.05 {
		return strconv.Itoa(int(math.Round(percent))) + "%"
	}
	return strconv.FormatFloat(percent, 'f', 1, 64) + "%"
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// openTextureResize opens the texture resizer on one mod.
func (s *Shell) openTextureResize(info mod.ModInfo) {
	s.mods.texture = newTextureWorkspace(info.Path, info.Name)
}

func (s *Shell) textureResizeDialog(cx *fairy.Cx) {
	w := s.mods.texture
	if w == nil {
		return
	}
	closeDialog := func() {
		if w.dialog == nil || !w.dialog.running {
			s.mods.texture = nil
		}
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T("page.tools.texture_resizer.title"),
		Body:  fairy.Col(s.textureWorkspaceView(cx, w, "mod.texture")).H(600),
		Width: 960, OnClose: closeDialog, Key: "mod.texture",
	})
	s.textureProcessDialog(cx, w, "mod.texture")
}
