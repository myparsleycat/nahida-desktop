package ui

import (
	"context"
	"math"
	"slices"
	"strings"

	"github.com/samber/lo"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
)

const (
	nteImporter = "NTE"
	// noImporter is the select value of a game without an importer, whose mod folder is chosen by hand.
	noImporter = "__none__"
)

// gameDraft is the add or edit game form. The path field holds the NTE install folder for NTE, the mod folder
// without an importer, and nothing for an XXMI importer, whose own Mods folder the backend resolves.
type gameDraft struct {
	editing     string
	name        string
	nameTouched bool
	importer    string
	path        string
	pathTouched bool
	customPath  string
	busy        bool

	nte        *mod.NtePathResolution
	resolving  bool
	notFound   bool
	resolveSeq int
}

func importerOf(game mod.GameConfig) string {
	if game.Importer == nil || *game.Importer == "" {
		return noImporter
	}
	return *game.Importer
}

func gamePathKind(importer string) string {
	switch importer {
	case nteImporter:
		return "nte"
	case noImporter:
		return "manual"
	}
	return "xxmi"
}

func (s *Shell) openAddGame() {
	s.mods.gameDraft = &gameDraft{importer: noImporter}
	s.mods.nteProgress = nil
}

func (s *Shell) openEditGame(game mod.GameConfig) {
	d := &gameDraft{editing: game.Game, importer: importerOf(game)}
	d.path = d.savedPath(game, d.importer)
	if game.LinkedModFolderPath != nil && *game.LinkedModFolderPath != "" {
		d.customPath = game.ModFolderPath
	}
	if d.importer == nteImporter {
		d.nte = &mod.NtePathResolution{
			GameRootPath:        deref(game.GameInstallPath),
			ExecutablePath:      deref(game.GameExecutablePath),
			ModFolderPath:       game.ModFolderPath,
			LinkedModFolderPath: game.ModFolderPath,
		}
		if linked := deref(game.LinkedModFolderPath); linked != "" {
			d.nte.LinkedModFolderPath = linked
		}
	}
	s.mods.gameDraft = d
	s.mods.nteProgress = nil
}

// savedPath is the stored value of the path field for importer, kept only while importer keeps the saved kind.
func (d *gameDraft) savedPath(game mod.GameConfig, importer string) string {
	if gamePathKind(importer) != gamePathKind(importerOf(game)) {
		return ""
	}
	switch importer {
	case nteImporter:
		for _, path := range []*string{game.GameInstallPath, game.LinkedModFolderPath} {
			if path != nil && *path != "" {
				return *path
			}
		}
		return game.ModFolderPath
	case noImporter:
		return game.ModFolderPath
	}
	return ""
}

func (d *gameDraft) resetNte() {
	d.resolveSeq++
	d.nte, d.resolving, d.notFound = nil, false, false
}

func (s *Shell) editingGame() *mod.GameConfig {
	d := s.mods.gameDraft
	if d == nil || d.editing == "" {
		return nil
	}
	for i := range s.mods.games {
		if s.mods.games[i].Game == d.editing {
			return &s.mods.games[i]
		}
	}
	return nil
}

// setGameImporter switches the importer. The path field holds a different kind of path per importer kind, so a
// value of one kind never carries over to another.
func (s *Shell) setGameImporter(d *gameDraft, importer string) {
	previous := d.importer
	d.importer = importer
	d.resetNte()
	if gamePathKind(previous) != gamePathKind(importer) {
		d.path, d.pathTouched = "", false
		if game := s.editingGame(); game != nil {
			d.path = d.savedPath(*game, importer)
		}
	}
	if previous == nteImporter && importer != nteImporter {
		d.customPath = ""
	}
	if importer == nteImporter && d.editing == "" {
		d.name = T("page.mod.dialog.add-game.nte_game_name")
	}
}

func (s *Shell) pickGameFolder(picked func(string)) {
	go func() {
		path, err := s.deps.Mod.PickFolder()
		fairy.Go(func() {
			if err != nil {
				s.modError(err, "pick-game-folder", nil)
				s.toasts.Show(kit.ToastError, err.Error(), "")
				return
			}
			if path != nil && *path != "" {
				picked(*path)
			}
		})
	}()
}

