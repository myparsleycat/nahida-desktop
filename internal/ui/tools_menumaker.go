package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/tools/menumaker"
)

// Renderer state keys of the React frontend, kept so that its stored settings and drafts carry over.
const (
	menuMakerSettingsKey   = "nahida.menu-maker.settings"
	menuMakerDraftsKey     = "nahida.menu-maker.drafts"
	menuMakerFavoritesKey  = "nahida.menu-maker.iconify.favorites"
	menuMakerDraftMediaKey = "nahida.menu-maker.draft-media."
	menuMakerIconifyKey    = "nahida.menu-maker.iconify-cache."
	menuMakerDraftLimit    = 30
)

var menuMakerSourceExtension = regexp.MustCompile(`(?i)\.(?:ini|txt)$`)

// menuMakerIcon is a slot's icon: a Lucide name, an Iconify SVG or an uploaded PNG data URL.
type menuMakerIcon struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	SVG     string `json:"svg,omitempty"`
	DataURL string `json:"dataUrl,omitempty"`
}

type menuMakerSlot struct {
	menumaker.MenuMakerSlot
	Icon menuMakerIcon `json:"icon"`

	// image is the decoded upload icon, filled in when the slot is first drawn.
	image *fairy.Image
}

type menuMakerSettings struct {
	menumaker.MenuMakerSettings
	PanelImageDataURL string `json:"panelImageDataUrl,omitempty"`

	panelImage *fairy.Image
}

type menuMakerDraft struct {
	ID             string            `json:"id"`
	SourcePath     string            `json:"sourcePath"`
	SourceName     string            `json:"sourceName"`
	SourceSHA256   string            `json:"sourceSHA256"`
	SlotSignature  string            `json:"slotSignature"`
	UpdatedAt      int64             `json:"updatedAt"`
	SourceEncoding string            `json:"sourceEncoding,omitempty"`
	SourceHasBOM   bool              `json:"sourceHasBOM,omitempty"`
	SourceNewline  string            `json:"sourceNewline,omitempty"`
	Settings       menuMakerSettings `json:"settings"`
	Slots          []menuMakerSlot   `json:"slots"`
}

// menuMakerDraftMedia is the part of a draft too large for the draft list: the source text and the images.
type menuMakerDraftMedia struct {
	OriginalText      string            `json:"originalText"`
	PanelImageDataURL string            `json:"panelImageDataUrl,omitempty"`
	SlotImages        map[string]string `json:"slotImages,omitempty"`
}

type menuMakerTool struct {
	loaded    bool
	loading   bool
	afterLoad []func()

	// modPath and modName are the mod the tool was opened for from the mod screen.
	modPath, modName string
	source           *menumaker.MenuMakerSource
	document         *menumaker.MenuMakerDocument
	slots            []menuMakerSlot
	settings         menuMakerSettings
	scan             *menumaker.MenuMakerScanResult
	sourceAvailable  bool
	busy             bool
	revision         int

	preview  *menumaker.MenuMakerGenerateResult
	genError string
	genSeq   int

	drafts    []menuMakerDraft
	favorites []string
	// iconify caches Iconify SVGs by collection prefix and icon name; nil until it is read.
	iconify map[string]map[string]string

	selected    []string
	mergeMode   string
	sidePanel   string
	narrowPanel string
	width       float32
	numbers     map[string]string
	drag        menuMakerDrag

	persistTimer  *time.Timer
	settingsDirty bool

	iconPicker  *menuMakerIconPicker
	crop        *menuMakerCrop
	draftsOpen  bool
	compareOpen bool
}

type menuMakerDrag struct {
	active       bool
	index, from  int
	columns      int
	stepX, stepY float32
}

func defaultMenuMakerSettings() menuMakerSettings {
	return menuMakerSettings{MenuMakerSettings: menumaker.MenuMakerSettings{
		MenuKey: "alt", ClickModifier: "alt", Columns: 3, Gap: 14, BaseWidth: 1920, BaseHeight: 1080,
		PanelScale: 1, SlotAlignment: "center", FallbackType: "cycle", ShowKeyHint: true, HideUploadLabel: true,
		Palette: menumaker.MenuMakerPalette{
			Accent: "#ff4fb3", PanelBackground: "#11131a", PanelBackgroundAlpha: 210, PanelBorder: "#ff4fb3",
			PanelBorderAlpha: 255, SlotBackground: "#1c2030", SlotBackgroundAlpha: 200, SlotHover: "#5d2850",
			SlotHoverAlpha: 230, SlotBorder: "#4a5068", SlotBorderAlpha: 255, Title: "#ffffff",
			TitleShadow: "#000000",
		},
	}}
}

