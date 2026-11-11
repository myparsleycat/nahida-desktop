package ui

import (
	"context"
	"errors"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
)

var (
	errBisectExcludeInvalid   = errors.New("bisect exclude path is invalid")
	errBisectExcludeDuplicate = errors.New("bisect exclude path is listed twice")
)

// bisectExclude is a row of the exclude list. committed is the validated path relative to the importer, set once
// the backend accepted the row; failed is the text that last failed, so it is not validated again until edited.
type bisectExclude struct {
	id        int
	value     string
	committed *string
	failed    string
}

// bisectTool is the mod bisect tool. game is the game the user picked; "" means the first game NTE does not own.
type bisectTool struct {
	game       string
	excludes   []*bisectExclude
	nextID     int
	busy       bool
	recovering bool
	// stateRead is set once the backend's bisect state was read for this screen.
	stateRead bool
}

func (b *bisectTool) addExclude() {
	b.nextID++
	b.excludes = append(b.excludes, &bisectExclude{id: b.nextID})
}

func (b *bisectTool) removeExclude(row *bisectExclude) {
	for i, other := range b.excludes {
		if other == row {
			b.excludes = append(b.excludes[:i], b.excludes[i+1:]...)
			return
		}
	}
}

func (s *Shell) bisectGame() string {
	b := &s.tools.bisect
	if b.game != "" {
		return b.game
	}
	for _, game := range s.mods.games {
		if deref(game.Importer) != nteImporter {
			return game.Game
		}
	}
	return ""
}

