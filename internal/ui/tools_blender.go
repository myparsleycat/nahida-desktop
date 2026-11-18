package ui

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/blenderlink"
	"nahida.live/desktop/internal/mod/character"
	"nahida.live/desktop/internal/setting"
)

// blenderLinkTool holds the newest state of the Blender live link server; read is false until the page has read
// it once, after which blenderlink.EventState keeps it current.
type blenderLinkTool struct {
	state blenderlink.State
	read  bool
	// choices holds the game and character chosen for each connection's scene.
	choices map[uint64]*blenderChoice
	addon   blenderAddonTool
}

type blenderChoice struct {
	importer, character string
	// guessed is the importer the character was last guessed for.
	guessed string
}

// blenderGames are the importers whose mods XXMI-Tools exports.
var blenderGames = []string{"GIMI", "SRMI", "ZZMI"}

func (s *Shell) blenderLinkManager() *blenderlink.Manager {
	if s.deps.BlenderLink == nil {
		return nil
	}
	return s.deps.BlenderLink()
}

func (s *Shell) blenderLinkToolPage(cx *fairy.Cx) *fairy.El {
	b := &s.tools.blenderLink
	t := kit.T()
	const prefix = "page.tools.blender_link."
	if !b.read {
		if m := s.blenderLinkManager(); m != nil {
			b.state, b.read = m.State(), true
		}
	}

	loaded := s.settingsLoaded(setting.KeyModelViewerBlenderLink)
	title := T(prefix + "title")
	page := toolPage(toolHeading(title, T(prefix+"description"),
		kit.Switch(s.boolSetting(setting.KeyModelViewerBlenderLink),
			func(on bool) { s.setSetting(setting.KeyModelViewerBlenderLink, on) },
			kit.Disabled(!loaded || s.settingSaving(setting.KeyModelViewerBlenderLink))).Label(title)))

	var rows []*fairy.El
	switch {
	case !b.state.Listening:
		rows = append(rows, fairy.Text(T(prefix+"not_running")).FontSize(14).LineHeight(20).
			Color(t.MutedForeground).Wrap())
	case len(b.state.Links) == 0:
		rows = append(rows, fairy.Text(T(prefix+"empty")).FontSize(14).LineHeight(20).
			Color(t.MutedForeground).Wrap())
	}
	if b.state.Listening {
		for _, link := range b.state.Links {
			rows = append(rows, s.blenderLinkRow(link))
		}
	}
	// The viewer opens over this page; the mod screen shows its own.
	s.modelViewerDialog(cx)
	return page.Child(s.blenderAddonSection(), toolSection(T(prefix+"connections"), rows...))
}