// resolveNte finds the NTE game below path. A newer path change drops the result of an earlier lookup.
func (s *Shell) resolveNte(d *gameDraft, then func()) {
	d.resolveSeq++
	seq, path := d.resolveSeq, strings.TrimSpace(d.path)
	d.resolving = true
	s.background(func(ctx context.Context) {
		resolution, err := s.deps.Mod.ResolveNteInstallPath(ctx, path)
		fairy.Go(func() {
			if seq != d.resolveSeq {
				return
			}
			d.resolving = false
			if err != nil {
				s.modError(err, "resolve-nte-install-path", map[string]any{"path": path})
				s.toasts.Show(kit.ToastError, err.Error(), "")
				return
			}
			d.nte, d.notFound = resolution, resolution == nil
			if resolution != nil && then != nil {
				then()
			}
		})
	})
}

// nteLinkedFolder is the folder the game Mods folder links to, or nil when the game uses its own Mods folder.
func nteLinkedFolder(resolution mod.NtePathResolution, customPath string) *string {
	linked := resolution.LinkedModFolderPath
	if customPath == "" &&
		platform.SamePathFold(strings.TrimSpace(resolution.ModFolderPath), strings.TrimSpace(linked)) {
		return nil
	}
	return &linked
}

func (s *Shell) submitGame(d *gameDraft) {
	d.nameTouched, d.pathTouched = true, true
	if d.busy || d.resolving {
		return
	}
	name, path, customPath := strings.TrimSpace(d.name), strings.TrimSpace(d.path), strings.TrimSpace(d.customPath)
	if d.importer == nteImporter && d.editing == "" {
		name = T("page.mod.dialog.add-game.nte_game_name")
	}
	if d.editing == "" && name == "" || (d.importer == nteImporter || d.importer == noImporter) && path == "" {
		return
	}
	if d.importer == nteImporter && d.nte == nil {
		s.resolveNte(d, func() {
			if s.mods.gameDraft == d {
				s.submitGame(d)
			}
		})
		return
	}

	updates := mod.GameUpdates{}
	switch d.importer {
	case nteImporter:
		importer := nteImporter
		updates = mod.GameUpdates{
			ModFolderPath:       d.nte.ModFolderPath,
			Importer:            &importer,
			LinkedModFolderPath: nteLinkedFolder(*d.nte, customPath),
			GameInstallPath:     &d.nte.GameRootPath,
			GameExecutablePath:  &d.nte.ExecutablePath,
		}
		if customPath != "" {
			updates.ModFolderPath = customPath
		}
	case noImporter:
		updates.ModFolderPath = path
	default:
		importer := d.importer
		updates.Importer = &importer
	}

	d.busy = true
	s.mods.nteProgress = nil
	editing := d.editing
	s.background(func(ctx context.Context) {
		var err error
		if editing == "" {
			err = s.deps.Mod.AddGame(ctx, name, updates.ModFolderPath, updates.Importer, updates.LinkedModFolderPath,
				updates.GameInstallPath, updates.GameExecutablePath)
		} else {
			err = s.deps.Mod.UpdateGame(ctx, editing, updates)
		}
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				s.showGameError(err, editing, updates)
				return
			}
			if s.mods.gameDraft == d {
				s.mods.gameDraft = nil
			}
			s.loadGames(false)
			if editing != "" && editing == s.mods.game {
				s.selectGroup(nil)
				s.refreshFolders()
				s.background(func(ctx context.Context) {
					if err := s.deps.Mod.WatchGame(ctx, editing); err != nil {
						s.modError(err, "watch-game", map[string]any{"game": editing})
					}
				})
			}
		})
	})
}

