package ui

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/tools"
)

const togglePresetsPrefix = "page.mod.toggle-presets."

// togglePresetsDialog manages the toggle presets of one mod: saving the current values, applying, renaming and
// deleting them. Until a store is chosen it asks for one.
type togglePresetsDialog struct {
	mod    mod.ModInfo
	list   mod.TogglePresetList
	loaded bool
	busy   bool
	linked bool
	name   string
	// renamingID is the preset whose name is being edited inline, and renameName the edited name.
	renamingID string
	renameName string
	// storePick is the store selected in the chooser shown before the first preset.
	storePick string
}

func (s *Shell) openTogglePresets(info mod.ModInfo) {
	s.mods.togglePresets = &togglePresetsDialog{mod: info, storePick: mod.TogglePresetStoreMetadata}
	s.loadTogglePresets()
}

func (s *Shell) loadTogglePresets() {
	d := s.mods.togglePresets
	if d == nil {
		return
	}
	path := d.mod.Path
	s.background(func(ctx context.Context) {
		list, err := s.deps.Mod.GetTogglePresets(ctx, path)
		linked := s.deps.Tools != nil && s.deps.Tools.ModelViewerGameLinked(path)
		fairy.Go(func() {
			if s.mods.togglePresets != d {
				return
			}
			d.loaded, d.linked = true, linked
			if err != nil {
				showTogglePresetError(&s.toasts, err)
				return
			}
			d.list = list
		})
	})
}

// refreshTogglePresetsLinked rechecks whether the capture reads the running game, after the game link changed.
func (s *Shell) refreshTogglePresetsLinked() {
	d := s.mods.togglePresets
	if d == nil || s.deps.Tools == nil {
		return
	}
	path := d.mod.Path
	s.background(func(context.Context) {
		linked := s.deps.Tools.ModelViewerGameLinked(path)
		fairy.Go(func() {
			if s.mods.togglePresets == d {
				d.linked = linked
			}
		})
	})
}

func (s *Shell) togglePresetsDialog(cx *fairy.Cx) {
	d := s.mods.togglePresets
	if d == nil {
		return
	}
	close := func() {
		if !d.busy {
			s.mods.togglePresets = nil
		}
	}
	if d.loaded && d.list.Store == "" {
		togglePresetStoreChooser(cx, "mod.toggle-presets.store", &d.storePick, d.busy, func(store string) {
			s.chooseTogglePresetStore(d, store)
		}, close)
		return
	}

	t := kit.T()
	supported, known := s.desktopChannel(s.currentImporter())
	// A game linked now serves the channel even when the next launch would deploy a DLL without it.
	allowed := supported || d.linked
	header := fairy.Row(
		fairy.Text(T(togglePresetsPrefix+"stored-in")).FontSize(14).LineHeight(20).Color(t.MutedForeground).Grow(1),
		kit.Select("mod.toggle-presets.store-select", d.list.Store, togglePresetStoreItems(false), func(store string) {
			s.changeTogglePresetStore(d.list.Store, store, s.loadTogglePresets)
		}, kit.Disabled(d.busy || !d.loaded), kit.Label(T(togglePresetsPrefix+"stored-in"))).W(208),
	).Gap(8).Align(fairy.Center)

	capture := T(togglePresetsPrefix + "capture-ini")
	captureIcon := lucide.FileText
	if d.linked {
		capture, captureIcon = T(togglePresetsPrefix+"capture-game"), lucide.Gamepad2
	}
	save := func(string) { s.createTogglePreset(d) }
	create := fairy.Col(
		fairy.Row(
			kit.TextField(d.name, func(v string) { d.name = v },
				kit.Placeholder(T(togglePresetsPrefix+"name-placeholder")), kit.Label(T("g.name")),
				kit.Disabled(d.busy)).AutoFocus().OnSubmit(save).Grow(1).MinW(0),
			kit.Button(T(togglePresetsPrefix+"save-current"), func() { save("") }, kit.Icon(lucide.BookmarkPlus),
				kit.Disabled(d.busy || !allowed || strings.TrimSpace(d.name) == "")),
		).Gap(8),
		fairy.Row(
			fairy.Icon(captureIcon).Size(14, 14).Color(t.MutedForeground).Shrink(0),
			fairy.Text(capture).FontSize(13).LineHeight(18).Color(t.MutedForeground).Wrap().Grow(1).MinW(0),
		).Gap(6).Align(fairy.Center),
	).Gap(8)

	body := fairy.Col().Gap(12)
	if known && !allowed {
		body.Child(fairy.Row(
			fairy.Icon(lucide.Info).Size(14, 14).Color(t.MutedForeground).Shrink(0),
			fairy.Text(T(togglePresetsPrefix+"requires-desktop-channel")).FontSize(13).LineHeight(18).
				Color(t.MutedForeground).Wrap().Grow(1).MinW(0),
		).Gap(6).Align(fairy.Center))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(togglePresetsPrefix + "title"),
		Description: d.mod.Name,
		Body:        body.Child(header, create, kit.Separator(), s.togglePresetList(d, allowed)),
		Width:       480,
		OnClose:     close,
		Key:         "mod.toggle-presets",
	})
}

