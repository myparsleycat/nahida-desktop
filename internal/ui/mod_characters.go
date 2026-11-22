package ui

import (
	"context"
	"slices"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/mod/character"
	"nahida.live/desktop/internal/platform"
)

const characterDialogPrefix = "page.mod.dialog.character."

// characterDraft is the dialog that assigns a catalog character to a sidebar folder.
type characterDraft struct {
	folder mod.FolderGroup
	game   string
	id     string
	// current is the folder's stored assignment, once loaded.
	current *mod.CharacterAssignment
	loaded  bool
	busy    bool
}

func (s *Shell) openCharacterDraft(folder mod.FolderGroup) {
	games := character.Games()
	game := s.currentImporter()
	if !slices.Contains(games, game) {
		game = games[0]
	}
	d := &characterDraft{folder: folder, game: game}
	s.mods.characterDraft = d
	s.backgroundRead(func(ctx context.Context) {
		current, err := s.deps.Mod.FolderCharacter(ctx, folder.Path)
		fairy.Go(func() {
			d.loaded = true
			if err != nil {
				s.toasts.Show(kit.ToastError, T(characterDialogPrefix+"#.failed"), "")
				return
			}
			if current != nil && d.id == "" {
				d.current = current
				d.game, d.id = current.Game, current.ID
			}
		})
	})
}

func characterItems(game string, compare func(a, b string) int) []kit.Item {
	lang := catalog.Language()
	characters := character.List(game)
	slices.SortFunc(characters, func(a, b character.Character) int {
		if c := compare(a.Name(lang), b.Name(lang)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	items := make([]kit.Item, len(characters))
	for i, c := range characters {
		// The combobox filters by label, so the English name stays searchable in every language.
		label := c.Name(lang)
		if english := c.Name("en"); english != label {
			label += " · " + english
		}
		items[i] = kit.Item{Value: c.ID, Label: label}
	}
	return items
}

func (s *Shell) saveFolderCharacter(d *characterDraft, assignment *mod.CharacterAssignment) {
	if d.busy {
		return
	}
	d.busy = true
	path := d.folder.Path
	s.background(func(ctx context.Context) {
		err := s.deps.Mod.SetFolderCharacter(ctx, path, assignment)
		fairy.Go(func() {
			d.busy = false
			if err != nil {
				s.toasts.Show(kit.ToastError, T(characterDialogPrefix+"#.failed"), "")
				return
			}
			if s.mods.characterDraft == d {
				s.mods.characterDraft = nil
			}
			for v := range s.viewers {
				if v.canLoadFolderCharacter() && platform.SameOrChildPath(path, v.path) {
					v.loadCharacter()
				}
			}
		})
	})
}

func (s *Shell) characterDialog(cx *fairy.Cx) {
	m := &s.mods
	d := m.characterDraft
	if d == nil {
		return
	}
	const prefix = characterDialogPrefix
	disabled := d.busy || !d.loaded
	body := fairy.Col(s.characterFields("mod.character", &d.game, &d.id, disabled)...).Gap(16)

	close := func() {
		if !d.busy {
			m.characterDraft = nil
		}
	}
	unchanged := d.current != nil && d.current.Game == d.game && d.current.ID == d.id
	var leading []*fairy.El
	if d.current != nil {
		leading = append(leading, kit.Button(T("g.clear"), func() { s.saveFolderCharacter(d, nil) },
			kit.Outline, kit.Disabled(d.busy)))
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:          T(prefix + "title"),
		Description:    T(prefix+"description", "name", d.folder.Name),
		Body:           body,
		LeadingActions: leading,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T("g.save"), func() {
				s.saveFolderCharacter(d, &mod.CharacterAssignment{Game: d.game, ID: d.id})
			}, kit.Loading(d.busy), kit.Disabled(disabled || d.id == "" || unchanged)),
		},
		OnClose: close,
		Key:     "mod.character",
	})
}

func (s *Shell) characterFields(key string, game, id *string, disabled bool) []*fairy.El {
	const prefix = characterDialogPrefix
	games := character.Games()
	gameItems := make([]kit.Item, len(games))
	for i, name := range games {
		gameItems[i] = kit.Item{Value: name, Label: T(prefix + "games." + name)}
	}
	return []*fairy.El{
		modDialogField(T(prefix+"game"), kit.Select(key+".game", *game, gameItems, func(value string) {
			*game, *id = value, ""
		}, kit.Placeholder(T(prefix+"select-game")), kit.Disabled(disabled))),
		modDialogField(
			T(prefix+"character"),
			kit.Combobox(key+".id."+*game, *id, characterItems(*game, s.nameCompare()),
				func(value string) { *id = value }, kit.Placeholder(T(prefix+"search-placeholder")),
				kit.Disabled(disabled || *game == "")),
		),
	}
}