func boundMenuMakerSlots(slots []menuMakerSlot) []menumaker.MenuMakerSlot {
	out := make([]menumaker.MenuMakerSlot, len(slots))
	for i, slot := range slots {
		out[i] = slot.MenuMakerSlot
	}
	return out
}

// withSuggestedIcons gives each parsed slot the icon its handlers suggest.
func withSuggestedIcons(slots []menumaker.MenuMakerSlot) []menuMakerSlot {
	out := make([]menuMakerSlot, len(slots))
	for i, slot := range slots {
		out[i] = menuMakerSlot{MenuMakerSlot: slot, Icon: suggestMenuMakerIcon(slot)}
	}
	return out
}

// openMenuMaker opens the tool on a mod folder and scans it for INI files, as the mod screen's menu did.
func (s *Shell) openMenuMaker(modPath, modName string) {
	s.navigate(RouteTools)
	s.openTool(toolMenuMaker)
	m := &s.tools.menuMaker
	m.modPath, m.modName = modPath, modName
	s.menuMakerReady(func() { s.menuMakerScan(modPath, false) })
}

// menuMakerReady runs then once the stored settings, drafts and favorites are read.
func (s *Shell) menuMakerReady(then func()) {
	m := &s.tools.menuMaker
	if m.loaded {
		if then != nil {
			then()
		}
		return
	}
	if then != nil {
		m.afterLoad = append(m.afterLoad, then)
	}
	if m.loading {
		return
	}
	m.loading = true
	m.settings = defaultMenuMakerSettings()
	m.mergeMode, m.sidePanel, m.narrowPanel = "guiOnly", "editor", "preview"
	m.numbers = map[string]string{}
	s.background(func(ctx context.Context) {
		settings, err := s.deps.Settings.RendererStateWithPrefix(ctx, menuMakerSettingsKey)
		var drafts, favorites map[string]string
		if err == nil {
			drafts, err = s.deps.Settings.RendererStateWithPrefix(ctx, menuMakerDraftsKey)
		}
		if err == nil {
			favorites, err = s.deps.Settings.RendererStateWithPrefix(ctx, menuMakerFavoritesKey)
		}
		if err != nil {
			_ = infra.ReportError(s.deps.Log, err, "MenuMaker", infra.Diagnostic{Operation: "menu-maker.load-state"})
		}
		fairy.Go(func() {
			m.loading, m.loaded = false, true
			if raw, ok := settings[menuMakerSettingsKey]; ok {
				stored := defaultMenuMakerSettings()
				if json.Unmarshal([]byte(raw), &stored) == nil {
					if stored.Columns == 0 {
						stored.Columns = 3
					}
					stored.PanelImageDataURL = ""
					m.settings = stored
				}
			}
			m.drafts = parseMenuMakerDrafts(drafts[menuMakerDraftsKey])
			_ = json.Unmarshal([]byte(favorites[menuMakerFavoritesKey]), &m.favorites)
			pending := m.afterLoad
			m.afterLoad = nil
			for _, then := range pending {
				then()
			}
		})
	})
}

func parseMenuMakerDrafts(raw string) []menuMakerDraft {
	var drafts []menuMakerDraft
	if raw == "" || json.Unmarshal([]byte(raw), &drafts) != nil {
		return nil
	}
	return slices.DeleteFunc(drafts, func(d menuMakerDraft) bool {
		return d.ID == "" || d.SourceSHA256 == "" || d.SlotSignature == "" || d.Slots == nil
	})
}

func (s *Shell) setRendererState(key string, value *string) {
	s.settings.writes <- func() {
		if err := s.deps.Settings.SetRendererState(context.Background(), key, value); err != nil {
			s.reportRendererState(err, key)
		}
	}
}

func (s *Shell) menuMakerFailed(err error, operation, message string, fields map[string]any) {
	if infra.IsCancellationError(err) {
		return
	}
	_ = infra.ReportError(s.deps.Log, err, "MenuMaker", infra.Diagnostic{Operation: operation, Fields: fields})
	s.toasts.Show(kit.ToastError, message, "")
}