func (s *Shell) showGameError(err error, editing string, updates mod.GameUpdates) {
	const prefix = "page.mod.hooks.use-mod-mutations.add-game-mutation."
	codes := []struct{ code, key string }{
		{"DUPLICATE_GAME_NAME", "duplicate-game-name"},
		{"INVALID_MOD_FOLDER_PATH", "invalid-mod-folder-path"},
		{"DUPLICATE_MOD_FOLDER_PATH", "duplicate-mod-folder-path"},
		{"IMPORTER_NOT_AVAILABLE", "importer-not-available"},
		{"INVALID_PARAMS", "invalid-params"},
		{"NTE_MODS_LINK_CONFLICT", "nte_mods_link_conflict"},
		{"NTE_MODS_LINK_PATH_OCCUPIED", "nte_mods_link_path_occupied"},
		{"NTE_CUSTOM_MOD_FOLDER_INSIDE_LINK_PATH", "nte_custom_mod_folder_inside_link_path"},
	}
	text := err.Error()
	for _, c := range codes {
		if strings.Contains(text, c.code) {
			kind := kit.ToastError
			if strings.HasPrefix(c.code, "DUPLICATE_") {
				kind = kit.ToastWarning
			}
			s.toasts.Show(kind, T(prefix+c.key), "")
			return
		}
	}

	operation := "add-game"
	if editing != "" {
		operation = "update-game"
	}
	s.modError(err, operation, map[string]any{
		"game": editing, "modFolderPath": updates.ModFolderPath, "importer": deref(updates.Importer),
		"gameInstallPath": deref(updates.GameInstallPath),
	})
	// React's update path showed the backend's message, and its add path only the NTE bootstrap ones.
	if strings.Contains(text, "NTE_BOOTSTRAP_") || editing != "" && text != "" && text != "Unknown error" {
		s.toasts.Show(kit.ToastError, text, "")
		return
	}
	s.toasts.Show(kit.ToastError, T(prefix+"failed"), "")
}

func modDialogField(label string, control *fairy.El) *fairy.El {
	return fairy.Col(fairy.Text(label).FontSize(14).LineHeight(19.25).Medium(), control.Label(label)).Gap(8)
}