func (s *Shell) blenderLinkRow(link blenderlink.LinkState) *fairy.El {
	t := kit.T()
	const prefix = "page.tools.blender_link."
	b := &s.tools.blenderLink
	scenes := s.deps.Tools.BlenderLink()
	sceneGame := scenes.SceneGame(link.Session)
	if b.choices == nil {
		b.choices = map[uint64]*blenderChoice{}
	}
	choice := b.choices[link.Session]
	if choice == nil {
		choice = &blenderChoice{importer: sceneGame}
		b.choices[link.Session] = choice
	}
	if sceneGame != "" && choice.importer != sceneGame {
		choice.importer, choice.character = sceneGame, ""
	}
	if choice.importer != "" && choice.guessed != choice.importer {
		choice.guessed = choice.importer
		if choice.character == "" {
			choice.character = guessBlenderCharacter(choice.importer, scenes.SceneNames(link.Session))
		}
	}

	name := "Blender " + link.BlenderVersion
	if choice.importer != "" {
		name += " · " + choice.importer
	}
	info := fairy.Col(
		fairy.Text(name).FontSize(14).LineHeight(20).Medium(),
		fairy.Text(T(prefix+"objects", "count", strconv.Itoa(link.Objects))).FontSize(13).LineHeight(18).
			Color(t.MutedForeground),
	).Gap(2).Grow(1).MinW(0)
	if blenderAddonOlder(link.AddonVersion) {
		series := blenderlink.Series(link.BlenderVersion)
		warning := fairy.Row(
			fairy.Icon(lucide.TriangleAlert).Size(14, 14).Color(t.MutedForeground).Shrink(0),
			fairy.Text(T(blenderAddonPrefix+"link_outdated", "version", link.AddonVersion,
				"bundled", blenderlink.AddonVersion())).
				FontSize(13).LineHeight(18).Color(t.MutedForeground).Wrap().MinW(0),
		).Gap(6).Align(fairy.Center)
		if series != "" && s.blenderAddonUpdatable(series) {
			warning.Child(kit.Button(T(blenderAddonPrefix+"update"), func() { s.installBlenderAddon(series) },
				kit.Small, kit.Outline, kit.Loading(b.addon.busy == series), kit.Disabled(b.addon.busy != "")).
				Shrink(0))
		}
		info.Child(warning)
	}
	for _, message := range link.Errors {
		info.Child(fairy.Row(
			fairy.Icon(lucide.TriangleAlert).Size(14, 14).Color(t.Destructive).Shrink(0),
			fairy.Text(message).FontSize(13).LineHeight(18).Color(t.Destructive).Wrap().MinW(0),
		).Gap(6).Align(fairy.Start))
	}

	id := strconv.FormatUint(link.Session, 10)
	games := make([]kit.Item, len(blenderGames))
	for i, game := range blenderGames {
		games[i] = kit.Item{Value: game, Label: game}
	}
	gameSelect := kit.Select("tools.blender_link.game."+id, choice.importer, games, func(game string) {
		if game != choice.importer {
			choice.importer, choice.character = game, ""
		}
	}, kit.Placeholder(T(prefix+"game")), kit.Disabled(sceneGame != "")).MinW(120)
	characterSelect := kit.Combobox("tools.blender_link.character."+id, choice.character,
		characterItems(choice.importer), func(character string) { choice.character = character },
		kit.Placeholder(T(prefix+"character")), kit.Disabled(choice.importer == "")).MinW(224)

	session, label := link.Session, scenes.SceneName(link.Session)
	opening := s.blenderViewerOpening(session)
	open := kit.Button(T(prefix+"open_viewer"), func() {
		s.openBlenderViewer(label, viewerBlenderSource{
			link: session, importer: choice.importer, character: choice.character,
		})
		if m := s.blenderLinkManager(); m != nil {
			m.RequestResync(session)
		}
	}, kit.Small, kit.Icon(lucide.Box), kit.Loading(opening),
		kit.Disabled(opening || link.Objects == 0 || choice.importer == ""))
	controls := fairy.Row(gameSelect, characterSelect, open.Shrink(0)).Gap(8).Align(fairy.Center).Wrap()
	return fairy.Col(info, controls).Gap(8)
}

// guessBlenderCharacter picks the character whose ID, alias or English name the scene's component names start
// with, preferring the longest match, as XXMI mods name their components (CitlaliBody, EllenHair).
func guessBlenderCharacter(game string, names []string) string {
	compact := func(text string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return unicode.ToLower(r)
			}
			return -1
		}, text)
	}
	best, bestLength := "", 0
	for _, entry := range character.List(game) {
		candidates := append([]string{entry.ID, entry.Name("en")}, entry.Aliases...)
		for _, candidate := range candidates {
			key := compact(candidate)
			if len(key) < 3 || len(key) <= bestLength {
				continue
			}
			for _, name := range names {
				if strings.HasPrefix(compact(name), key) {
					best, bestLength = entry.ID, len(key)
					break
				}
			}
		}
	}
	return best
}

// blenderViewerOpening reports whether the viewer of a connection's scene is still loading its first scene.
func (s *Shell) blenderViewerOpening(link uint64) bool {
	v := s.mods.viewer
	return v != nil && !v.opened && v.blender != nil && v.blender.link == link
}