func (s *Shell) menuMakerScan(root string, includeTXT bool) {
	m := &s.tools.menuMaker
	m.busy = true
	go func() {
		result, err := s.deps.Tools.MenuMakerScanFolder(context.Background(), root, includeTXT)
		fairy.Go(func() {
			m.busy = false
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.scan", T("page.tools.menu_maker.scan_failed"),
					map[string]any{"root": root, "includeTXT": includeTXT})
				return
			}
			m.scan = &result
			if len(result.Files) == 1 {
				s.menuMakerLoadSource(result.Files[0].Path)
			}
		})
	}()
}

func (s *Shell) menuMakerLoadSource(filePath string) {
	m := &s.tools.menuMaker
	m.busy = true
	go func() {
		source, err := s.deps.Tools.MenuMakerLoadSource(context.Background(), filePath)
		var draft *menuMakerDraft
		var media *menuMakerDraftMedia
		if err == nil {
			draft, media = s.menuMakerMatchingDraft(source)
		}
		fairy.Go(func() {
			m.busy = false
			if err != nil {
				message := T("page.tools.menu_maker.load_failed")
				if errors.Is(err, menumaker.ErrNoKeySections) {
					message = T("page.tools.menu_maker.no_keys")
				}
				s.menuMakerFailed(err, "menu-maker.load-source", message, map[string]any{"path": filePath})
				return
			}
			slots, settings := withSuggestedIcons(source.Document.Slots), m.settings
			if draft != nil {
				slots, settings = restoreMenuMakerDraftMedia(*draft, media)
			}
			s.menuMakerLoad(source, source.Document, slots, settings, true)
			if draft != nil {
				s.toasts.Show(kit.ToastSuccess, T("page.tools.menu_maker.draft_restored"), "")
			}
		})
	}()
}

// menuMakerMatchingDraft finds the draft saved for this source text and its handlers. It runs off the UI thread
// and reads the draft list the UI thread last stored.
func (s *Shell) menuMakerMatchingDraft(source menumaker.MenuMakerSource) (*menuMakerDraft, *menuMakerDraftMedia) {
	var drafts []menuMakerDraft
	fairy.Do(func() { drafts = slices.Clone(s.tools.menuMaker.drafts) })
	signature := menumaker.SlotSignature(source.Document.Slots)
	i := slices.IndexFunc(drafts, func(d menuMakerDraft) bool {
		return d.SourceSHA256 == source.SHA256 && d.SlotSignature == signature
	})
	if i < 0 {
		return nil, nil
	}
	media, err := s.loadMenuMakerDraftMedia(context.Background(), drafts[i].ID)
	if err != nil {
		_ = infra.ReportError(s.deps.Log, err, "MenuMaker", infra.Diagnostic{
			Operation: "menu-maker.load-draft-media", Fields: map[string]any{"draftId": drafts[i].ID},
		})
	}
	return &drafts[i], media
}

func (s *Shell) menuMakerLoad(
	source menumaker.MenuMakerSource,
	document menumaker.MenuMakerDocument,
	slots []menuMakerSlot,
	settings menuMakerSettings,
	sourceAvailable bool,
) {
	m := &s.tools.menuMaker
	m.source, m.document = &source, &document
	m.slots, m.settings, m.sourceAvailable = slots, settings, sourceAvailable
	m.selected = nil
	m.settingsDirty = true
	s.menuMakerChanged()
}

// menuMakerChanged regenerates the preview and saves the draft after an edit.
func (s *Shell) menuMakerChanged() {
	m := &s.tools.menuMaker
	m.revision++
	s.menuMakerGenerate()
	if m.persistTimer != nil {
		m.persistTimer.Stop()
	}
	m.persistTimer = time.AfterFunc(500*time.Millisecond, func() { fairy.Go(s.menuMakerPersist) })
}

func (s *Shell) menuMakerSetSettings(update func(*menumaker.MenuMakerSettings)) {
	m := &s.tools.menuMaker
	update(&m.settings.MenuMakerSettings)
	m.settingsDirty = true
	s.menuMakerChanged()
}

func (s *Shell) menuMakerSetSlots(slots []menuMakerSlot) {
	s.tools.menuMaker.slots = slots
	s.menuMakerChanged()
}

