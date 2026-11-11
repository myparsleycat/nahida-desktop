package ui

import (
	"context"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/feedback"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// feedbackCategories are the categories reported from the app; the website category is offered on the website.
var feedbackCategories = []string{"desktop", "service", "xxmi-libs"}

// feedbackErrorCodes prefix the submit errors the screen explains.
var feedbackErrorCodes = []string{
	"FEEDBACK_UNSUPPORTED_FILE",
	"FEEDBACK_MEDIA_TOO_LARGE",
	"FEEDBACK_TOO_MANY_MEDIA",
	"FEEDBACK_TOO_MANY_MOD_FILES",
	"FEEDBACK_EMPTY_MOD",
	"FEEDBACK_BUSY",
}

type feedbackSection struct {
	started      bool
	rules        *feedback.Rules
	rulesLoading bool
	rulesFailed  bool
	category     string
	body         string
	includeLog   bool
	attachments  []feedback.Attachment
	// progress is the upload percentage while a report is sent, nil otherwise.
	progress  *float64
	cancelled bool
}

// setUploadProgress updates the upload percentage of a report being sent; progress is nil while none is.
func setUploadProgress(progress **float64, p feedback.Progress) {
	if *progress == nil {
		return
	}
	value := 100.0
	if p.TotalBytes > 0 {
		value = float64(p.UploadedBytes) / float64(p.TotalBytes) * 100
	}
	*progress = &value
}

func (f *feedbackSection) media() (files []feedback.Attachment, bytes int64) {
	for _, item := range f.attachments {
		if item.Kind != feedback.KindMod {
			files = append(files, item)
			bytes += item.Size
		}
	}
	return files, bytes
}

// problem is the first bound the report breaks, as the message to show, or "" when the server would take it.
func (f *feedbackSection) problem() string {
	files, bytes := f.media()
	return feedbackProblem(f.rules, f.rulesFailed, f.body, len(files), bytes)
}

// feedbackProblem is the first bound a report of body and media breaks, as the message to show, or "".
func feedbackProblem(rules *feedback.Rules, rulesFailed bool, body string, files int, bytes int64) string {
	const prefix = "page.setting.feedback.error."
	if rules == nil {
		if rulesFailed {
			return T(prefix + "rules_unavailable")
		}
		return ""
	}
	switch {
	case bodyLength(body) > rules.MaxBodyLength:
		return T(prefix+"body_too_long", "max", catalog.Integer(int64(rules.MaxBodyLength)))
	case files > rules.MaxMediaFiles:
		return T(prefix+"FEEDBACK_TOO_MANY_MEDIA", "count", rules.MaxMediaFiles)
	case bytes > rules.MaxMediaBytes:
		return T(prefix+"FEEDBACK_MEDIA_TOO_LARGE", "size", formatSize(rules.MaxMediaBytes))
	}
	return ""
}

func (s *Shell) loadFeedbackRules() {
	f := &s.settings.feedback
	if f.rulesLoading {
		return
	}
	f.rulesLoading, f.rulesFailed = true, false
	go func() {
		rules, err := s.deps.Feedback.Rules(context.Background())
		if err != nil {
			_ = infra.ReportError(s.deps.Log, err, "Feedback.Rules", infra.Diagnostic{
				Operation: "feedback.rules", Stage: "settings",
			})
		}
		fairy.Go(func() {
			f.rulesLoading = false
			if err != nil {
				f.rulesFailed = true
				return
			}
			f.rules = &rules
		})
	}()
}

func (s *Shell) feedbackSettings() *fairy.El {
	const prefix = "page.setting.feedback."
	t := kit.T()
	f := &s.settings.feedback
	if !f.started {
		f.started, f.category, f.includeLog = true, feedbackCategories[0], true
	}
	if f.rules == nil && !f.rulesFailed {
		s.loadFeedbackRules()
	}
	sending := f.progress != nil
	rules := f.rules
	problem := f.problem()

	categories := make([]kit.Item, 0, len(feedbackCategories))
	for _, category := range feedbackCategories {
		categories = append(categories, kit.Item{Value: category, Label: T(prefix + "category." + category)})
	}
	label := func(text string) *fairy.El { return fairy.Text(text).FontSize(14).LineHeight(20).Medium() }
	counter := func(text string, over bool) *fairy.El {
		color := t.MutedForeground
		if over {
			color = t.Destructive
		}
		// The React row aligned the counter to the label's baseline, which sits 2px above the label's line box.
		return fairy.Text(text).FontSize(12).LineHeight(16).Color(color).MarginEdges(0, 0, 2, 0)
	}

	length := bodyLength(f.body)
	bodyCount := catalog.Integer(int64(length))
	if rules != nil {
		bodyCount += " / " + catalog.Integer(int64(rules.MaxBodyLength))
	}
	bodyOver := rules != nil && length > rules.MaxBodyLength
	_, mediaBytes := f.media()
	mediaSize := formatSize(mediaBytes)
	if rules != nil {
		mediaSize += " / " + formatSize(rules.MaxMediaBytes)
	}

	attachments := fairy.Col(
		fairy.Row(label(T(prefix+"attachments.label")), fairy.Spacer(),
			counter(mediaSize, rules != nil && mediaBytes > rules.MaxMediaBytes)).Align(fairy.End),
		fairy.Text(T(prefix+"attachments.description")).FontSize(12).LineHeight(16).Color(t.MutedForeground),
	).Gap(8)
	if len(f.attachments) > 0 {
		list := fairy.Col().Gap(4).Role(fairy.RoleList)
		for _, item := range f.attachments {
			list.Child(s.feedbackAttachment(item, sending))
		}
		attachments.Child(list)
	}
	modNote := T(prefix + "attachments.mod_login_required")
	if s.signedIn() {
		modNote = T(prefix + "attachments.mod_description")
	}
	attachments.Child(
		fairy.Row(
			kit.Button(T(prefix+"attachments.add_files"), func() { s.addFeedbackPaths(s.feedbackFilePicker()) },
				kit.Outline, kit.Small, kit.Icon(lucide.Paperclip), kit.Disabled(sending || rules == nil)),
			kit.Button(T(prefix+"attachments.add_mod"), func() { s.addFeedbackPaths(s.pickFeedbackMod) },
				kit.Outline, kit.Small, kit.Icon(lucide.FolderPlus), kit.Disabled(sending || !s.signedIn())),
		).Wrap().Gap(8),
		fairy.Text(modNote).FontSize(12).LineHeight(16).Color(t.MutedForeground),
	)

	form := fairy.Col(
		fairy.Col(
			label(T(prefix+"category.label")),
			kit.Select("settings.feedback.category", f.category, categories, func(value string) { f.category = value },
				// The label is an inline span, so space-y-2 only separates the trigger from Base UI's hidden input.
				kit.Disabled(sending), kit.Label(T(prefix+"category.label"))).MarginEdges(0, 0, 8, 0),
		),
		fairy.Col(
			fairy.Row(label(T(prefix+"body.label")), fairy.Spacer(), counter(bodyCount, bodyOver)).Align(fairy.End),
			kit.TextArea(f.body, func(value string) { f.body = value }, kit.Placeholder(T(prefix+"body.placeholder")),
				kit.Disabled(sending), kit.Invalid(bodyOver), kit.Label(T(prefix+"body.label"))).MinH(160).MaxH(384).
				ScrollY(),
		).Gap(8),
		attachments,
		fairy.Row(
			kit.Checkbox(f.includeLog, func(on bool) { f.includeLog = on }, kit.Disabled(sending)).
				Label(T(prefix+"log.label")).MarginEdges(2, 0, 0, 0),
			fairy.Col(
				fairy.Text(T(prefix+"log.label")).FontSize(14).LineHeight(20).Medium(),
				fairy.Text(T(prefix+"log.description")).FontSize(12).LineHeight(16).Color(t.MutedForeground),
			).Gap(2).Grow(1).MinW(0).Disabled(sending).Cursor(fairy.CursorPointer).
				OnClick(func() { f.includeLog = !f.includeLog }),
		).Gap(10).Align(fairy.Start),
	).Gap(20).PadXY(0, 4)
	if f.progress != nil {
		form.Child(kit.Progress(*f.progress / 100))
	}
	if problem != "" {
		form.Child(fairy.Text(problem).FontSize(12).LineHeight(16).Color(t.Destructive))
		if f.rulesFailed {
			form.Child(kit.Button(T("page.setting.acc.retry"), s.loadFeedbackRules, kit.Outline, kit.Small).
				AlignSelf(fairy.Start))
		}
	}

	submitLabel := T(prefix + "submit")
	if sending {
		submitLabel = T(prefix + "sending")
	}
	actions := fairy.Row().Gap(8).Justify(fairy.JustifyEnd)
	if sending {
		actions.Child(kit.Button(T(prefix+"cancel"), s.cancelFeedback, kit.Outline))
	}
	actions.Child(kit.Button(submitLabel, s.submitFeedback, kit.Loading(sending),
		kit.Disabled(sending || rules == nil || strings.TrimSpace(f.body) == "" || problem != "")))
	form.Child(actions)

	header := fairy.Col(
		fairy.Text(T(prefix+"title")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
		fairy.Text(T(prefix+"description")).FontSize(12).LineHeight(16).Color(t.MutedForeground),
	).Gap(2).PadEdges(0, 4, 8, 4)
	return settingsBody(fairy.Col(header, settingsGroup("", form)).Gap(4).FontSize(14))
}

func (s *Shell) feedbackAttachment(item feedback.Attachment, sending bool) *fairy.El {
	const prefix = "page.setting.feedback.attachments."
	t := kit.T()
	f := &s.settings.feedback
	size := formatSize(item.Size)
	if item.Kind == feedback.KindMod {
		size = T(prefix+"file_count", "count", item.Files) + " · " + size
	}
	path := item.Path
	return fairy.Row(
		fairy.Icon(feedbackKindIcon(item.Kind)).Size(16, 16).Color(t.MutedForeground).Shrink(0),
		fairy.Text(item.Name).FontSize(14).LineHeight(20).Grow(1).MinW(0).Ellipsis().Tooltip(item.Path),
		fairy.Text(size).FontSize(12).LineHeight(16).Color(t.MutedForeground).Shrink(0),
		kit.IconButton(lucide.X, func() {
			f.attachments = slices.DeleteFunc(f.attachments, func(a feedback.Attachment) bool { return a.Path == path })
		}, kit.Ghost, kit.XS, kit.Disabled(sending), kit.Label(T(prefix+"remove"))),
	).Key("feedback.attachment."+item.Path).Gap(8).Align(fairy.Center).PadXY(10, 6).Radius(6).
		Fill(t.Muted.Alpha(0.5))
}

// feedbackKindIcon maps the server's media kinds; a kind it adds later gets the generic icon.
func feedbackKindIcon(kind feedback.Kind) *svg.Vector {
	switch kind {
	case feedback.KindImage:
		return lucide.Image
	case feedback.KindVideo:
		return lucide.Film
	case feedback.KindText:
		return lucide.FileText
	case feedback.KindMod:
		return lucide.Folder
	case "archive":
		return lucide.FileArchive
	case feedback.KindUnsupported:
	}
	return lucide.Paperclip
}

// bodyLength counts UTF-16 code units, as the server bounds the report body.
func bodyLength(body string) int { return len(utf16.Encode([]rune(body))) }

// feedbackFilePicker returns a picker for the media files the server accepts. It reads the rules on the UI
// thread; the picker itself blocks and runs in the background.
func (s *Shell) feedbackFilePicker() func() ([]string, error) {
	var extensions []string
	if rules := s.settings.feedback.rules; rules != nil {
		for _, item := range rules.MediaExtensions {
			extensions = append(extensions, strings.TrimPrefix(item.Ext, "."))
		}
	}
	options := platform.OpenDialogOptions{
		Title: T("page.setting.feedback.attachments.add_files"),
		Filters: []platform.FileFilter{
			{Name: T("page.setting.feedback.attachments.filter"), Extensions: extensions},
		},
		Properties: []string{"openFile", "multiSelections"},
		Role:       "feedback-attachment",
	}
	return func() ([]string, error) {
		result, err := s.deps.Dialog.ShowOpenDialog(options)
		if err != nil || result.Canceled {
			return nil, err
		}
		return result.FilePaths, nil
	}
}

func (s *Shell) pickFeedbackMod() ([]string, error) {
	result, err := s.deps.Dialog.SelectDirectory("feedback-mod")
	if err != nil || result.Canceled || result.FilePath == "" {
		return nil, err
	}
	return []string{result.FilePath}, nil
}

// addFeedbackPaths attaches what pick returns, leaving out unsupported and empty files with a toast each.
func (s *Shell) addFeedbackPaths(pick func() ([]string, error)) {
	const prefix = "page.setting.feedback.error."
	go func() {
		paths, err := pick()
		var described []feedback.Attachment
		if err == nil && len(paths) > 0 {
			described, err = s.deps.Feedback.Describe(context.Background(), paths)
		}
		if err != nil {
			err = infra.ReportError(s.deps.Log, err, "Feedback.Describe", infra.Diagnostic{
				Operation: "feedback.attach", Fields: map[string]any{"paths": paths},
			})
			s.toasts.Show(kit.ToastError, err.Error(), "")
			return
		}
		var unsupported, empty []string
		var accepted []feedback.Attachment
		for _, item := range described {
			switch {
			case item.Kind == feedback.KindUnsupported:
				unsupported = append(unsupported, item.Name)
			case item.Kind != feedback.KindMod && item.Size == 0:
				// An empty file has no bytes to sign an upload for.
				empty = append(empty, item.Name)
			default:
				accepted = append(accepted, item)
			}
		}
		if len(unsupported) > 0 {
			s.toasts.Show(
				kit.ToastError,
				T(prefix+"FEEDBACK_UNSUPPORTED_FILE", "name", strings.Join(unsupported, ", ")),
				"",
			)
		}
		if len(empty) > 0 {
			s.toasts.Show(kit.ToastError, T(prefix+"empty_file", "name", strings.Join(empty, ", ")), "")
		}
		fairy.Go(func() {
			f := &s.settings.feedback
			for _, item := range accepted {
				if !slices.ContainsFunc(
					f.attachments,
					func(a feedback.Attachment) bool { return a.Path == item.Path },
				) {
					f.attachments = append(f.attachments, item)
				}
			}
		})
	}()
}

func (s *Shell) submitFeedback() {
	const prefix = "page.setting.feedback."
	f := &s.settings.feedback
	if f.progress != nil || f.rules == nil {
		return
	}
	rules := *f.rules
	request := feedback.Request{
		Category:   f.category,
		Body:       f.body,
		IncludeLog: f.includeLog,
		Files:      []string{},
		ModDirs:    []feedback.ModDir{},
		Locale:     catalog.Language(),
	}
	for _, item := range f.attachments {
		if item.Kind == feedback.KindMod {
			request.ModDirs = append(request.ModDirs, feedback.ModDir{Path: item.Path})
		} else {
			request.Files = append(request.Files, item.Path)
		}
	}
	zero := 0.0
	f.progress, f.cancelled = &zero, false
	go func() {
		result, err := s.deps.Feedback.Submit(context.Background(), request)
		fairy.Go(func() {
			f.progress = nil
			if err == nil {
				s.feedbackSent(result)
				f.body, f.attachments = "", nil
				return
			}
			if !f.cancelled {
				s.toasts.Show(kit.ToastError, T(prefix+"failed"), feedbackFailure(err, rules))
			}
		})
	}()
}

func (s *Shell) feedbackSent(result feedback.Result) {
	const prefix = "page.setting.feedback."
	body := ""
	if result.SkippedModFiles > 0 {
		body = T(prefix+"skipped_mod_files", "count", result.SkippedModFiles)
	}
	s.toasts.Show(kit.ToastSuccess, T(prefix+"sent"), body)
}

// feedbackFailure explains a submit error the screen knows, or returns its text.
func feedbackFailure(err error, rules feedback.Rules) string {
	message := err.Error()
	for _, code := range feedbackErrorCodes {
		if detail, ok := strings.CutPrefix(message, code+":"); ok {
			count := rules.MaxModFiles
			if code == "FEEDBACK_TOO_MANY_MEDIA" {
				count = rules.MaxMediaFiles
			}
			return T("page.setting.feedback.error."+code, "name", detail, "count", count,
				"size", formatSize(rules.MaxMediaBytes))
		}
	}
	return message
}

func (s *Shell) cancelFeedback() {
	s.settings.feedback.cancelled = true
	go s.deps.Feedback.Cancel()
}
