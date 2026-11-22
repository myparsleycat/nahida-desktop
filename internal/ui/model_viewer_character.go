package ui

import (
	"context"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/mod/character"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
)

const viewerCharacterPrefix = "page.tools.model_viewer.dialog.character."

type viewerCharacter struct {
	game string
	character.Character
	characterToon
}

func (c *viewerCharacter) styleKey() string { return c.game + "/" + c.ID }

type viewerCharacterSelection struct {
	saved   mod.ViewerCharacter
	request uint64
	loading bool
	failed  bool
	draft   *viewerCharacterDraft
}

type viewerCharacterDraft struct {
	game   string
	id     string
	busy   bool
	failed bool
}

func (v *ModelViewer) canLoadFolderCharacter() bool {
	return !v.closed && v.scene != nil && v.blender == nil && v.shell != nil && v.shell.deps.Mod != nil
}

func (v *ModelViewer) characterSelectionAvailable() bool {
	return v.tool == nil && v.canLoadFolderCharacter()
}

type viewerCharacterLoad struct {
	saved mod.ViewerCharacter
	next  *viewerCharacter
	err   error
}

// installCharacter applies the character resolved with the scene, so the first frame draws with its toon style.
func (v *ModelViewer) installCharacter(scene *viewerScene) {
	if scene.character == nil || !v.canLoadFolderCharacter() {
		v.loadCharacter()
		return
	}
	v.finishCharacter(*scene.character)
}

func (v *ModelViewer) loadCharacter() {
	if b := v.blender; b != nil {
		v.applyCharacter(v.catalogCharacter(b.importer, b.character))
		return
	}
	if !v.canLoadFolderCharacter() {
		return
	}
	selection := &v.characterSelection
	selection.request++
	selection.loading = true
	request, scene, ctx, post := selection.request, v.scene, v.ctx, v.poster()
	go func() {
		load := v.resolveCharacter(ctx, scene.transport.OpenedModPath)
		post(func() {
			if v.closed || v.scene != scene || selection.request != request {
				return
			}
			v.finishCharacter(*load)
		})
	}()
}

// resolveCharacter reads the mod's character off the UI thread. It returns nil when the viewer has no mod service.
func (v *ModelViewer) resolveCharacter(ctx context.Context, openedPath string) *viewerCharacterLoad {
	if v.shell == nil || v.shell.deps.Mod == nil {
		return nil
	}
	saved, err := v.shell.deps.Mod.GetViewerCharacter(ctx, v.path, openedPath)
	load := &viewerCharacterLoad{saved: saved, err: err}
	assignment := saved.Override
	if assignment == nil && saved.Inherited != nil {
		assignment = &saved.Inherited.CharacterAssignment
	}
	if assignment != nil && ctx.Err() == nil {
		load.next = v.catalogCharacter(assignment.Game, assignment.ID)
	}
	return load
}

func (v *ModelViewer) finishCharacter(load viewerCharacterLoad) {
	selection := &v.characterSelection
	selection.saved, selection.loading = load.saved, false
	selection.failed = load.err != nil || !load.saved.DirectoryMatched
	if load.next != nil || load.err == nil {
		v.applyCharacter(load.next)
	}
}

func (v *ModelViewer) catalogCharacter(game, id string) *viewerCharacter {
	entry, ok := character.Lookup(game, id)
	if !ok {
		return nil
	}
	toon, err := loadCharacterToon(game, id)
	if err != nil {
		_ = infra.ReportError(v.log, err, "ModelViewer.loadCharacter", infra.Diagnostic{
			Operation: "load-character-toon", Fields: map[string]any{"game": game, "character": id},
		})
	}
	return &viewerCharacter{game: game, Character: entry, characterToon: toon}
}

func (v *ModelViewer) applyCharacter(next *viewerCharacter) {
	if (v.character == nil) == (next == nil) && (next == nil || v.character.styleKey() == next.styleKey()) {
		return
	}
	v.flushToonDraft()
	v.stopMotion()
	v.cancelOriginal()
	if v.scene != nil {
		v.dropOriginal(v.scene)
	}
	v.downloads.reset()
	v.motionFailed, v.originalFailed = viewerCharacterKey{}, viewerCharacterKey{}
	v.expression = viewerExpression{}
	v.character = next
	v.revision++
}