func (s *Shell) togglePresetList(d *togglePresetsDialog, allowed bool) *fairy.El {
	t := kit.T()
	if !d.loaded {
		return fairy.Row(kit.Spinner(16)).Justify(fairy.JustifyCenter).Pad(16)
	}
	if len(d.list.Presets) == 0 {
		return fairy.Text(T(togglePresetsPrefix+"empty")).FontSize(14).LineHeight(20).Color(t.MutedForeground).
			PadXY(0, 16).AlignSelf(fairy.Center)
	}
	list := fairy.Col().Gap(4).MaxH(320).ScrollY()
	for _, preset := range d.list.Presets {
		if d.renamingID == preset.ID {
			commit := func(string) { s.renameTogglePreset(d, preset) }
			cancel := func() { d.renamingID = "" }
			list.Child(fairy.Row(
				kit.TextField(d.renameName, func(v string) { d.renameName = v }, kit.Label(T("g.name")),
					kit.Disabled(d.busy)).AutoFocus().OnSubmit(commit).Grow(1).MinW(0),
				kit.IconButton(lucide.Check, func() { commit("") }, kit.Ghost, kit.Small, kit.Loading(d.busy),
					kit.Tooltip(T("g.save"))).Label(T("g.save")),
				kit.IconButton(lucide.X, cancel, kit.Ghost, kit.Small, kit.Disabled(d.busy),
					kit.Tooltip(T("g.cancel"))).Label(T("g.cancel")),
			).Key("toggle-preset:"+preset.ID).Gap(4).Align(fairy.Center).PadXY(8, 6))
			continue
		}
		detail := togglePresetSourceLabel(preset.Source)
		if updated := formatTogglePresetTime(preset.UpdatedAt); updated != "" {
			detail += " · " + updated
		}
		list.Child(fairy.Row(
			fairy.Col(
				fairy.Text(preset.Name).FontSize(14).LineHeight(20).Medium().Ellipsis(),
				fairy.Text(detail).FontSize(12).LineHeight(16).Color(t.MutedForeground).Ellipsis(),
			).Grow(1).MinW(0),
			kit.Button(
				T("g.apply"),
				func() { s.applyTogglePreset(d.mod.Path, preset, &d.busy) },
				kit.Outline,
				kit.Small,
				kit.Disabled(d.busy || !allowed),
			),
			kit.IconButton(lucide.Pencil, func() { d.renamingID, d.renameName = preset.ID, preset.Name }, kit.Ghost,
				kit.Small, kit.Disabled(d.busy), kit.Tooltip(T("g.rename"))).Label(T("g.rename")),
			kit.IconButton(lucide.Trash, func() { s.deleteTogglePreset(d, preset) }, kit.Ghost, kit.Small,
				kit.Disabled(d.busy), kit.Tooltip(T("g.delete"))).Label(T("g.delete")),
		).Key("toggle-preset:"+preset.ID).Gap(4).Align(fairy.Center).PadXY(8, 6).Radius(t.RadiusMd()).
			Fill(t.Muted.Alpha(0.5)))
	}
	return list
}

func (s *Shell) chooseTogglePresetStore(d *togglePresetsDialog, store string) {
	if d.busy {
		return
	}
	d.busy = true
	s.background(func(ctx context.Context) {
		_, err := s.deps.Mod.SetTogglePresetStore(ctx, store)
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				showTogglePresetError(&s.toasts, err)
				return
			}
			s.loadTogglePresets()
		})
	})
}

