package ui

import (
	"context"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
)

// gameSelector is the bottom of the folder sidebar: the game and the preset to apply.
func (s *Shell) gameSelector() *fairy.El {
	t := kit.T()
	m := &s.mods
	games := []kit.Item{{Label: T("page.mod.game-selector.games"), Heading: true}}
	for _, game := range m.games {
		games = append(games, kit.Item{Value: game.Game, Label: game.Game})
	}
	gamePlaceholder := T("page.mod.game-selector.select_game")
	if len(m.games) == 0 {
		gamePlaceholder = T("page.mod.game-selector.no_games")
	}

	presets := []kit.Item{{Label: T("page.mod.game-selector.presets"), Heading: true}}
	for _, preset := range m.presets {
		label := preset.Name
		if preset.IsLegacy {
			label += " (" + T("page.mod.dialog.preset-management.legacy-badge") + ")"
		}
		presets = append(presets, kit.Item{Value: preset.ID, Label: label})
	}
	presetPlaceholder := T("page.mod.game-selector.select_preset")
	if len(m.presets) == 0 {
		presetPlaceholder = T("page.mod.game-selector.no_presets")
	}
	openID := ""
	if m.openPreset != nil {
		openID = m.openPreset.ID
	}
	newPreset := T("page.mod.dialog.add-preset.title")

	gameRow := fairy.Row().Gap(4)
	if len(m.games) > 0 {
		launch := T("page.mod.game-selector.launch_game")
		gameRow.Child(kit.IconButton(lucide.Play, s.playGame, kit.Outline, kit.Disabled(m.game == ""),
			kit.Tooltip(launch)).Label(launch))
	}
	editGame := kit.RowAction(lucide.Pencil, T("page.mod.dialog.edit-game.title"), func(name string) {
		for _, game := range m.games {
			if game.Game == name {
				s.openEditGame(game)
				return
			}
		}
	})
	gameRow.Child(kit.Select("mod.game", m.game, games, func(game string) { s.selectGame(game, true) },
		kit.Placeholder(gamePlaceholder), kit.Disabled(len(m.games) == 0), editGame).Grow(1).MinW(0))
	addGame := T("page.mod.dialog.add-game.title")
	gameRow.Child(kit.IconButton(lucide.Plus, s.openAddGame, kit.Outline, kit.Tooltip(addGame)).Label(addGame))

	return fairy.Col(
		gameRow,
		fairy.Row(
			kit.Select("mod.preset", openID, presets, s.openPresetDialog, kit.Placeholder(presetPlaceholder),
				kit.Disabled(len(m.presets) == 0)).Grow(1).MinW(0),
			kit.IconButton(lucide.Plus, func() { m.presetDraft = &presetDraft{} }, kit.Outline,
				kit.Disabled(m.game == ""), kit.Tooltip(newPreset)).Label(newPreset),
		).Gap(4),
	).Shrink(0).Pad(8).Gap(12).BorderEdges(1, 0, 0, 0, t.Border)
}

func (s *Shell) openPresetDialog(id string) {
	m := &s.mods
	for i := range m.presets {
		if m.presets[i].ID == id {
			preset := m.presets[i]
			m.openPreset = &preset
			m.presetBusy = false
		}
	}
}

// presetDialog shows the chosen preset with its description, to apply or delete it.
func (s *Shell) presetDialog(cx *fairy.Cx) {
	t := kit.T()
	m := &s.mods
	preset := m.openPreset
	if preset == nil {
		return
	}
	body := fairy.Col().Gap(8)
	if preset.Description != nil && *preset.Description != "" {
		body.Child(fairy.Text(*preset.Description).FontSize(14).LineHeight(20).Color(t.MutedForeground).Wrap())
	}
	if preset.IsLegacy {
		body.Child(
			fairy.Text(T("page.mod.dialog.preset-management.legacy-description")).
				FontSize(14).
				LineHeight(20).
				Color(t.Destructive).
				Wrap(),
		)
	}
	close := func() {
		if !m.presetBusy {
			m.openPreset = nil
		}
	}
	footer := fairy.Row(
		kit.Button(T("g.delete"), func() { s.deletePreset(*preset) }, kit.Destructive, kit.Disabled(m.presetBusy)).
			AutoFocus(),
		fairy.Row(
			kit.Button(T("g.cancel"), close, kit.Outline),
			kit.Button(T("g.apply"), func() { s.applyPreset(*preset) }, kit.Disabled(preset.IsLegacy || m.presetBusy)),
		).Gap(8),
	).Justify(fairy.JustifyBetween).Gap(8).Pad(16).MarginEdges(0, -16, -16, -16).
		BorderEdges(1, 0, 0, 0, t.Border).Fill(t.Muted.Alpha(0.5)).Radii(0, 0, t.RadiusXl(), t.RadiusXl())
	kit.ShowDialog(cx, kit.DialogProps{
		Title:   preset.Name,
		Body:    fairy.Col(body, footer).Gap(16),
		OnClose: close,
		Key:     "mod.preset",
	})
}

