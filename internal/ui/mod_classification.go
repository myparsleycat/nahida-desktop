package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
)

const classificationNameMax = 64

// classificationEditor is the dialog that creates, renames and deletes the classifications of the selected game.
// The draft is the classification being edited; a nil id is a new one.
type classificationEditor struct {
	game     string
	id       *string
	name     string
	groups   []classificationGroupDraft
	nextKey  int
	saving   bool
	deleting bool
}

// classificationGroupDraft keeps key stable while a group is renamed or moved; id is empty until it is saved.
type classificationGroupDraft struct {
	key  int
	id   string
	name string
}

func (e *classificationEditor) busy() bool { return e.saving || e.deleting }

func (e *classificationEditor) load(c *mod.Classification) {
	e.id, e.name, e.groups = nil, "", nil
	if c == nil {
		return
	}
	id := c.ID
	e.id, e.name = &id, c.Name
	e.groups = lo.Map(c.Groups, func(g mod.ClassificationGroup, _ int) classificationGroupDraft {
		e.nextKey++
		return classificationGroupDraft{key: e.nextKey, id: g.ID, name: g.Name}
	})
}

func (e *classificationEditor) canSave() bool {
	return strings.TrimSpace(e.name) != "" &&
		lo.EveryBy(e.groups, func(g classificationGroupDraft) bool { return strings.TrimSpace(g.name) != "" })
}

func (s *Shell) openClassificationEditor() {
	m := &s.mods
	if m.game == "" {
		return
	}
	e := &classificationEditor{game: m.game}
	current := s.activeClassification()
	if current == nil && len(m.classifications) > 0 {
		current = &m.classifications[0]
	}
	e.load(current)
	m.classificationEditor = e
}

func (s *Shell) saveClassification(e *classificationEditor) {
	if e.busy() || !e.canSave() {
		return
	}
	e.saving = true
	id, name := e.id, e.name
	groups := lo.Map(e.groups, func(g classificationGroupDraft, _ int) mod.ClassificationGroup {
		return mod.ClassificationGroup{ID: g.id, Name: g.name}
	})
	s.background(func(ctx context.Context) {
		saved, err := s.deps.Mod.SaveClassification(ctx, e.game, id, name, groups)
		fairy.Go(func() {
			e.saving = false
			if err != nil {
				s.classificationFailed(err, "save-classification", e.game)
				return
			}
			e.load(&saved)
			s.loadClassifications()
		})
	})
}

func (s *Shell) deleteClassification(e *classificationEditor) {
	if e.busy() || e.id == nil {
		return
	}
	id := *e.id
	s.Confirm(Confirmation{
		Title:        T("page.mod.dialog.classification.delete-title"),
		Description:  T("page.mod.dialog.classification.delete-description", "name", e.name),
		ConfirmLabel: T("g.delete"),
		OnResult: func(confirmed bool) {
			if !confirmed || e.busy() {
				return
			}
			e.deleting = true
			s.background(func(ctx context.Context) {
				err := s.deps.Mod.DeleteClassification(ctx, id)
				fairy.Go(func() {
					e.deleting = false
					if err != nil {
						s.classificationFailed(err, "delete-classification", e.game)
						return
					}
					next, _ := lo.Find(s.mods.classifications, func(c mod.Classification) bool { return c.ID != id })
					if next.ID == "" {
						e.load(nil)
					} else {
						e.load(&next)
					}
					s.loadClassifications()
				})
			})
		},
	})
}

// classificationFailed shows the error, and a missing classification reads the list again, as it is stale.
func (s *Shell) classificationFailed(err error, operation, game string) {
	if !strings.Contains(err.Error(), "CLASSIFICATION") {
		s.modError(err, operation, map[string]any{"game": game})
	}
	s.showClassificationError(err)
	if strings.Contains(err.Error(), "CLASSIFICATION_NOT_FOUND") {
		s.loadClassifications()
	}
}