// changeTogglePresetStore moves the presets to another store after a confirmation. With no store chosen yet it
// only records the choice. after runs on the UI thread once the store changed.
func (s *Shell) changeTogglePresetStore(from, to string, after func()) {
	if from == to || to == "" {
		return
	}
	if from == "" {
		s.background(func(ctx context.Context) {
			_, err := s.deps.Mod.SetTogglePresetStore(ctx, to)
			fairy.Go(func() {
				if err != nil {
					showTogglePresetError(&s.toasts, err)
				} else if after != nil {
					after()
				}
			})
		})
		return
	}

	const prefix = togglePresetsPrefix + "migrate."
	var migration mod.TogglePresetMigration
	s.Confirm(Confirmation{
		Title: T(prefix + "title"),
		Description: T(prefix+"description", "from", togglePresetStoreLabel(from),
			"to", togglePresetStoreLabel(to)),
		ConfirmLabel: T(prefix + "confirm"),
		Run: func() error {
			ctx := context.Background()
			if s.deps.Ready != nil {
				if err := s.deps.Ready(ctx); err != nil {
					return err
				}
			}
			var err error
			migration, err = s.deps.Mod.SetTogglePresetStore(ctx, to)
			if err != nil && strings.HasPrefix(err.Error(), "TOGGLE_PRESET_MIGRATION_FAILED") {
				return errors.New(T(prefix+"failed", "count", len(migration.Failed)))
			}
			return err
		},
		OnResult: func(confirmed bool) {
			if !confirmed {
				return
			}
			showTogglePresetMigration(&s.toasts, migration)
			if after != nil {
				after()
			}
		},
	})
}

func (s *Shell) createTogglePreset(d *togglePresetsDialog) {
	name := strings.TrimSpace(d.name)
	if d.busy || name == "" {
		return
	}
	d.busy = true
	path := d.mod.Path
	s.background(func(ctx context.Context) {
		preset, err := s.deps.Mod.CreateTogglePreset(ctx, path, name)
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				showTogglePresetError(&s.toasts, err)
				return
			}
			d.name = ""
			s.toasts.Show(kit.ToastSuccess, T(togglePresetsPrefix+"toast.saved"),
				T(togglePresetsPrefix+"toast.saved-from", "source", togglePresetSourceLabel(preset.Source)))
			s.loadTogglePresets()
		})
	})
}

func (s *Shell) renameTogglePreset(d *togglePresetsDialog, preset mod.TogglePreset) {
	name := strings.TrimSpace(d.renameName)
	if d.busy {
		return
	}
	if name == "" || name == preset.Name {
		d.renamingID = ""
		return
	}
	d.busy = true
	path := d.mod.Path
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.RenameTogglePreset(ctx, path, preset.ID, name)
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				showTogglePresetError(&s.toasts, err)
				return
			}
			d.renamingID = ""
			s.loadTogglePresets()
		})
	})
}

func (s *Shell) deleteTogglePreset(d *togglePresetsDialog, preset mod.TogglePreset) {
	s.Confirm(Confirmation{
		Title:        T(togglePresetsPrefix + "delete-title"),
		Description:  T(togglePresetsPrefix+"delete-description", "name", preset.Name),
		ConfirmLabel: T("g.delete"),
		Danger:       true,
		OnResult: func(confirmed bool) {
			if !confirmed || d.busy {
				return
			}
			d.busy = true
			path := d.mod.Path
			s.background(func(ctx context.Context) {
				err := s.deps.Mod.DeleteTogglePreset(ctx, path, preset.ID)
				fairy.Go(func() {
					d.busy = false
					if err != nil {
						showTogglePresetError(&s.toasts, err)
						return
					}
					s.loadTogglePresets()
				})
			})
		},
	})
}

// togglePresetMenu is the toggle preset menu at the top of a mod card's INI list. It reads the presets again each
// time it opens, so changes made in the dialog or the model viewer show without being tracked.
type togglePresetMenu struct {
	presets  []mod.TogglePreset
	loaded   bool
	open     bool
	applying bool
}