func (s *Shell) applyPreset(preset mod.Preset) {
	m := &s.mods
	if m.presetBusy {
		return
	}
	m.presetBusy = true
	s.background(func(ctx context.Context) {
		result, err := s.deps.Mod.ApplyPreset(ctx, preset.ID)
		fairy.Go(func() {
			m.presetBusy = false
			if err != nil {
				message := err.Error()
				if strings.Contains(message, "LEGACY_PRESET_NOT_SUPPORTED") {
					message = T("page.mod.hooks.use-mod-mutations.apply-preset-mutation.legacy")
				}
				s.toasts.Show(kit.ToastError, message, "")
				return
			}
			m.openPreset = nil
			s.refreshFolders()
			s.refreshListing()
			const prefix = "page.mod.hooks.use-mod-mutations.apply-preset-mutation."
			if n := len(result.Failed); n > 0 {
				s.toasts.Show(kit.ToastWarning, T(prefix+"failed", "count", n), "")
			}
			if n := len(result.Missing); n > 0 {
				s.toasts.Show(kit.ToastWarning, T(prefix+"missing", "count", n), "")
			}
			if len(result.Failed) == 0 && len(result.Missing) == 0 {
				s.toasts.Show(kit.ToastSuccess, T(prefix+"success"), "")
			}
		})
	})
}

func (s *Shell) deletePreset(preset mod.Preset) {
	m := &s.mods
	s.Confirm(Confirmation{
		Title:        T("page.mod.dialog.preset-management.delete-title"),
		Description:  T("page.mod.dialog.preset-management.delete-description", "name", preset.Name),
		ConfirmLabel: T("g.delete"),
		Danger:       true,
		OnResult: func(confirmed bool) {
			if !confirmed {
				return
			}
			m.presetBusy = true
			s.background(func(ctx context.Context) {
				err := s.deps.Mod.DeletePreset(ctx, preset.ID)
				fairy.Go(func() {
					m.presetBusy = false
					if err != nil {
						s.toasts.Show(kit.ToastError, err.Error(), "")
						return
					}
					m.openPreset = nil
					s.loadPresets()
				})
			})
		},
	})
}

// presetDraft is the form that saves the enabled state of the game's mods as a new preset. Folders that count as
// the same mod once the DISABLED prefix is ignored are listed before the preset is created.
type presetDraft struct {
	name        string
	description string
	touched     bool
	busy        bool
	conflicts   []mod.PresetConflict
}

func (s *Shell) presetDraftDialog(cx *fairy.Cx) {
	t := kit.T()
	m := &s.mods
	d := m.presetDraft
	if d == nil {
		return
	}
	close := func() {
		if !d.busy {
			m.presetDraft = nil
		}
	}
	name := strings.TrimSpace(d.name)
	invalid := d.touched && name == ""
	nameLabel := T("g.name")
	nameField := kit.TextField(
		d.name,
		func(v string) { d.name, d.touched = v, true },
		kit.Placeholder(
			nameLabel,
		),
		kit.Label(nameLabel),
	).AutoFocus().OnSubmit(func(string) { s.createPreset(d, false) })
	nameRow := fairy.Col(fairy.Text(nameLabel).FontSize(14).LineHeight(17.5).Medium().Role(fairy.RoleHidden), nameField).
		Gap(8)
	if invalid {
		nameRow.Child(fairy.Text(T("page.mod.dialog.add-preset.#.0")).FontSize(14).LineHeight(20).Color(t.Destructive))
	}
	descriptionLabel := T("page.mod.dialog.add-preset.description-placeholder")
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T("page.mod.dialog.add-preset.title"),
		Body: fairy.Col(
			nameRow,
			fairy.Col(
				fairy.Text(descriptionLabel).FontSize(14).LineHeight(17.5).Medium().Role(fairy.RoleHidden),
				kit.TextArea(d.description, func(v string) { d.description = v }, kit.Placeholder(descriptionLabel),
					kit.Label(descriptionLabel)),
			).Gap(8),
		).Gap(12),
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline),
			kit.Button(T("g.create"), func() { s.createPreset(d, false) }, kit.Loading(d.busy),
				kit.Disabled(name == "")),
		},
		OnClose: close,
		Key:     "mod.new-preset",
	})
	if d.conflicts != nil {
		s.presetConflictDialog(cx, d)
	}
}