func (s *Shell) classificationDialog(cx *fairy.Cx) {
	t := kit.T()
	m := &s.mods
	e := m.classificationEditor
	if e == nil {
		return
	}
	if e.game != m.game {
		m.classificationEditor = nil
		return
	}
	const prefix = "page.mod.dialog.classification."
	busy := e.busy()

	list := fairy.Col().Gap(4)
	for i, c := range m.classifications {
		selected := e.id != nil && *e.id == c.ID
		button := kit.Button("", func() { e.load(&c) }, kit.Ghost, kit.Disabled(busy)).
			Child(fairy.Text(c.Name).Ellipsis().MinW(0)).Label(c.Name).Justify(fairy.JustifyStart)
		if selected {
			button.Fill(t.Accent)
		}
		if i == 0 {
			button.AutoFocus()
		}
		list.Child(button.Key("mod.classification:" + c.ID))
	}
	newButton := kit.Button(T(prefix+"new"), func() { e.load(nil) }, kit.Outline, kit.Icon(lucide.Plus),
		kit.Disabled(busy))
	if e.id == nil {
		newButton.Border(1, t.Primary)
	}
	if len(m.classifications) == 0 {
		newButton.AutoFocus()
	}
	sidebar := fairy.Col(fairy.Col(list).MaxH(288).ScrollY(), newButton).W(160).Shrink(0).Gap(4)

	nameField := kit.TextField(e.name, func(v string) { e.name = limitRunes(v, classificationNameMax) },
		kit.Placeholder(T(prefix+"name-placeholder")), kit.Disabled(busy))

	groups := fairy.Col().Gap(8)
	move := func(i, direction int) {
		e.groups[i], e.groups[i+direction] = e.groups[i+direction], e.groups[i]
	}
	for i, g := range e.groups {
		key := g.key
		edit := func(v string) {
			for j := range e.groups {
				if e.groups[j].key == key {
					e.groups[j].name = limitRunes(v, classificationNameMax)
				}
			}
		}
		groups.Child(fairy.Row(
			kit.TextField(g.name, edit, kit.Placeholder(T(prefix+"group-placeholder")), kit.Disabled(busy)).
				Grow(1).MinW(0),
			kit.IconButton(lucide.ArrowUp, func() { move(i, -1) }, kit.Ghost, kit.Disabled(busy || i == 0),
				kit.Tooltip(T(prefix+"move-up"))).Label(T(prefix+"move-up")),
			kit.IconButton(lucide.ArrowDown, func() { move(i, 1) }, kit.Ghost,
				kit.Disabled(busy || i == len(e.groups)-1), kit.Tooltip(T(prefix+"move-down"))).
				Label(T(prefix+"move-down")),
			kit.IconButton(lucide.Trash, func() {
				e.groups = lo.Reject(
					e.groups,
					func(item classificationGroupDraft, _ int) bool { return item.key == key },
				)
			}, kit.Ghost, kit.Disabled(busy), kit.Tooltip(T("g.delete"))).Label(T("g.delete")),
		).Key("mod.classification.group:" + strconv.Itoa(key)).Gap(4).Align(fairy.Center))
	}
	addGroup := kit.Button(T(prefix+"add-group"), func() {
		e.nextKey++
		e.groups = append(e.groups, classificationGroupDraft{key: e.nextKey})
	}, kit.Outline, kit.Icon(lucide.Plus), kit.Disabled(busy))

	editor := fairy.Col(
		modDialogField(T(prefix+"name-label"), nameField),
		modDialogField(T(prefix+"groups-label"), fairy.Col(fairy.Col(groups).MaxH(224).ScrollY(), addGroup).Gap(8)),
	).Grow(1).MinW(0).Gap(16)

	close := func() {
		if !busy {
			m.classificationEditor = nil
		}
	}
	kit.ShowDialog(cx, kit.DialogProps{
		Title:       T(prefix + "title"),
		Description: T(prefix + "description"),
		Body:        fairy.Row(sidebar, editor).MinH(288).Gap(16),
		Width:       576,
		Actions: []*fairy.El{
			kit.Button(T("g.delete"), func() { s.deleteClassification(e) }, kit.Destructive,
				kit.Disabled(busy || e.id == nil), kit.Loading(e.deleting)),
			// Holds the delete button at the left edge, away from the dialog's own actions.
			fairy.Box().Grow(1000),
			kit.Button(T("g.close"), close, kit.Outline),
			kit.Button(T("g.save"), func() { s.saveClassification(e) }, kit.Loading(e.saving),
				kit.Disabled(busy || !e.canSave())),
		},
		OnClose: close,
		Key:     "mod.classifications",
	})
}