func (s *Shell) togglePresetCardMenu(info mod.ModInfo) *fairy.El {
	id := "mod.toggle-presets:" + info.Path
	menu := s.mods.togglePresetMenus[info.Path]
	if open := kit.DropdownMenuOpen(id); open && (menu == nil || !menu.open) {
		if menu == nil {
			menu = &togglePresetMenu{}
			s.mods.togglePresetMenus[info.Path] = menu
		}
		menu.open = true
		s.loadTogglePresetMenu(info.Path, menu)
	} else if !open && menu != nil {
		menu.open = false
	}

	applying := menu != nil && menu.applying
	label := T(togglePresetsPrefix + "menu")
	trigger := kit.IconButton(lucide.SlidersHorizontal, nil, kit.Ghost, kit.Small, kit.Loading(applying)).
		Label(label).Tooltip(label)
	return fairy.Row(kit.DropdownMenuFunc(id, trigger, func() []kit.MenuItem {
		return s.togglePresetCardItems(info, s.mods.togglePresetMenus[info.Path])
	}, kit.MaxWidth(240), kit.AlignEnd)).Justify(fairy.JustifyEnd)
}

func (s *Shell) togglePresetCardItems(info mod.ModInfo, menu *togglePresetMenu) []kit.MenuItem {
	supported, known := s.desktopChannel(s.currentImporter())
	var items []kit.MenuItem
	if known && !supported {
		items = append(items, desktopChannelNote(), kit.MenuItem{Separator: true})
	}
	switch {
	case menu == nil || !menu.loaded:
		items = append(items, kit.MenuItem{Label: T(togglePresetsPrefix + "loading"), Loading: true, Disabled: true})
	case len(menu.presets) == 0:
		items = append(items, kit.MenuItem{Label: T(togglePresetsPrefix + "empty"), Disabled: true})
	default:
		for _, preset := range menu.presets {
			items = append(items, kit.MenuItem{
				Label: preset.Name, Icon: lucide.Bookmark, Disabled: menu.applying || !supported,
				OnClick: func() { s.applyTogglePreset(info.Path, preset, &menu.applying) },
			})
		}
	}
	return append(items, kit.MenuItem{Separator: true}, kit.MenuItem{
		Label: T(togglePresetsPrefix + "manage"), Icon: lucide.Settings2,
		OnClick: func() { s.openTogglePresets(info) },
	})
}

// desktopChannel reports whether the games of importer serve the desktop channel the toggle presets and the game
// preview use. known is false until that has been read.
func (s *Shell) desktopChannel(importer string) (supported, known bool) {
	if s.deps.XXMI == nil || importer == noImporter || importer == nteImporter {
		return false, true
	}
	q := fetch(s, keyed(s.xxmi.desktopChannel, importer), func(ctx context.Context) (bool, error) {
		return s.deps.XXMI.DesktopChannelSupported(ctx, importer)
	})
	return q.loaded && q.value && q.err == nil, q.loaded || q.err != nil
}

func (s *Shell) currentImporter() string {
	if game := s.gameConfig(); game != nil {
		return importerOf(*game)
	}
	return noImporter
}

func desktopChannelNote() kit.MenuItem {
	return kit.MenuItem{Label: T(togglePresetsPrefix + "requires-desktop-channel"), Icon: lucide.Info, Disabled: true}
}

func (s *Shell) loadTogglePresetMenu(path string, menu *togglePresetMenu) {
	s.background(func(ctx context.Context) {
		list, err := s.deps.Mod.GetTogglePresets(ctx, path)
		fairy.Go(func() {
			menu.loaded = true
			if err != nil {
				showTogglePresetError(&s.toasts, err)
				return
			}
			menu.presets = list.Presets
		})
	})
}

// applyTogglePreset applies a preset of the mod at path; busy is the flag of the dialog or menu that started it.
func (s *Shell) applyTogglePreset(path string, preset mod.TogglePreset, busy *bool) {
	if *busy {
		return
	}
	*busy = true
	s.background(func(ctx context.Context) {
		result, err := s.deps.Mod.ApplyTogglePreset(ctx, path, preset.ID)
		fairy.Go(func() {
			*busy = false
			if err != nil {
				showTogglePresetError(&s.toasts, err)
				return
			}
			showToggleApplyResult(&s.toasts, result)
		})
	})
}

// togglePresetStoreChooser asks where toggle presets are stored, before the first one is saved.
func togglePresetStoreChooser(
	cx *fairy.Cx,
	key string,
	pick *string,
	busy bool,
	onConfirm func(store string),
	onClose func(),
) {
	const prefix = togglePresetsPrefix + "store-chooser."
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + "title"),
		Description: T(prefix + "description"),
		Body:        kit.RadioGroup(*pick, togglePresetStoreItems(true), func(store string) { *pick = store }),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), onClose, kit.Outline, kit.Disabled(busy)),
			kit.Button(T("g.confirm"), func() { onConfirm(*pick) }, kit.Loading(busy), kit.Disabled(*pick == "")),
		},
		OnClose: onClose,
		Key:     key,
	})
}