func (s *Shell) presetConflictDialog(cx *fairy.Cx, d *presetDraft) {
	t := kit.T()
	const prefix = "page.mod.dialog.add-preset.conflict."
	list := fairy.Col().MaxH(256).ScrollY().Gap(12)
	for _, conflict := range d.conflicts {
		item := fairy.Col(
			fairy.Text(T(prefix+"mod-key-label")).FontSize(14).LineHeight(20).Medium(),
			fairy.Text(conflict.ModKey).FontSize(14).LineHeight(20).Color(t.MutedForeground),
		).Pad(12).Radius(t.RadiusMd()).Fill(t.Muted.Alpha(0.5))
		candidates := fairy.Col().Gap(4).MarginEdges(8, 0, 0, 0)
		for _, candidate := range conflict.Candidates {
			state := T(prefix + "disabled")
			if candidate.IsEnabled {
				state = T(prefix + "enabled")
			}
			candidates.Child(fairy.Row(
				fairy.Text(candidate.RelativePath).FontSize(14).LineHeight(20).Grow(1).MinW(0),
				fairy.Text(state).FontSize(14).LineHeight(20).Shrink(0).Color(t.MutedForeground),
			).Gap(12).Align(fairy.Start))
		}
		list.Child(item.Child(candidates))
	}
	cancel := func() {
		if !d.busy {
			d.conflicts = nil
		}
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + "title"),
		Alert:       true,
		Description: T(prefix + "description"),
		Body:        list,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), cancel, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T(prefix+"action"), func() { s.createPreset(d, true) }, kit.Loading(d.busy)),
		},
		Key:      "mod.preset-conflicts",
		OnEscape: cancel,
	})
}

// createPreset saves the preset. Without resolve it lists the duplicate folders first when there are any.
func (s *Shell) createPreset(d *presetDraft, resolve bool) {
	m := &s.mods
	name := strings.TrimSpace(d.name)
	d.touched = true
	if d.busy || name == "" {
		return
	}
	d.busy = true
	game, description := m.game, d.description
	s.background(func(ctx context.Context) {
		var conflicts []mod.PresetConflict
		var err error
		// A failed conflict lookup shows the backend's message, as the React dialog did.
		lookupFailed := false
		if !resolve {
			conflicts, err = s.deps.Mod.GetPresetCreateConflicts(ctx, game)
			lookupFailed = err != nil
		}
		// A failed resolution keeps the conflict list open with the conflicts as they are now.
		var listed []mod.PresetConflict
		if err == nil && len(conflicts) == 0 {
			_, err = s.deps.Mod.CreatePreset(ctx, game, name, &description, resolve)
			switch {
			case err != nil && strings.Contains(err.Error(), "PRESET_CONFLICTS_EXIST"):
				conflicts, err = s.deps.Mod.GetPresetCreateConflicts(ctx, game)
				lookupFailed = err != nil
			case err != nil && resolve && strings.Contains(err.Error(), "PRESET_CONFLICT_RESOLUTION_FAILED"):
				listed, _ = s.deps.Mod.GetPresetCreateConflicts(ctx, game)
			}
		}
		fairy.Go(func() {
			d.busy = false
			switch {
			case err != nil && lookupFailed:
				s.modError(err, "get-preset-create-conflicts", map[string]any{"game": game})
				s.toasts.Show(kit.ToastError, err.Error(), "")
			case err != nil:
				s.showPresetError(err)
				if len(listed) > 0 {
					d.conflicts = listed
				}
			case len(conflicts) > 0:
				d.conflicts = conflicts
			default:
				if m.presetDraft == d {
					m.presetDraft = nil
				}
				if m.game == game {
					s.loadPresets()
				}
			}
		})
	})
}

func (s *Shell) showPresetError(err error) {
	const prefix = "page.mod.hooks.use-mod-mutations.create-preset-mutation."
	switch text := err.Error(); {
	case strings.Contains(text, "PRESET_NAME_EXISTS"):
		s.toasts.Show(kit.ToastError, T(prefix+"duplicate-name"), "")
	case strings.Contains(text, "PRESET_CONFLICT_RESOLUTION_FAILED"):
		s.toasts.Show(kit.ToastError, T(prefix+"conflict-resolve-failed"), "")
	default:
		s.modError(err, "create-preset", nil)
		s.toasts.Show(kit.ToastError, T(prefix+"failed"), "")
	}
}