func (s *Shell) menuMakerUpdateSlot(id string, update func(*menuMakerSlot)) {
	m := &s.tools.menuMaker
	slots := slices.Clone(m.slots)
	for i := range slots {
		if slots[i].ID == id {
			update(&slots[i])
		}
	}
	s.menuMakerSetSlots(slots)
}

func (m *menuMakerTool) generateRequest() menumaker.MenuMakerGenerateRequest {
	return menumaker.MenuMakerGenerateRequest{
		SourcePath: m.source.Path, SourceText: m.source.Text,
		Slots: boundMenuMakerSlots(m.slots), Settings: m.settings.MenuMakerSettings,
	}
}

// menuMakerGenerate builds the INI preview in the background; only the latest request is shown.
func (s *Shell) menuMakerGenerate() {
	m := &s.tools.menuMaker
	if m.source == nil || m.source.Text == "" {
		return
	}
	m.genSeq++
	seq, req := m.genSeq, m.generateRequest()
	go func() {
		result, err := s.deps.Tools.MenuMakerGenerate(context.Background(), req)
		fairy.Go(func() {
			if seq != m.genSeq {
				return
			}
			if err != nil {
				m.preview = nil
				m.genError = menuMakerGenerateError(err)
				return
			}
			m.preview, m.genError = &result, ""
		})
	}()
}

func menuMakerGenerateError(err error) string {
	if errors.Is(err, menumaker.ErrNoVisibility) {
		return T("page.tools.menu_maker.no_visibility")
	}
	return T("page.tools.menu_maker.generate_failed")
}

// menuMakerPersist saves the settings and the draft of the open source.
func (s *Shell) menuMakerPersist() {
	m := &s.tools.menuMaker
	m.persistTimer = nil
	if m.settingsDirty {
		m.settingsDirty = false
		stored := m.settings
		stored.PanelImageDataURL = ""
		if data, err := json.Marshal(stored); err == nil {
			value := string(data)
			s.setRendererState(menuMakerSettingsKey, &value)
		}
	}
	if m.source == nil || m.document == nil {
		return
	}

	source := *m.source
	signature := menumaker.SlotSignature(m.document.Slots)
	id := source.SHA256 + ":" + signature
	media := menuMakerDraftMedia{
		OriginalText: source.Text, PanelImageDataURL: m.settings.PanelImageDataURL, SlotImages: map[string]string{},
	}
	settings := m.settings
	settings.PanelImageDataURL, settings.panelImage = "", nil
	slots := slices.Clone(m.slots)
	for i := range slots {
		slots[i].image = nil
		if slots[i].Icon.Kind == "upload" {
			media.SlotImages[slots[i].ID] = slots[i].Icon.DataURL
			slots[i].Icon.DataURL = ""
		}
	}
	draft := menuMakerDraft{
		ID: id, SourcePath: source.Path, SourceName: source.FileName, SourceSHA256: source.SHA256,
		SlotSignature: signature, UpdatedAt: time.Now().UnixMilli(), SourceEncoding: source.Encoding,
		SourceHasBOM: source.HasBOM, SourceNewline: source.Newline, Settings: settings, Slots: slots,
	}
	drafts := []menuMakerDraft{draft}
	for _, d := range m.drafts {
		superseded := platform.SamePathFold(d.SourcePath, source.Path) && d.SlotSignature == signature
		if d.ID != id && !superseded {
			drafts = append(drafts, d)
		}
	}
	s.saveMenuMakerDrafts(drafts)
	if data, err := json.Marshal(media); err == nil {
		value := string(data)
		s.setRendererState(menuMakerDraftMediaKey+menuMakerDraftKey(id), &value)
	}
}

// saveMenuMakerDrafts stores the newest drafts up to the limit and deletes the media of the others.
func (s *Shell) saveMenuMakerDrafts(drafts []menuMakerDraft) {
	m := &s.tools.menuMaker
	slices.SortStableFunc(drafts, func(a, b menuMakerDraft) int { return int(b.UpdatedAt - a.UpdatedAt) })
	kept := drafts[:min(len(drafts), menuMakerDraftLimit)]
	for _, old := range m.drafts {
		if !slices.ContainsFunc(kept, func(d menuMakerDraft) bool { return d.ID == old.ID }) {
			s.setRendererState(menuMakerDraftMediaKey+menuMakerDraftKey(old.ID), nil)
		}
	}
	m.drafts = slices.Clone(kept)
	data, err := json.Marshal(m.drafts)
	if err != nil {
		return
	}
	value := string(data)
	s.setRendererState(menuMakerDraftsKey, &value)
}

