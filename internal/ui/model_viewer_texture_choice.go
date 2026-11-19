package ui

import (
	"cmp"
	"context"
	"slices"
	"strconv"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerTextureChoiceGroup is the meshes of one part that offer sibling texture sets, with sources the sets any of
// them offers. A mesh that does not offer the source picked keeps the game's textures. chosen is the source picked
// for them, or "" while they keep the game's textures, and shown the source drawn now, which differs from chosen
// while the user previews another. game holds, by mesh, the textures each mesh loaded with, which the choices are
// applied over.
type viewerTextureChoiceGroup struct {
	component string
	sources   []string
	meshes    []int
	game      []modelviewer.ModelViewerTextureChoice
	chosen    string
	shown     string
}

// viewerTextureChoiceGame is the Select value of the game's textures. A section name cannot hold "]", and the
// Select reports "" for no highlighted row.
const viewerTextureChoiceGame = "]game"

func viewerTextureChoiceGroups(transport *modelviewer.ModelViewerTransport) []viewerTextureChoiceGroup {
	var groups []viewerTextureChoiceGroup
	index := map[string]int{}
	for i := range transport.Meshes {
		mesh := &transport.Meshes[i]
		if len(mesh.TextureChoices) == 0 {
			continue
		}
		g, ok := index[mesh.Component]
		if !ok {
			g = len(groups)
			index[mesh.Component] = g
			groups = append(groups, viewerTextureChoiceGroup{component: mesh.Component})
		}
		group := &groups[g]
		for _, choice := range mesh.TextureChoices {
			if !slices.Contains(group.sources, choice.Source) {
				group.sources = append(group.sources, choice.Source)
			}
		}
		group.meshes = append(group.meshes, i)
		group.game = append(group.game, modelviewer.TextureChoiceOf(mesh))
	}
	return groups
}

// gameTextures returns the textures scene mesh i loaded with when it offers texture choices.
func (s *viewerScene) gameTextures(i int) (modelviewer.ModelViewerTextureChoice, bool) {
	for _, group := range s.textureChoices {
		if k := slices.Index(group.meshes, i); k >= 0 {
			return group.game[k], true
		}
	}
	return modelviewer.ModelViewerTextureChoice{}, false
}

// applySavedTextureChoices puts the textures of the sources saved records, by part, in the meshes that offer them.
// It changes copies of the meshes, which the backend session shares with transport.
func applySavedTextureChoices(transport *modelviewer.ModelViewerTransport, saved map[string]string) {
	if len(saved) == 0 {
		return
	}
	transport.Meshes = slices.Clone(transport.Meshes)
	for i := range transport.Meshes {
		mesh := &transport.Meshes[i]
		source := saved[mesh.Component]
		if source == "" {
			continue
		}
		for j := range mesh.TextureChoices {
			if mesh.TextureChoices[j].Source == source {
				modelviewer.ApplyTextureChoice(mesh, modelviewer.TextureChoiceOf(mesh), &mesh.TextureChoices[j])
				break
			}
		}
	}
}

// chooseTexture draws the meshes of group g with the textures of source, or with the game's when source is "",
// and records the choice in the mod's nhd.json.
func (v *ModelViewer) chooseTexture(g int, source string) {
	if v.applyTextureChoice(g, source) {
		v.saveTextureChoice(v.scene.textureChoices[g].component, source)
	}
}

func (v *ModelViewer) applyTextureChoice(g int, source string) bool {
	scene := v.scene
	if scene == nil || g >= len(scene.textureChoices) {
		return false
	}
	scene.textureChoices[g].chosen = source
	v.showTextureChoice(g, source)
	return true
}

// previewTextureChoice draws group g with the source of the Select row value while it is highlighted, and with the
// chosen source again when value is "".
func (v *ModelViewer) previewTextureChoice(g int, value string) {
	if v.scene == nil || g >= len(v.scene.textureChoices) {
		return
	}
	source := v.scene.textureChoices[g].chosen
	if value != "" {
		source = textureChoiceSource(value)
	}
	v.showTextureChoice(g, source)
}

// endTexturePreviews draws every group with its chosen source, for when the popover closes with a row highlighted.
func (v *ModelViewer) endTexturePreviews() {
	if v.scene == nil {
		return
	}
	for g := range v.scene.textureChoices {
		v.previewTextureChoice(g, "")
	}
}

func textureChoiceSource(value string) string {
	if value == viewerTextureChoiceGame {
		return ""
	}
	return value
}

func (v *ModelViewer) showTextureChoice(g int, source string) {
	scene := v.scene
	group := &scene.textureChoices[g]
	if group.shown == source {
		return
	}
	group.shown = source
	for k, i := range group.meshes {
		mesh := &scene.transport.Meshes[i]
		var choice *modelviewer.ModelViewerTextureChoice
		for j := range mesh.TextureChoices {
			if mesh.TextureChoices[j].Source == source {
				choice = &mesh.TextureChoices[j]
			}
		}
		modelviewer.ApplyTextureChoice(mesh, group.game[k], choice)
		modelviewer.ApplyTextureChoice(&scene.meshes[i].info, group.game[k], choice)
	}
	v.evaluate()
}

// storesTextureChoices reports whether the viewer shows a mod folder whose nhd.json can hold its choices.
func (v *ModelViewer) storesTextureChoices() bool {
	return v.blender == nil && v.shell != nil && v.shell.deps.Mod != nil
}

// loadTextureChoices applies the choices the mod's nhd.json records for the parts of scene.
func (v *ModelViewer) loadTextureChoices(scene *viewerScene) {
	if len(scene.textureChoices) == 0 || !v.storesTextureChoices() {
		return
	}
	mods, path := v.shell.deps.Mod, v.path
	v.shell.backgroundRead(func(ctx context.Context) {
		saved, err := mods.GetViewerTextureChoices(ctx, path)
		if err != nil || len(saved) == 0 {
			return
		}
		fairy.Go(func() {
			if v.scene != scene {
				return
			}
			for g, group := range scene.textureChoices {
				if source := saved[group.component]; group.chosen == "" && slices.Contains(group.sources, source) {
					v.applyTextureChoice(g, source)
				}
			}
		})
	})
}

// saveTextureChoice queues the write of part's choice. Writes run one at a time, so the last choice of a part is
// the one nhd.json keeps.
func (v *ModelViewer) saveTextureChoice(part, source string) {
	if !v.storesTextureChoices() {
		return
	}
	if v.choicePending == nil {
		v.choicePending = map[string]string{}
	}
	v.choicePending[part] = source
	if !v.choiceSaving {
		v.flushTextureChoices()
	}
}

func (v *ModelViewer) flushTextureChoices() {
	for part, source := range v.choicePending {
		delete(v.choicePending, part)
		v.choiceSaving = true
		mods, path, toasts := v.shell.deps.Mod, v.path, v.toasts
		v.shell.background(func(ctx context.Context) {
			err := mods.SetViewerTextureChoice(ctx, path, part, source)
			fairy.Go(func() {
				v.choiceSaving = false
				if err != nil && !infra.IsCancellationError(err) {
					toasts.Show(kit.ToastError, T("page.tools.model_viewer.texture_choice_save_failed"), err.Error())
				}
				v.flushTextureChoices()
			})
		})
		return
	}
}

// viewerTextureInfoChipWidth is the width of the texture chip, which shows the number of parts drawn with the
// game's textures when there are any.
func viewerTextureInfoChipWidth(gameParts int) float32 {
	if gameParts > 0 {
		return viewerGameTextureChipWidth
	}
	return 2*viewerStatusChipPadX + viewerStatusIconSize
}

// textureInfoChip shows the number of parts drawn without a texture and opens a popover that switches their
// highlight and picks the textures of parts whose INI leaves them open.
func (v *ModelViewer) textureInfoChip(t kit.Theme, status viewerStatus) *fairy.El {
	label := T("page.tools.model_viewer.texture_info")
	if status.gameParts > 0 {
		label = T("page.tools.model_viewer.game_textures", "count", status.gameParts)
	}
	chip := fairy.Row(fairy.Icon(lucide.Info).Size(viewerStatusIconSize, viewerStatusIconSize).Shrink(0)).
		Key("model-viewer.game-textures").W(viewerTextureInfoChipWidth(status.gameParts)).Shrink(0).
		Gap(viewerStatusChipGap).Align(fairy.Center).H(viewerStatusChipHeight).PadXY(viewerStatusChipPadX, 0).
		Radius(viewerStatusChipHeight/2).HoverFill(hoverFill(&t)).Cursor(fairy.CursorPointer).
		Role(fairy.RoleButton).Expanded(v.textureInfoOpen).Label(label).Focusable().
		FocusRing(3, t.Ring.Alpha(0.5)).Tooltip(label).
		Animate(viewerAnimationBarMotion).
		OnClick(func() {
			v.textureInfoOpen = !v.textureInfoOpen
			if !v.textureInfoOpen {
				v.endTexturePreviews()
			}
		})
	if status.gameParts > 0 {
		chip.Child(viewerStatusText(strconv.Itoa(status.gameParts)).W(viewerStatusCountWidth))
	}
	if v.highlightGameTextures {
		amber := fairy.Hex(viewerGameTextureHighlight)
		chip.Color(amber).Fill(amber.Alpha(0.12))
	}
	if !v.textureInfoOpen {
		return chip
	}
	return chip.Popover(v.textureInfoPopover(t, status), fairy.PopoverOptions{
		Side: fairy.SideTop, Align: fairy.AlignCenter, Gap: 8,
		OnDismiss: func() {
			v.textureInfoOpen = false
			v.endTexturePreviews()
		},
	})
}

func (v *ModelViewer) textureInfoPopover(t kit.Theme, status viewerStatus) *fairy.El {
	muted := func(text string) *fairy.El {
		return fairy.Text(text).FontSize(12).LineHeight(18).Color(t.MutedForeground).Wrap()
	}
	body := fairy.Col().Gap(10)
	if status.gameParts > 0 {
		label := T("page.tools.model_viewer.game_textures", "count", status.gameParts)
		body.Child(fairy.Col(
			fairy.Col(
				fairy.Text(label).FontSize(13).LineHeight(20).Medium().Wrap(),
				muted(T("page.tools.model_viewer.game_textures_hint")),
			).Gap(2),
			kit.Switch(v.highlightGameTextures, func(on bool) {
				v.highlightGameTextures = on
				v.revision++
			}, kit.Label(T("page.tools.model_viewer.game_textures_highlight"))),
		).Gap(10))
	}
	if groups := v.scene.textureChoices; len(groups) > 0 {
		if status.gameParts > 0 {
			body.Child(kit.Separator())
		}
		body.Child(fairy.Col(
			fairy.Text(T("page.tools.model_viewer.texture_choices")).FontSize(13).LineHeight(20).Medium(),
			muted(T("page.tools.model_viewer.texture_choices_hint")),
		).Gap(2))
		compare := v.shell.nameCompare()
		for g, group := range groups {
			items := []kit.Item{
				{Value: viewerTextureChoiceGame, Label: T("page.tools.model_viewer.texture_choice_game")},
			}
			sources := slices.Clone(group.sources)
			slices.SortStableFunc(sources, compare)
			for _, source := range sources {
				items = append(items, kit.Item{Value: source, Label: source})
			}
			id := "model-viewer.texture-choice." + strconv.Itoa(g)
			body.Child(fairy.Row(
				fairy.Text(group.component).FontSize(12).LineHeight(18).Medium().Ellipsis().Grow(1).MinW(0),
				kit.Select(id, cmp.Or(group.chosen, viewerTextureChoiceGame), items,
					func(value string) { v.chooseTexture(g, textureChoiceSource(value)) },
					kit.OnHighlight(func(value string) { v.previewTextureChoice(g, value) }),
					kit.Width(150), kit.Label(group.component)),
			).Gap(8).Align(fairy.Center).Key(id))
		}
	}
	return t.Surface(body, t.Popover).W(300).MaxH(max(v.height-24, 160)).ScrollY().Pad(12).
		Radius(t.RadiusLg()).Ring(1, t.Foreground.Alpha(0.1)).Color(t.PopoverForeground).
		Label(T("page.tools.model_viewer.texture_info"))
}