func (s *Shell) gameDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.gameDraft
	if d == nil {
		return
	}
	editing := s.editingGame()
	if d.editing != "" && editing == nil && m.gamesLoaded {
		m.gameDraft = nil
		return
	}
	t := kit.T()
	nte, manual := d.importer == nteImporter, d.importer == noImporter
	close := func() {
		if !d.busy {
			d.resetNte()
			m.gameDraft = nil
		}
	}

	body := fairy.Col().Gap(16).PadXY(0, 16)
	if d.editing == "" {
		name := d.name
		if nte {
			name = T("page.mod.dialog.add-game.nte_game_name")
		}
		invalid := d.nameTouched && strings.TrimSpace(name) == ""
		var onChange func(string)
		if !nte {
			onChange = func(v string) { d.name, d.nameTouched = v, true }
		}
		field := kit.TextField(name, onChange, kit.Invalid(invalid))
		field.AutoFocus().OnSubmit(func(string) { s.submitGame(d) })
		nameField := modDialogField(T("page.mod.dialog.add-game.name_input_placeholder"), field)
		if invalid {
			nameField.Child(
				fairy.Text(T("page.mod.dialog.add-game.#.0")).FontSize(14).LineHeight(20).Color(t.Destructive),
			)
		}
		body.Child(nameField)
	}

	body.Child(s.gameImporterField(d, editing))

	if nte || manual {
		label, required := T("page.mod.dialog.add-game.path_input_placeholder"), T("page.mod.dialog.add-game.#.1")
		if d.editing != "" {
			label = T("page.mod.dialog.edit-game.path_label")
		}
		if nte {
			label, required = T("page.mod.dialog.add-game.nte_install_path"), T("page.mod.dialog.add-game.#.2")
		}
		invalid := d.pathTouched && strings.TrimSpace(d.path) == ""
		var onChange func(string)
		if nte {
			onChange = func(v string) { d.path, d.pathTouched = v, true; d.resetNte() }
		}
		pick := func() {
			s.pickGameFolder(func(path string) {
				d.path, d.pathTouched = path, true
				d.resetNte()
				if d.importer == nteImporter {
					s.resolveNte(d, nil)
				}
			})
		}
		pathField := kit.TextField(d.path, onChange, kit.Invalid(invalid), kit.Placeholder(label)).
			Label(label).
			Grow(1).
			MinW(0)
		if d.editing == "" {
			pathField.FocusVisibleBorder(t.Input).FocusRing(0, fairy.Transparent)
		}
		row := fairy.Row(
			pathField,
			kit.IconButton(lucide.FolderOpen, pick, kit.Outline, kit.Tooltip(T("g.browse")),
				kit.Disabled(nte && d.resolving)).Label(T("g.browse")),
		).Gap(8)
		field := modDialogField(label, row)
		if nte && d.nte != nil {
			field.Child(fairy.Text(d.nte.ModFolderPath).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
		}
		if invalid {
			field.Child(fairy.Text(required).FontSize(14).LineHeight(20).Color(t.Destructive))
		}
		if nte && d.notFound {
			field.Child(
				fairy.Text(T("page.mod.dialog.add-game.nte_not_found")).
					FontSize(14).
					LineHeight(20).
					Color(t.Destructive),
			)
		}
		body.Child(field)
	}

	if nte {
		body.Child(s.nteCustomFolderField(d))
		if d.editing == "" && d.nte != nil && d.nte.RequiresElevation {
			amber := fairy.Hex(0xfe9a00)
			ink := fairy.Hex(0x461901)
			if t.Dark {
				ink = fairy.Hex(0xfee685)
			}
			body.Child(fairy.Row(
				fairy.Icon(lucide.ShieldAlert).Size(16, 16).Color(ink).MarginEdges(2, 0, 0, 0).Shrink(0),
				fairy.Col(
					fairy.Text(T("page.mod.dialog.add-game.nte_system_folder_warning_title")).
						FontSize(14).LineHeight(20).Medium().Color(ink),
					fairy.Text(T("page.mod.dialog.add-game.nte_system_folder_warning_description")).
						FontSize(14).LineHeight(20).Color(ink.Alpha(0.8)),
				).Gap(2).Grow(1).MinW(0),
			).Gap(8).PadXY(10, 8).Radius(t.RadiusLg()).Border(1, amber.Alpha(0.4)).Fill(amber.Alpha(0.1)))
		}
	}
	if progress := s.nteProgressView(d.busy && nte); progress != nil {
		body.Child(progress)
	}
	if editing != nil {
		body.Child(s.gameOrderField(*editing))
		body.Child(
			fairy.Row(kit.Button(T("page.mod.dialog.delete-game.title"), func() { s.confirmDeleteGame(editing.Game) },
				kit.Outline, kit.Icon(lucide.Trash), kit.Disabled(d.busy))).Justify(fairy.JustifyEnd),
		)
	}

	// As the React form, a field counts as invalid once it was edited and left empty.
	invalid := d.pathTouched && (nte || manual) && strings.TrimSpace(d.path) == ""
	if d.editing == "" && !nte && d.nameTouched && strings.TrimSpace(d.name) == "" {
		invalid = true
	}
	title, submit := T("page.mod.dialog.add-game.title"), T("g.add")
	if d.editing != "" {
		title, submit = d.editing, T("g.save")
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title: title,
		Body:  body,
		Width: 384,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(submit, func() { s.submitGame(d) }, kit.Loading(d.busy || d.resolving), kit.Disabled(invalid)),
		},
		OnClose: close,
		Key:     "mod.game",
	})
}

func errorText(invalid bool, text string) string {
	if invalid {
		return text
	}
	return ""
}

// gameImporterField lists the enabled XXMI importers and NTE. Adding a game disables importers that are not
// installed; editing keeps a game's saved importer listed even after it was disabled, so opening the dialog never
// changes it.
func (s *Shell) gameImporterField(d *gameDraft, editing *mod.GameConfig) *fairy.El {
	overview := s.xxmiOverview()
	items := []kit.Item{{Value: noImporter, Label: T("page.mod.dialog.edit-game.no_importer")}}
	for _, importer := range overview.value.Importers {
		item := kit.Item{Value: importer.Key, Label: importer.Key}
		if editing == nil && importer.InstalledVersion == nil {
			item.Label += " (" + T("page.setting.xxmi.builtin.notInstalled") + ")"
			item.Disabled = true
		}
		items = append(items, item)
	}
	if editing != nil {
		if saved := importerOf(*editing); saved != noImporter && saved != nteImporter &&
			!slices.ContainsFunc(items, func(item kit.Item) bool { return item.Value == saved }) {
			items = append(items, kit.Item{Value: saved, Label: saved})
		}
	}
	items = append(items, kit.Item{Value: nteImporter, Label: nteImporter})

	label := T("page.mod.dialog.edit-game.importer_label")
	field := modDialogField(label, kit.Select("mod.game.importer", d.importer, items,
		func(v string) { s.setGameImporter(d, v) }, kit.Placeholder(T("g.select"))).WFull().AutoFocus())
	if editing == nil && overview.loaded && !overview.value.Configured && d.importer != nteImporter {
		t := kit.T()
		field.Child(fairy.Col(
			fairy.Text(T("page.mod.dialog.add-game.xxmi_path_required")).
				FontSize(14).
				LineHeight(20).
				Color(t.MutedForeground),
			kit.Button(T("page.mod.dialog.add-game.open_xxmi_settings"), func() {
				s.mods.gameDraft = nil
				s.navigate(RouteXXMI)
			}, kit.Outline).AlignSelf(fairy.Start),
		).Gap(12).PadXY(10, 8).Radius(t.RadiusLg()).Border(1, t.Border).Fill(t.Card))
	}
	return field
}