// menuMakerDraftKey shortens a draft ID, which holds the slot signature JSON, into a renderer state key.
func menuMakerDraftKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:16])
}

func (s *Shell) loadMenuMakerDraftMedia(ctx context.Context, id string) (*menuMakerDraftMedia, error) {
	key := menuMakerDraftMediaKey + menuMakerDraftKey(id)
	values, err := s.deps.Settings.RendererStateWithPrefix(ctx, key)
	if err != nil {
		return nil, err
	}
	raw, ok := values[key]
	if !ok {
		return s.legacyMenuMakerDraftMedia(id, key)
	}
	var media menuMakerDraftMedia
	if err := json.Unmarshal([]byte(raw), &media); err != nil {
		return nil, err
	}
	return &media, nil
}

// legacyMenuMakerDraftMedia reads a draft's media from where the React menu maker stored it, WebView2's
// IndexedDB, and keeps a copy in the renderer state the native tool reads.
func (s *Shell) legacyMenuMakerDraftMedia(id, key string) (*menuMakerDraftMedia, error) {
	legacy, err := s.deps.Tools.MenuMakerLegacyDraftMedia(id)
	if err != nil || legacy == nil {
		return nil, err
	}
	media := menuMakerDraftMedia{OriginalText: legacy.OriginalText, PanelImageDataURL: legacy.PanelImageDataURL}
	if legacy.SlotImages != "" {
		_ = json.Unmarshal([]byte(legacy.SlotImages), &media.SlotImages)
	}
	if data, err := json.Marshal(media); err == nil {
		value := string(data)
		s.setRendererState(key, &value)
	}
	return &media, nil
}

func restoreMenuMakerDraftMedia(draft menuMakerDraft, media *menuMakerDraftMedia) ([]menuMakerSlot, menuMakerSettings) {
	settings := draft.Settings
	slots := slices.Clone(draft.Slots)
	if media == nil {
		return slots, settings
	}
	if media.PanelImageDataURL != "" {
		settings.PanelImageDataURL = media.PanelImageDataURL
	}
	for i := range slots {
		if image, ok := media.SlotImages[slots[i].ID]; ok && slots[i].Icon.Kind == "upload" && image != "" {
			slots[i].Icon.DataURL = image
		}
	}
	return slots, settings
}

// menuMakerRestoreDraft loads a draft from the drafts dialog. A draft of another source opens its saved text,
// which cannot be applied to the mod because the file on disk may differ.
func (s *Shell) menuMakerRestoreDraft(draft menuMakerDraft) {
	m := &s.tools.menuMaker
	current := m.source
	var currentDocument *menumaker.MenuMakerDocument
	matchesCurrent := false
	if current != nil && m.document != nil {
		currentDocument = m.document
		matchesCurrent = draft.SourceSHA256 == current.SHA256 &&
			draft.SlotSignature == menumaker.SlotSignature(m.document.Slots)
	}
	available := m.sourceAvailable
	go func() {
		ctx := context.Background()
		media, err := s.loadMenuMakerDraftMedia(ctx, draft.ID)
		var source menumaker.MenuMakerSource
		var document menumaker.MenuMakerDocument
		if err == nil {
			switch {
			case matchesCurrent:
				source, document = *current, *currentDocument
			case media == nil || media.OriginalText == "":
				err = errors.New("MENU_MAKER_DRAFT_TEXT_MISSING")
			default:
				document, err = s.deps.Tools.MenuMakerParse(ctx, media.OriginalText)
				source = menumaker.MenuMakerSource{
					Path: draft.SourcePath, FileName: draft.SourceName, Text: media.OriginalText,
					SHA256: draft.SourceSHA256, Encoding: orDefault(draft.SourceEncoding, "utf8"),
					HasBOM: draft.SourceHasBOM, Newline: orDefault(draft.SourceNewline, "lf"), Document: document,
				}
			}
		}
		if err == nil && menumaker.SlotSignature(document.Slots) != draft.SlotSignature {
			err = errors.New("MENU_MAKER_DRAFT_SIGNATURE_CHANGED")
		}
		fairy.Go(func() {
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.restore-draft", T("page.tools.menu_maker.draft_restore_failed"),
					map[string]any{"draftId": draft.ID})
				return
			}
			slots, settings := restoreMenuMakerDraftMedia(draft, media)
			s.menuMakerLoad(source, document, slots, settings, matchesCurrent && available)
			m.draftsOpen = false
			s.toasts.Show(kit.ToastSuccess, T("page.tools.menu_maker.draft_restored"), "")
		})
	}()
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (s *Shell) menuMakerChooseFile() {
	m := &s.tools.menuMaker
	defaultPath := m.modPath
	if m.source != nil {
		defaultPath = m.source.Path
	}
	go func() {
		result, err := s.deps.Dialog.ShowOpenDialog(platform.OpenDialogOptions{
			Title: T("page.tools.menu_maker.choose_file"), DefaultPath: defaultPath,
			Filters:    []platform.FileFilter{{Name: "3DMigoto INI", Extensions: []string{"ini", "txt"}}},
			Properties: []string{"openFile"}, Role: "menu-maker-source",
		})
		fairy.Go(func() {
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.choose-file", T("page.tools.menu_maker.load_failed"), nil)
				return
			}
			if !result.Canceled && len(result.FilePaths) > 0 {
				s.menuMakerLoadSource(result.FilePaths[0])
			}
		})
	}()
}