func (s *Shell) bisectToolPage() *fairy.El {
	b := &s.tools.bisect
	t := kit.T()
	const prefix = "page.tools.mod_bisect."
	s.startMods()
	if !b.stateRead {
		b.stateRead = true
		s.background(func(context.Context) {
			snapshot := s.deps.Tools.BisectGetState()
			fairy.Go(func() {
				if s.mods.bisect == nil {
					s.mods.bisect = snapshot
				}
			})
		})
	}

	snapshot := s.mods.bisect
	status := tools.BisectStatus("idle")
	if snapshot != nil && snapshot.Status != "" {
		status = snapshot.Status
	}
	game := s.bisectGame()
	active := status == "scanning" || status == "round"
	busy := b.busy || b.recovering
	canStart := game != "" && !active && status != "reverting"
	canCancel := snapshot != nil && status != "idle" && status != "done" && status != "cancelled"
	canRecover := game != "" && (status == "idle" || status == "cancelled")

	var games []kit.Item
	for _, g := range s.mods.games {
		item := kit.Item{Value: g.Game, Label: g.Game}
		if deref(g.Importer) == nteImporter {
			item.Label += " (" + T(prefix+"nte_unsupported") + ")"
			item.Description = T(prefix + "nte_tooltip")
			item.Disabled = true
		}
		games = append(games, item)
	}
	placeholder := T(prefix + "select_game")
	if len(games) == 0 {
		placeholder = T(prefix + "no_games")
	}
	gameSelect := kit.Select("tools.bisect.game", game, games, func(value string) {
		if value != game {
			b.game, b.excludes = value, nil
		}
	}, kit.Placeholder(placeholder), kit.Disabled(active || busy || len(games) == 0)).MinW(224)

	controls := fairy.Row(
		gameSelect,
		kit.Button(T(prefix+"start"), s.startBisect, kit.Disabled(!canStart || busy)),
		kit.Button(T(prefix+"cancel"), func() {
			s.runBisect(&b.busy, map[string]any{"game": game}, "bisect-cancel", s.deps.Tools.BisectCancel, nil)
		}, kit.Destructive, kit.Disabled(!canCancel || busy)),
		kit.Button(T(prefix+"recover_disabled"), s.recoverBisect, kit.Secondary,
			kit.Disabled(!canRecover || busy), kit.Tooltip(T(prefix+"recover_disabled_hint"))),
		fairy.Text(T(prefix+"status."+string(status))).Font(monoFont).FontSize(12).LineHeight(16).
			Color(bisectStatusColor(status)),
	).Gap(8).Wrap().RowGap(8).Align(fairy.Center)

	preserve := true
	if s.settingsLoaded(setting.KeyGeneralBisectPreserveD3dx) {
		preserve = s.boolSetting(setting.KeyGeneralBisectPreserveD3dx)
	}
	content := fairy.Col(
		controls,
		s.bisectExcludeList(game, active || busy || game == ""),
		settingRow(T(prefix+"preserve_d3dx"), T(prefix+"preserve_d3dx_description"),
			kit.Switch(preserve, func(on bool) { s.setSetting(setting.KeyGeneralBisectPreserveD3dx, on) },
				kit.Disabled(active || busy)).Label(T(prefix+"preserve_d3dx"))).Pad(0),
	).Gap(12).PadEdges(4, 14, 4, 30).BorderEdges(0, 0, 0, 1, t.Foreground.Alpha(0.2))
	if snapshot != nil && snapshot.Error != nil && status != "done" {
		content.Child(
			fairy.Text(*snapshot.Error).FontSize(12).LineHeight(16).Color(t.Destructive).Wrap().Pad(8).Radius(6).
				Fill(t.Destructive.Alpha(0.1)),
		)
	}
	section := fairy.Col(fairy.Col(
		fairy.Text(T(prefix+"title")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
		fairy.Text(T(prefix+"description")).FontSize(12).LineHeight(16).Color(t.MutedForeground).Wrap(),
	).Gap(2).PadEdges(0, 4, 8, 4), content).Gap(4)

	page := toolPage(section)
	if card := s.bisectStatusCard(&b.busy, map[string]any{"game": game}, snapshot); card != nil {
		page.Child(card)
	}
	return page
}

func (s *Shell) bisectExcludeList(game string, disabled bool) *fairy.El {
	b := &s.tools.bisect
	t := kit.T()
	const prefix = "page.tools.mod_bisect."
	rows := fairy.Col().Gap(6)
	for _, row := range b.excludes {
		key := "tools.bisect.exclude." + itoa(row.id)
		rows.Child(fairy.Row(
			kit.TextField(row.value, func(value string) { row.value, row.failed = value, "" },
				kit.Placeholder(T(prefix+"exclude_paths_placeholder")), kit.Label(T(prefix+"exclude_paths")),
				kit.Disabled(disabled)).Key(key).Grow(1).MinW(0).
				OnBlur(func() { s.commitBisectExclude(row, game) }).
				OnSubmit(func(string) { s.commitBisectExclude(row, game) }),
			kit.IconButton(lucide.X, func() { b.removeExclude(row) }, kit.Ghost, kit.Small,
				kit.Label(T(prefix+"exclude_paths_remove")), kit.Disabled(disabled)),
		).Gap(6).Align(fairy.Center))
	}
	rows.Child(kit.Button(T(prefix+"exclude_paths_add"), b.addExclude, kit.Outline, kit.Small,
		kit.Icon(lucide.Plus), kit.Disabled(disabled)).AlignSelf(fairy.Start))
	return fairy.Col(
		fairy.Col(
			fairy.Text(T(prefix+"exclude_paths")).FontSize(14).Medium(),
			fairy.Text(T(prefix+"exclude_paths_description")).FontSize(12).Color(t.MutedForeground).Wrap(),
		).Gap(2),
		rows,
	).Gap(8)
}

// commitBisectExclude validates an edited exclude row and replaces its text with the path the backend resolved.
// An empty row is removed.
func (s *Shell) commitBisectExclude(row *bisectExclude, game string) {
	b := &s.tools.bisect
	const prefix = "page.tools.mod_bisect."
	trimmed := strings.TrimSpace(row.value)
	switch {
	case trimmed == "":
		b.removeExclude(row)
		return
	case row.committed != nil && row.value == *row.committed, row.failed == trimmed:
		return
	case game == "":
		s.toasts.Show(kit.ToastWarning, T(prefix+"exclude_no_game"), "")
		return
	}
	go func() {
		relative, err := s.deps.Tools.BisectValidateExcludePath(context.Background(), game, trimmed)
		fairy.Go(func() {
			if strings.TrimSpace(row.value) != trimmed {
				return
			}
			if err == nil && b.excludeTaken(row, relative) {
				err = errBisectExcludeDuplicate
			}
			if err != nil {
				row.failed = trimmed
				s.toasts.Show(kit.ToastWarning, bisectExcludeMessage(err), "")
				return
			}
			row.value, row.committed = relative, &relative
		})
	}()
}

func (b *bisectTool) excludeTaken(row *bisectExclude, relative string) bool {
	for _, other := range b.excludes {
		if other != row && other.committed != nil && platform.SamePathFold(*other.committed, relative) {
			return true
		}
	}
	return false
}

func bisectExcludeMessage(err error) string {
	if errors.Is(err, errBisectExcludeDuplicate) {
		return T("page.tools.mod_bisect.exclude_duplicate")
	}
	return T("page.tools.mod_bisect.exclude_invalid")
}

// startBisect validates the exclude rows not yet accepted, then starts the bisect with all of them.
func (s *Shell) startBisect() {
	b := &s.tools.bisect
	game := s.bisectGame()
	if game == "" {
		return
	}
	// The rows are read here on the UI thread; the validation below runs in the background.
	type pending struct {
		row       *bisectExclude
		text      string
		validated bool
	}
	var rows []pending
	for _, row := range b.excludes {
		rows = append(rows, pending{
			row: row, text: strings.TrimSpace(row.value),
			validated: row.committed != nil && *row.committed == row.value,
		})
	}
	fields := map[string]any{"game": game}
	s.runBisect(&b.busy, fields, "bisect-start", func(ctx context.Context) (tools.BisectSnapshot, error) {
		var paths []string
		resolved := map[*bisectExclude]string{}
		for _, p := range rows {
			if p.text == "" {
				continue
			}
			path := p.text
			if !p.validated {
				relative, err := s.deps.Tools.BisectValidateExcludePath(ctx, game, p.text)
				if err != nil {
					return tools.BisectSnapshot{}, errors.Join(errBisectExcludeInvalid, err)
				}
				path = relative
			}
			for _, other := range paths {
				if platform.SamePathFold(other, path) {
					return tools.BisectSnapshot{}, errBisectExcludeDuplicate
				}
			}
			paths = append(paths, path)
			resolved[p.row] = path
		}
		fairy.Go(func() {
			kept := b.excludes[:0]
			for _, row := range b.excludes {
				if path, ok := resolved[row]; ok {
					row.value, row.committed = path, &path
				}
				if strings.TrimSpace(row.value) != "" {
					kept = append(kept, row)
				}
			}
			b.excludes = kept
		})
		return s.deps.Tools.BisectStart(ctx, game, paths)
	}, func(err error) {
		if errors.Is(err, errBisectExcludeInvalid) || errors.Is(err, errBisectExcludeDuplicate) ||
			isExcludeValidationMessage(err.Error()) {
			s.toasts.Show(kit.ToastWarning, bisectExcludeMessage(err), "")
			return
		}
		s.modError(err, "bisect-start", fields)
		s.toasts.Show(kit.ToastError, err.Error(), "")
	})
}

func (s *Shell) recoverBisect() {
	b := &s.tools.bisect
	game := s.bisectGame()
	if game == "" || b.recovering {
		return
	}
	b.recovering = true
	s.background(func(ctx context.Context) {
		count, err := s.deps.Tools.BisectRecover(ctx, game)
		if err != nil {
			err = s.toolError(err, "bisect-recover", map[string]any{"game": game})
		}
		fairy.Go(func() {
			b.recovering = false
			const prefix = "page.tools.mod_bisect."
			switch {
			case err != nil:
				s.toasts.Show(kit.ToastError, err.Error(), "")
			case count > 0:
				s.toasts.Show(kit.ToastSuccess, T(prefix+"recover_disabled_success", "count", count), "")
			default:
				s.toasts.Show(kit.ToastInfo, T(prefix+"recover_disabled_none"), "")
			}
		})
	})
}