func (s *Shell) nteCustomFolderField(d *gameDraft) *fairy.El {
	t := kit.T()
	label := T("page.mod.dialog.add-game.nte_custom_mod_folder")
	pick := func() { s.pickGameFolder(func(path string) { d.customPath = path }) }
	return fairy.Col(
		fairy.Text(label).FontSize(14).LineHeight(19.25).Medium(),
		fairy.Row(
			kit.TextField(d.customPath, nil).Label(label).Grow(1).MinW(0).
				FocusVisibleBorder(t.Input).FocusRing(0, fairy.Transparent),
			kit.IconButton(lucide.FolderOpen, pick, kit.Outline, kit.Tooltip(T("g.browse"))).Label(T("g.browse")),
			kit.IconButton(lucide.X, func() { d.customPath = "" }, kit.Outline, kit.Tooltip(T("g.clear")),
				kit.Disabled(d.customPath == "")).Label(T("g.clear")),
		).Gap(8),
		fairy.Text(T("page.mod.dialog.add-game.nte_custom_mod_folder_description")).
			FontSize(12).
			LineHeight(16).
			Color(t.MutedForeground).
			Wrap(),
	).Gap(8)
}

// nteProgressView shows the bootstrap progress while active, and a failure until the next attempt.
func (s *Shell) nteProgressView(active bool) *fairy.El {
	progress := s.mods.nteProgress
	if !active && (progress == nil || progress.Phase != "failed") {
		return nil
	}
	if progress == nil {
		progress = &mod.NteBootstrapProgress{Phase: "checking", Progress: new(float64)}
	}
	t := kit.T()
	const prefix = "page.mod.dialog.add-game.nte_bootstrap_progress."
	var icon *fairy.El
	switch progress.Phase {
	case "failed":
		icon = fairy.Icon(lucide.CircleX).Size(16, 16).Color(t.Destructive)
	case "completed":
		icon = fairy.Icon(lucide.CircleCheck).Size(16, 16).Color(t.Primary)
	default:
		icon = kit.Spinner(16)
	}
	value, amount := 0.15, T(prefix+"working")
	if progress.Progress != nil {
		value = *progress.Progress / 100
		amount = T(prefix+"percent", "value", int(math.Round(*progress.Progress)))
	}
	bar := kit.Progress(value)
	if progress.Progress == nil {
		bar.Pulse(2, 0.5)
	}
	view := fairy.Col(
		fairy.Row(
			icon.Shrink(0),
			fairy.Text(T(prefix+progress.Phase, "archiveName", progress.ArchiveName)).FontSize(12).LineHeight(16).
				Ellipsis().Grow(1).MinW(0),
			fairy.Text(amount).FontSize(12).LineHeight(16).Shrink(0),
		).Gap(8).Align(fairy.Center).FontSize(12).Color(t.MutedForeground),
		bar,
	).Gap(8).PadXY(12, 8).Radius(6).Fill(t.Muted.Alpha(0.5))
	if progress.Message != "" {
		view.Child(fairy.Text(progress.Message).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap())
	}
	return view
}