func (s *Shell) menuMakerChooseFolder() {
	go func() {
		result, err := s.deps.Dialog.SelectDirectory("menu-maker-source")
		fairy.Go(func() {
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.choose-folder", T("page.tools.menu_maker.scan_failed"), nil)
				return
			}
			if !result.Canceled && result.FilePath != "" {
				s.menuMakerScan(result.FilePath, false)
			}
		})
	}()
}

func (m *menuMakerTool) outputName() string {
	if m.source == nil {
		return "mod.ini"
	}
	return menuMakerSourceExtension.ReplaceAllString(m.source.FileName, ".ini")
}

// menuMakerExport generates the INI and its textures from the current state off the UI thread.
func (s *Shell) menuMakerExport(
	req menumaker.MenuMakerGenerateRequest,
	slots []menuMakerSlot,
	settings menuMakerSettings,
) (menumaker.MenuMakerGenerateResult, []menumaker.MenuMakerGeneratedAsset, error) {
	generated, err := s.deps.Tools.MenuMakerGenerate(context.Background(), req)
	if err != nil {
		return generated, nil, err
	}
	fairy.Go(func() { s.tools.menuMaker.preview = &generated })
	assets, err := renderMenuMakerAssets(slots, settings, generated.Geometry, generated.SlotStates)
	return generated, assets, err
}

func (s *Shell) menuMakerExportFailed(err error, operation, fallback string, fields map[string]any) {
	message := fallback
	switch {
	case errors.Is(err, errMenuMakerFont):
		message = T("page.tools.menu_maker.font_failed")
	case errors.Is(err, menumaker.ErrSourceChanged):
		message = T("page.tools.menu_maker.source_changed")
	}
	s.menuMakerFailed(err, operation, message, fields)
}

func (s *Shell) menuMakerApply() {
	m := &s.tools.menuMaker
	if m.source == nil || !m.sourceAvailable || m.busy {
		return
	}
	m.busy = true
	source, req, slots, settings := *m.source, m.generateRequest(), slices.Clone(m.slots), m.settings
	s.background(func(ctx context.Context) {
		generated, assets, err := s.menuMakerExport(req, slots, settings)
		var result menumaker.MenuMakerWriteResult
		if err == nil {
			result, err = s.deps.Tools.MenuMakerApplyBundle(ctx, menumaker.MenuMakerApplyRequest{
				SourcePath: source.Path, SourceSHA256: source.SHA256, Slots: req.Slots, Settings: req.Settings,
				Encoding: source.Encoding, HasBOM: source.HasBOM, Newline: source.Newline, Assets: assets,
			})
		}
		var document *menumaker.MenuMakerDocument
		if err == nil && result.OutputINIPath != "" && result.SourceSHA256 != "" {
			if parsed, parseErr := s.deps.Tools.MenuMakerParse(ctx, generated.INIText); parseErr == nil {
				document = &parsed
			}
		}
		fairy.Go(func() {
			m.busy = false
			if err != nil {
				s.menuMakerExportFailed(err, "menu-maker.apply", T("page.tools.menu_maker.apply_failed"),
					map[string]any{"sourcePath": source.Path})
				return
			}
			s.toasts.Show(kit.ToastSuccess, T("page.tools.menu_maker.applied", "path", result.OutputINIPath), "")
			if document == nil || m.source == nil || m.source.Path != source.Path {
				return
			}
			updated := *m.source
			updated.Path, updated.FileName = result.OutputINIPath, filepath.Base(result.OutputINIPath)
			updated.Text, updated.SHA256, updated.Document = generated.INIText, result.SourceSHA256, *document
			m.source, m.document = &updated, document
		})
	})
}