func togglePresetStoreItems(described bool) []kit.Item {
	items := []kit.Item{
		{Value: mod.TogglePresetStoreMetadata, Label: togglePresetStoreLabel(mod.TogglePresetStoreMetadata)},
		{Value: mod.TogglePresetStoreDatabase, Label: togglePresetStoreLabel(mod.TogglePresetStoreDatabase)},
	}
	if described {
		for i := range items {
			items[i].Description = T(togglePresetsPrefix + "store." + items[i].Value + "-description")
		}
	}
	return items
}

func togglePresetStoreLabel(store string) string {
	if store == "" {
		return T(togglePresetsPrefix + "store.unset")
	}
	return T(togglePresetsPrefix + "store." + store)
}

func togglePresetSourceLabel(source string) string {
	switch source {
	case "game", "ini", "mixed", mod.TogglePresetSourceViewer:
		return T(togglePresetsPrefix + "source." + source)
	}
	return source
}

func formatTogglePresetTime(value string) string {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return at.Local().Format("2006-01-02 15:04")
}

// showTogglePresetError shows a toggle preset failure. The mod service has logged unexpected failures already.
func showTogglePresetError(toasts *kit.Toaster, err error) {
	const prefix = togglePresetsPrefix + "toast."
	text := err.Error()
	for code, key := range map[string]string{
		"PRESET_NAME_EXISTS":         "duplicate-name",
		"INVALID_PRESET_NAME":        "invalid-name",
		"TOGGLE_PRESET_NO_VARIABLES": "no-variables",
		"TOGGLE_PRESET_NOT_FOUND":    "not-found",
	} {
		if strings.Contains(text, code) {
			toasts.Show(kit.ToastError, T(prefix+key), "")
			return
		}
	}
	toasts.Show(kit.ToastError, T(prefix+"failed"), text)
}

func showToggleApplyResult(toasts *kit.Toaster, result tools.ToggleStateApplyResult) {
	const prefix = togglePresetsPrefix + "toast."
	inGame := len(result.Applied) > 0
	switch {
	case inGame:
		toasts.Show(kit.ToastSuccess, T(prefix+"applied"), T(prefix+"applied-game"))
	case len(result.Written) > 0 || result.UserINI > 0:
		toasts.Show(kit.ToastSuccess, T(prefix+"applied"), T(prefix+"applied-files"))
	default:
		toasts.Show(kit.ToastInfo, T(prefix+"no-changes"), "")
	}
	if len(result.Missing) > 0 {
		toasts.Show(kit.ToastWarning, T(prefix+"missing-in-game"), "")
	}
	if n := len(result.Skipped) + len(result.MissingINIs); n > 0 {
		toasts.Show(kit.ToastWarning, T(prefix+"skipped", "count", n),
			strings.Join(append(append([]string{}, result.MissingINIs...), result.Skipped...), "\n"))
	}
	if result.UnlinkedGame {
		toasts.Show(kit.ToastWarning, T(prefix+"unlinked-game"), "")
	}
	if result.GameErr != "" {
		toasts.Show(kit.ToastWarning, T(prefix+"game-error"), result.GameErr)
	}
}

func showTogglePresetMigration(toasts *kit.Toaster, migration mod.TogglePresetMigration) {
	const prefix = togglePresetsPrefix + "migrate."
	if migration.From == "" || migration.From == migration.To {
		return
	}
	toasts.Show(kit.ToastSuccess, T(prefix+"done", "moved", migration.Moved, "mods", migration.Mods), "")
	if migration.Renamed+len(migration.Orphaned)+len(migration.Ambiguous)+migration.Leftover > 0 {
		toasts.Show(kit.ToastWarning, T(prefix+"warnings",
			"renamed", migration.Renamed, "orphaned", len(migration.Orphaned),
			"ambiguous", len(migration.Ambiguous), "leftover", migration.Leftover), "")
	}
}

// viewerToggleValue converts a preset's INI text to the type the viewer state holds for the variable.
func viewerToggleValue(current any, text string) any {
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	switch current.(type) {
	case string:
		return text
	case int:
		if err == nil {
			return int(number)
		}
	case float32:
		if err == nil {
			return float32(number)
		}
	default:
		if err == nil {
			return number
		}
	}
	return text
}