func (s *Shell) gameOrderField(game mod.GameConfig) *fairy.El {
	t := kit.T()
	games := s.mods.games
	index := slices.IndexFunc(games, func(g mod.GameConfig) bool { return g.Game == game.Game })
	move := func(direction int) {
		target := index + direction
		if index < 0 || target < 0 || target >= len(games) {
			return
		}
		order := make([]string, 0, len(games))
		for _, g := range games {
			order = append(order, g.Game)
		}
		order[index], order[target] = order[target], order[index]
		s.reorderGames(order)
	}
	return fairy.Col(
		fairy.Text(T("page.mod.dialog.edit-game.order_label")).FontSize(14).LineHeight(19.25).Medium(),
		fairy.Row(
			fairy.Text(T("page.mod.dialog.edit-game.order_value", "current", index+1, "total", len(games))).
				FontSize(14).LineHeight(20).Color(t.MutedForeground).Grow(1),
			kit.IconButton(lucide.ArrowUp, func() { move(-1) }, kit.Outline, kit.Tooltip(T("g.move_up")),
				kit.Disabled(index <= 0)).Label(T("g.move_up")),
			kit.IconButton(lucide.ArrowDown, func() { move(1) }, kit.Outline, kit.Tooltip(T("g.move_down")),
				kit.Disabled(index < 0 || index >= len(games)-1)).Label(T("g.move_down")),
		).Gap(8).Align(fairy.Center).PadXY(12, 8).Radius(t.RadiusLg()).Fill(t.Muted.Alpha(0.5)),
		fairy.Text(T("page.mod.dialog.edit-game.order_description")).
			FontSize(12).
			LineHeight(16).
			Color(t.MutedForeground).
			Wrap(),
	).Gap(8)
}

// reorderGames shows the new order at once and stores it; a failure reads the stored order back.
func (s *Shell) reorderGames(order []string) {
	m := &s.mods
	byName := map[string]mod.GameConfig{}
	for _, game := range m.games {
		byName[game.Game] = game
	}
	games := make([]mod.GameConfig, 0, len(order))
	for i, name := range order {
		game := byName[name]
		game.Order = int64(i + 1)
		games = append(games, game)
	}
	m.games = games
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.ReorderGames(ctx, order)
		fairy.Go(func() {
			if err != nil {
				s.modError(err, "reorder-games", map[string]any{"games": order})
				s.toasts.Show(kit.ToastError, T("page.mod.hooks.use-mod-mutations.reorder-games-mutation.failed"), "")
			}
			s.loadGames(false)
		})
	})
}

func (s *Shell) confirmDeleteGame(game string) {
	m := &s.mods
	s.Confirm(Confirmation{
		Title:        T("page.mod.dialog.delete-game.title"),
		Description:  T("page.mod.dialog.delete-game.description", "name", game),
		ConfirmLabel: T("g.delete"),
		Danger:       true,
		OnResult: func(confirmed bool) {
			if !confirmed {
				return
			}
			s.background(func(ctx context.Context) {
				err := s.deps.Mod.RemoveGame(ctx, game)
				fairy.Go(func() {
					if err != nil {
						s.modError(err, "remove-game", map[string]any{"game": game})
						s.toasts.Show(kit.ToastError, err.Error(), "")
						return
					}
					if d := m.gameDraft; d != nil && d.editing == game {
						m.gameDraft = nil
					}
					m.openPreset = nil
					m.games = slices.DeleteFunc(m.games, func(g mod.GameConfig) bool { return g.Game == game })
					if m.game == game {
						next := ""
						if len(m.games) > 0 {
							next = m.games[0].Game
						}
						s.selectGame(next, true)
					}
					s.loadGames(false)
				})
			})
		},
	})
}

// playGame launches the selected game: NTE through its saved launcher, any other game through its importer.
func (s *Shell) playGame() {
	game := s.gameConfig()
	if game == nil {
		return
	}
	if importerOf(*game) == nteImporter {
		if deref(game.NteLauncherPath) == "" {
			s.mods.nteLaunch = &nteLaunchDraft{game: game.Game}
			return
		}
		name := game.Game
		s.background(func(ctx context.Context) {
			err := s.deps.Mod.StartNteLauncher(ctx, name)
			if err == nil {
				return
			}
			fairy.Go(func() {
				if strings.Contains(err.Error(), "NTE_LAUNCHER_PATH_NOT_FOUND") {
					s.toasts.Show(
						kit.ToastError,
						T("page.mod.hooks.use-mod-mutations.start-nte-launcher.not-found"),
						"",
					)
					s.mods.nteLaunch = &nteLaunchDraft{game: name}
					return
				}
				s.showNteLaunchError(err, name)
			})
		})
		return
	}

	overview := s.xxmiOverview()
	if overview.loaded && !overview.value.Configured {
		s.toasts.Show(kit.ToastInfo, T("page.mod.dialog.add-game.xxmi_path_required"), "")
		s.navigate(RouteXXMI)
		return
	}
	if game.Importer == nil || *game.Importer == "" {
		s.toasts.Show(kit.ToastWarning, T("page.mod.play.no_importer"), "")
		return
	}
	s.startImporter(*game.Importer)
}