func (v *ModelViewer) openCharacterSelection() {
	selection := &v.characterSelection
	if !v.characterSelectionAvailable() || selection.loading || selection.draft != nil {
		return
	}
	d := &viewerCharacterDraft{game: viewerToonProfiles[v.toonProfile()].importer}
	if c := v.character; c != nil {
		d.game, d.id = c.game, c.ID
	}
	selection.draft = d
}

func (v *ModelViewer) characterSelectionDialog(cx *fairy.Cx) {
	selection := &v.characterSelection
	d := selection.draft
	if d == nil {
		return
	}
	const key = "model-viewer.character"
	close := func() {
		if !d.busy {
			selection.draft = nil
		}
	}
	current := T(viewerCharacterPrefix + "current_none")
	if c := v.character; c != nil {
		name := c.Name(catalog.Language())
		switch {
		case selection.saved.Override != nil:
			current = T(viewerCharacterPrefix+"current_direct", "name", name)
		case selection.saved.Inherited != nil:
			folder := modname.DisplayName(selection.saved.Inherited.Folder)
			current = T(viewerCharacterPrefix+"current_inherited", "name", name, "folder", folder)
		default:
			current = T(viewerCharacterPrefix+"current", "name", name)
		}
	}
	disabled := d.busy || selection.loading || !selection.saved.DirectoryMatched
	body := fairy.Col(v.shell.characterFields(key, &d.game, &d.id, disabled)...).Gap(16)
	if d.failed {
		body.Child(fairy.Text(T(viewerCharacterPrefix + "save_failed")).Color(kit.T().Destructive).Wrap())
	} else if selection.failed {
		body.Child(fairy.Text(T(viewerCharacterPrefix + "load_failed")).Color(kit.T().Destructive).Wrap())
	}
	var leading []*fairy.El
	if selection.saved.HasOverride {
		leading = append(leading, kit.Button(T(viewerCharacterPrefix+"clear"), func() { v.saveCharacterSelection(nil) },
			kit.Outline, kit.Disabled(disabled)))
	}
	assignment := mod.CharacterAssignment{Game: d.game, ID: d.id}
	unchanged := selection.saved.Override != nil && *selection.saved.Override == assignment
	kit.ShowDialog(cx, kit.DialogProps{
		Title:          T(viewerCharacterPrefix + "title"),
		Description:    current,
		Body:           body,
		LeadingActions: leading,
		Actions: []*fairy.El{
			kit.Button(T("g.cancel"), close, kit.Outline, kit.Disabled(d.busy)),
			kit.Button(T("g.save"), func() { v.saveCharacterSelection(&assignment) },
				kit.Loading(d.busy), kit.Disabled(disabled || d.id == "" || unchanged)),
		},
		OnClose: close,
		Key:     key,
	})
}

func (v *ModelViewer) saveCharacterSelection(assignment *mod.CharacterAssignment) {
	selection := &v.characterSelection
	d := selection.draft
	if !v.characterSelectionAvailable() || d == nil || d.busy || selection.loading ||
		!selection.saved.DirectoryMatched {
		return
	}
	d.busy, d.failed = true, false
	s, ctx, path, openedPath := v.shell, v.ctx, v.path, v.scene.transport.OpenedModPath
	s.background(func(context.Context) {
		err := s.deps.Mod.SetViewerCharacter(ctx, path, openedPath, assignment)
		fairy.Go(func() {
			if !v.closed && selection.draft == d {
				d.busy, d.failed = false, err != nil
				if err == nil {
					selection.draft = nil
				}
			}
			if err != nil {
				return
			}
			for viewer := range s.viewers {
				if viewer.canLoadFolderCharacter() &&
					platform.SamePathFold(viewer.scene.transport.OpenedModPath, openedPath) {
					viewer.loadCharacter()
				}
			}
		})
	})
}