func (s *Shell) menuMakerSave(zip bool) {
	m := &s.tools.menuMaker
	if m.source == nil || m.busy {
		return
	}
	source, req, slots, settings := *m.source, m.generateRequest(), slices.Clone(m.slots), m.settings
	name, filter := m.outputName(), platform.FileFilter{Name: "INI", Extensions: []string{"ini"}}
	if zip {
		name = menuMakerSourceExtension.ReplaceAllString(name, ".zip")
		filter = platform.FileFilter{Name: "ZIP", Extensions: []string{"zip"}}
	}
	go func() {
		selection, err := s.deps.Dialog.SaveFile(platform.SaveFileOptions{
			SuggestedName: name, Filters: []platform.FileFilter{filter}, Role: "menu-maker-output",
		})
		if err == nil && (selection.Canceled || selection.FilePath == "") {
			return
		}
		fairy.Do(func() { m.busy = true })
		if err == nil && zip {
			var assets []menumaker.MenuMakerGeneratedAsset
			if _, assets, err = s.menuMakerExport(req, slots, settings); err == nil {
				_, err = s.deps.Tools.MenuMakerSaveZIP(context.Background(), menumaker.MenuMakerSaveZIPRequest{
					DestinationPath: selection.FilePath, SourcePath: source.Path, SourceText: source.Text,
					Slots: req.Slots, Settings: req.Settings, Encoding: source.Encoding, HasBOM: source.HasBOM,
					Newline: source.Newline, Assets: assets,
				})
			}
		} else if err == nil {
			_, err = s.deps.Tools.MenuMakerSaveINI(context.Background(), menumaker.MenuMakerSaveINIRequest{
				DestinationPath: selection.FilePath, SourcePath: source.Path, SourceText: source.Text,
				Slots: req.Slots, Settings: req.Settings, Encoding: source.Encoding, HasBOM: source.HasBOM,
				Newline: source.Newline,
			})
		}
		fairy.Go(func() {
			m.busy = false
			if err != nil {
				s.menuMakerExportFailed(err, "menu-maker.save", T("page.tools.menu_maker.save_failed"),
					map[string]any{"destinationPath": selection.FilePath, "zip": zip})
				return
			}
			s.toasts.Show(kit.ToastSuccess, T("page.tools.menu_maker.saved"), "")
		})
	}()
}

// menuMakerMerge merges the selected slots; the merged slot keeps the icon of the first of them.
func (s *Shell) menuMakerMerge() {
	m := &s.tools.menuMaker
	icons := map[string]menuMakerSlot{}
	var primary *menuMakerSlot
	for i, slot := range m.slots {
		icons[slot.ID] = slot
		if primary == nil && slices.Contains(m.selected, slot.ID) {
			primary = &m.slots[i]
		}
	}
	merged := menumaker.MergeSlots(boundMenuMakerSlots(m.slots), m.selected, m.mergeMode)
	slots := make([]menuMakerSlot, len(merged))
	for i, slot := range merged {
		previous, ok := icons[slot.ID]
		if !ok && primary != nil {
			previous = *primary
		}
		slots[i] = menuMakerSlot{MenuMakerSlot: slot, Icon: previous.Icon, image: previous.image}
	}
	m.selected = nil
	s.menuMakerSetSlots(slots)
}

func (s *Shell) menuMakerMapSelected(update func(*menuMakerSlot)) {
	m := &s.tools.menuMaker
	slots := slices.Clone(m.slots)
	for i := range slots {
		if slices.Contains(m.selected, slots[i].ID) {
			update(&slots[i])
		}
	}
	s.menuMakerSetSlots(slots)
}