func (s *Shell) showNteLaunchError(err error, game string) {
	const prefix = "page.mod.hooks.use-mod-mutations.start-nte-launcher."
	switch text := err.Error(); {
	case strings.Contains(text, "NTE_LAUNCHER_PATH_NOT_FOUND"):
		s.toasts.Show(kit.ToastError, T(prefix+"not-found"), "")
	case strings.Contains(text, "NTE_LAUNCHER_PATH_NOT_SET"):
		s.toasts.Show(kit.ToastError, T(prefix+"not-set"), "")
	default:
		s.modError(err, "start-nte-launcher", map[string]any{"game": game})
		s.toasts.Show(kit.ToastError, lo.CoalesceOrEmpty(text, T(prefix+"failed")), "")
	}
}

// nteLaunchDraft asks for the NTE launcher, which is saved for the game and started.
type nteLaunchDraft struct {
	game string
	path string
	busy bool
}

func (s *Shell) nteLaunchDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.nteLaunch
	if d == nil {
		return
	}
	close := func() {
		if !d.busy {
			m.nteLaunch = nil
		}
	}
	pick := func() {
		go func() {
			path, err := s.deps.Mod.PickExecutable()
			fairy.Go(func() {
				if err != nil {
					s.modError(err, "pick-nte-launcher", nil)
					s.toasts.Show(kit.ToastError, err.Error(), "")
					return
				}
				if path != nil && *path != "" {
					d.path = *path
				}
			})
		}()
	}
	launch := func() {
		path := strings.TrimSpace(d.path)
		if d.busy || path == "" {
			return
		}
		d.busy = true
		s.background(func(ctx context.Context) {
			err := s.deps.Mod.SetNteLauncherPath(ctx, d.game, path)
			saved := err == nil
			if saved {
				err = s.deps.Mod.StartNteLauncher(ctx, d.game)
			}
			fairy.Go(func() {
				d.busy = false
				if saved {
					s.loadGames(false)
				}
				switch {
				case err == nil:
					if m.nteLaunch == d {
						m.nteLaunch = nil
					}
				case !saved && strings.Contains(err.Error(), "INVALID_LAUNCHER_PATH"):
					s.toasts.Show(kit.ToastError,
						T("page.mod.hooks.use-mod-mutations.set-game-executable-path.invalid-path"), "")
				case !saved:
					s.modError(err, "set-nte-launcher-path", map[string]any{"game": d.game, "path": path})
					s.toasts.Show(
						kit.ToastError,
						T("page.mod.hooks.use-mod-mutations.set-game-executable-path.failed"),
						"",
					)
				default:
					s.showNteLaunchError(err, d.game)
				}
			})
		})
	}
	label := T("page.mod.dialog.nte-launch.path_label")
	kit.ShowDialog(cx, kit.DialogProps{
		Title: T("page.mod.dialog.nte-launch.title"),
		Body: modDialogField(label, fairy.Row(
			kit.TextField(d.path, nil).Label(label).Grow(1).MinW(0).AutoFocus().
				FocusVisibleBorder(kit.T().Input).FocusRing(0, fairy.Transparent),
			kit.IconButton(lucide.FolderOpen, pick, kit.Outline, kit.Tooltip(T("g.browse"))).Label(T("g.browse")),
		).Gap(8)),
		Width: 384,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T("page.mod.dialog.nte-launch.launch"), launch, kit.Loading(d.busy),
				kit.Disabled(strings.TrimSpace(d.path) == "")),
		},
		OnClose: close,
		Key:     "mod.nte-launch",
	})
}
