package ui

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
)

const (
	modSidebarWidthDefault float32 = 256
	modSidebarWidthMin     float32 = 220
	modSidebarWidthMax     float32 = 480
	// modRefreshDelay merges the watcher events of one change, which can reach both the game and the folder
	// watcher, into one rescan.
	modRefreshDelay = 100 * time.Millisecond
)

// modScreen is the state of the mod screen. It lives on the UI thread; loads run in the background and hand
// their results back with fairy.Go, dropping a result that a newer load or another selection made stale.
type modScreen struct {
	started     bool
	games       []mod.GameConfig
	gamesLoaded bool
	game        string

	characters     []mod.FolderGroup
	charactersSeq  int
	charactersBusy bool
	// subGroups and manualGroups cache folder listings by parent path; a missing entry is loaded when a row
	// needs it.
	subGroups     map[string][]mod.FolderGroup
	manualGroups  map[string][]mod.FolderGroup
	loadingSub    map[string]bool
	loadingManual map[string]bool
	expanded      map[string]bool
	persistent    map[string]bool
	collapsed     map[string]bool

	classifications      []mod.Classification
	classificationEditor *classificationEditor
	presets              []mod.Preset

	group       *mod.FolderGroup
	listing     *mod.FolderGroup
	listingFull bool
	listingSeq  int
	// previewVersion changes when a folder preview is replaced under the same path, so its image is read again.
	previewVersion int

	sidebarSearch string
	folderSort    string
	folderDesc    bool
	hideEmpty     bool
	sidebarWidth  float32
	resizeFrom    float32
	// scrolledFolder is the selected folder the sidebar last scrolled to or the user clicked.
	scrolledFolder string
	// dropTarget is the key of the element under files being dragged over the window.
	dropTarget string

	search   string
	sortType string
	sortDesc bool
	list     bool
	iniShut  map[string]bool
	hover    string

	filterKey modFilterKey
	filtered  []mod.ModInfo
	// downloading and placeholders hold the lowercased paths of the listed mods that downloads are writing,
	// and of those among them that exist only as a download so far.
	downloading  map[string]bool
	placeholders map[string]bool

	// downloadResolved holds the downloads already matched to a folder, downloadPicked the download during
	// which the user chose a folder, and downloadTarget a match waiting for its game's folders.
	downloadResolved map[string]bool
	downloadPicked   string
	downloadTarget   *downloadTarget

	toggling      map[string]string
	exclusive     map[string]bool
	pendingEnable map[string]bool
	stepping      map[string]bool
	gameBanana    gameBananaSource
	bulkBusy      bool
	busyPaths     map[string]bool
	presetBusy    bool

	refreshPending bool
	refreshGame    bool

	folderDraft *folderDraft
	renameDraft *renameDraft
	keyEdit     *modKeyEdit
	pasteDraft  *pasteDraft
	openPreset  *mod.Preset
	presetDraft *presetDraft
	extractAsks []extractAsk
	downloadAsk *downloadAsk
	gameDraft   *gameDraft
	nteLaunch   *nteLaunchDraft
	nteStarting bool
	// nteProgress is the last NTE bootstrap progress event, which adding or editing an NTE game shows.
	nteProgress *mod.NteBootstrapProgress
	urlDownload *urlDownloadDraft
	// lightbox is the mod whose preview is shown large.
	lightbox *mod.ModInfo

	// modelPreviews holds the mod grid's rendered model previews.
	modelPreviews modelPreviews
	// mergeMode makes a click select a mod for merging instead of toggling it.
	mergeMode     bool
	mergeSelected map[string]bool
	mergeDraft    *mergeDraft

	conflict *conflictDraft
	bisect   *tools.BisectSnapshot
	// texture is the texture resizer open on one mod.
	texture *textureWorkspace
	fix     fixRunner
	// fixInspections lists the mods that need a fix; pendingFix is the one whose fixer opens once games load.
	fixInspections tools.FixInspectionSnapshot
	pendingFix     *tools.FixInspectionRecord
	outline        *outlineDraft
	// viewer is the model viewer dialog open on one mod.
	viewer *ModelViewer
}

func newModScreen() modScreen {
	return modScreen{
		subGroups:        map[string][]mod.FolderGroup{},
		manualGroups:     map[string][]mod.FolderGroup{},
		loadingSub:       map[string]bool{},
		loadingManual:    map[string]bool{},
		expanded:         map[string]bool{},
		persistent:       map[string]bool{},
		collapsed:        map[string]bool{},
		iniShut:          map[string]bool{},
		toggling:         map[string]string{},
		exclusive:        map[string]bool{},
		busyPaths:        map[string]bool{},
		pendingEnable:    map[string]bool{},
		stepping:         map[string]bool{},
		mergeSelected:    map[string]bool{},
		downloadResolved: map[string]bool{},
		folderSort:       "name",
		sortType:         "name",
	}
}

func (s *Shell) registerModEvents(on func(string, func([]any))) {
	on("mod:update-game", func([]any) { s.scheduleModRefresh(true) })
	on("mod:update-mods", func([]any) { s.scheduleModRefresh(false) })
	on("download:completed", func(data []any) {
		info, _ := first(data).(map[string]string)
		if info == nil {
			if raw, ok := first(data).(map[string]any); ok {
				info = map[string]string{}
				for key, value := range raw {
					info[key], _ = value.(string)
				}
			}
		}
		s.modDownloadCompleted(info["path"], info["name"])
	})
	on("mod:archiveExtractPrompt", func(data []any) {
		prompt, _ := first(data).(map[string]string)
		if prompt["requestId"] != "" {
			s.mods.downloadAsk = &downloadAsk{requestID: prompt["requestId"], fileName: prompt["fileName"]}
		}
	})
	on("mod:nte-bootstrap-progress", func(data []any) {
		if progress, ok := first(data).(mod.NteBootstrapProgress); ok {
			s.mods.nteProgress = &progress
		}
	})
	s.registerFixEvents(on)
	on("tools:bisectState", func(data []any) {
		if snapshot, ok := first(data).(tools.BisectSnapshot); ok {
			s.mods.bisect = &snapshot
		}
	})
	on("mod:archiveExtractPromptDismissed", func(data []any) {
		prompt, _ := first(data).(map[string]string)
		if ask := s.mods.downloadAsk; ask != nil && ask.requestID == prompt["requestId"] {
			s.mods.downloadAsk = nil
		}
	})
}

// background runs work once startup maintenance is done, as the Wails service guard made every Mod call
// wait. Work does not run when the application shuts down first.
func (s *Shell) background(work func(ctx context.Context)) {
	go func() {
		ctx := context.Background()
		if s.deps.Ready != nil {
			if err := s.deps.Ready(ctx); err != nil {
				return
			}
		}
		work(ctx)
	}()
}

func (s *Shell) modError(err error, operation string, fields map[string]any) {
	_ = infra.ReportError(s.deps.Log, err, "Mod", infra.Diagnostic{Operation: operation, Fields: fields})
}

// startMods loads the games the first time the screen shows.
func (s *Shell) startMods() {
	m := &s.mods
	if m.started {
		return
	}
	m.started = true
	s.loadFixLists()
	s.background(func(ctx context.Context) {
		expanded, err := s.deps.Mod.GetExpandedGroups(ctx)
		if err != nil {
			s.modError(err, "read-expanded-groups", nil)
		}
		fairy.Go(func() {
			for _, path := range expanded {
				m.persistent[path] = true
				m.expanded[path] = true
			}
		})
	})
	s.loadGames(true)
}

// loadGames reads the games. On the first load it restores the game the user had open: the game whose window
// was focused last, then the last selected one, then the first.
func (s *Shell) loadGames(restore bool) {
	m := &s.mods
	s.background(func(ctx context.Context) {
		games, err := s.deps.Mod.GetGames(ctx)
		if err != nil {
			s.modError(err, "read-games", nil)
		}
		choice := ""
		if restore && len(games) > 0 {
			has := func(game *string) bool {
				return game != nil && slices.ContainsFunc(games, func(g mod.GameConfig) bool { return g.Game == *game })
			}
			if focused, err := s.deps.Mod.GetPreviousFocusedGame(ctx); err == nil && has(focused) {
				choice = *focused
			} else if last, err := s.deps.Mod.GetLastGame(ctx); err == nil && has(last) {
				choice = *last
			}
		}
		fairy.Go(func() {
			if err != nil {
				return
			}
			before := s.gameConfig()
			m.games, m.gamesLoaded = games, true
			s.followModFolder(before)
			switch {
			// A download handed to the screen chooses its own game.
			case choice != "" && s.download == nil:
				s.selectGame(choice, false)
			case len(games) > 0 && !slices.ContainsFunc(games, func(g mod.GameConfig) bool { return g.Game == m.game }):
				s.selectGame(games[0].Game, true)
			case len(games) == 0 && m.game != "":
				s.selectGame("", true)
			}
		})
	})
}

// pendingFixReady reports whether the fix a fix inspection asked for can open now: a game holds its mod and no
// fixer is being prepared. Until then the request is kept, as the React route kept it.
func (s *Shell) pendingFixReady() bool {
	m := &s.mods
	record := m.pendingFix
	if record == nil || !m.gamesLoaded || m.fix.preparing {
		return false
	}
	return slices.ContainsFunc(m.games, func(game mod.GameConfig) bool {
		return strings.EqualFold(deref(game.Importer), record.Result.Importer) &&
			platform.SameOrChildPath(game.ModFolderPath, record.ModPath)
	})
}

// followModFolder rereads the open game when the backend moved its mod folder with its importer folder, which
// leaves the listings of the old folder and its watcher behind.
func (s *Shell) followModFolder(before *mod.GameConfig) {
	m := &s.mods
	after := s.gameConfig()
	if before == nil || after == nil || before.Game != after.Game ||
		platform.SamePathFold(before.ModFolderPath, after.ModFolderPath) {
		return
	}
	game := after.Game
	clear(m.subGroups)
	clear(m.manualGroups)
	s.selectGroup(nil)
	s.loadCharacters()
	s.background(func(ctx context.Context) {
		if err := s.deps.Mod.WatchGame(ctx, game); err != nil {
			s.modError(err, "watch-game", map[string]any{"game": game})
		}
	})
}

func (s *Shell) gameConfig() *mod.GameConfig {
	m := &s.mods
	for i := range m.games {
		if m.games[i].Game == m.game {
			return &m.games[i]
		}
	}
	return nil
}

func (s *Shell) isNteGame() bool {
	game := s.gameConfig()
	return game != nil && importerOf(*game) == nteImporter
}

// selectGame opens game. persist stores it as the last game, which a choice of the user or a fallback does.
func (s *Shell) selectGame(game string, persist bool) {
	m := &s.mods
	if persist {
		s.background(func(ctx context.Context) {
			if err := s.deps.Mod.SetLastGame(ctx, game); err != nil {
				s.modError(err, "save-last-game", map[string]any{"game": game})
			}
		})
	}
	if game == m.game && m.charactersSeq > 0 {
		return
	}
	m.game = game
	m.characters, m.classifications, m.presets = nil, nil, nil
	clear(m.subGroups)
	clear(m.manualGroups)
	m.sidebarSearch = ""
	s.selectGroup(nil)
	if game == "" {
		return
	}
	s.background(func(ctx context.Context) {
		if err := s.deps.Mod.WatchGame(ctx, game); err != nil {
			s.modError(err, "watch-game", map[string]any{"game": game})
		}
	})
	s.loadCharacters()
	s.loadPresets()
	s.loadClassifications()
}

func (s *Shell) loadCharacters() {
	m := &s.mods
	m.charactersSeq++
	seq, game := m.charactersSeq, m.game
	m.charactersBusy = true
	s.background(func(ctx context.Context) {
		groups, err := s.deps.Mod.GetCharacters(ctx, game, nil)
		if err != nil {
			s.modError(err, "read-characters", map[string]any{"game": game})
		}
		fairy.Go(func() {
			if seq != m.charactersSeq {
				return
			}
			m.charactersBusy = false
			if err != nil {
				return
			}
			m.characters = groups
			s.dropMissingSelection()
		})
	})
}

// dropMissingSelection clears a selected folder that is no longer below a listed folder.
func (s *Shell) dropMissingSelection() {
	m := &s.mods
	if m.group == nil {
		return
	}
	for _, top := range m.characters {
		if platform.SameOrChildPath(top.Path, m.group.Path) {
			return
		}
	}
	s.selectGroup(nil)
}

func (s *Shell) loadPresets() {
	m := &s.mods
	game := m.game
	s.background(func(ctx context.Context) {
		presets, err := s.deps.Mod.GetPresets(ctx, game)
		if err != nil {
			s.modError(err, "read-presets", map[string]any{"game": game})
			return
		}
		fairy.Go(func() {
			if m.game == game {
				m.presets = presets
			}
		})
	})
}

func (s *Shell) loadClassifications() {
	m := &s.mods
	game := m.game
	s.background(func(ctx context.Context) {
		classifications, err := s.deps.Mod.GetClassifications(ctx, game)
		if err != nil {
			s.modError(err, "read-classifications", map[string]any{"game": game})
			return
		}
		fairy.Go(func() {
			if m.game == game {
				m.classifications = classifications
			}
		})
	})
}

// subGroupsOf returns the listing below path, loading it the first time.
func (s *Shell) subGroupsOf(path string, manual bool) []mod.FolderGroup {
	m := &s.mods
	cache, loading := m.subGroups, m.loadingSub
	if manual {
		cache, loading = m.manualGroups, m.loadingManual
	}
	groups, ok := cache[path]
	if !ok && !loading[path] {
		s.loadSubGroups(path, manual)
	}
	return groups
}

func (s *Shell) loadSubGroups(path string, manual bool) {
	m := &s.mods
	loading := m.loadingSub
	if manual {
		loading = m.loadingManual
	}
	loading[path] = true
	game := m.game
	s.background(func(ctx context.Context) {
		var groups []mod.FolderGroup
		var err error
		if manual {
			groups, err = s.deps.Mod.GetManualSubGroups(ctx, path, nil)
		} else {
			groups, err = s.deps.Mod.GetSubGroups(ctx, path, nil)
		}
		if err != nil {
			s.modError(err, "read-sub-groups", map[string]any{"path": path, "manual": manual})
		}
		fairy.Go(func() {
			delete(loading, path)
			if m.game != game {
				return
			}
			if err != nil {
				groups = nil
			}
			if manual {
				m.manualGroups[path] = groups
			} else {
				m.subGroups[path] = groups
			}
		})
	})
}

// refreshFolders rescans the folder tree, keeping the shown listings until the new ones arrive.
func (s *Shell) refreshFolders() {
	m := &s.mods
	if m.game == "" {
		return
	}
	s.loadCharacters()
	for path := range m.subGroups {
		s.loadSubGroups(path, false)
	}
	for path := range m.manualGroups {
		s.loadSubGroups(path, true)
	}
}

func (s *Shell) scheduleModRefresh(game bool) {
	m := &s.mods
	if !m.started {
		return
	}
	m.refreshGame = m.refreshGame || game
	if m.refreshPending {
		return
	}
	m.refreshPending = true
	time.AfterFunc(modRefreshDelay, func() {
		fairy.Go(func() {
			game := m.refreshGame
			m.refreshPending, m.refreshGame = false, false
			s.refreshFolders()
			if !game || m.group != nil {
				s.refreshListing()
			}
		})
	})
}

func (s *Shell) modDownloadCompleted(path, name string) {
	m := &s.mods
	if !m.started {
		return
	}
	s.refreshFolders()
	if m.group != nil && platform.SameOrChildPath(m.group.Path, path) {
		s.refreshListing()
	}
	if s.router.Current() == RouteMod && name != "" {
		s.toasts.Show(kit.ToastSuccess, T("page.mod.download_completed", "name", name), "")
	}
}

// modFocused rescans the folders when the window comes back, as changes made meanwhile may have been missed.
func (s *Shell) modFocused() {
	if s.mods.started && s.mods.game != "" {
		s.loadCharacters()
	}
}

// selectGroup shows the mods of group, or none.
func (s *Shell) selectGroup(group *mod.FolderGroup) {
	m := &s.mods
	if group != nil && m.group != nil && m.group.Path == group.Path {
		m.group = group
		return
	}
	m.group, m.listing, m.listingFull, m.hover, m.lightbox = group, nil, false, "", nil
	m.listingSeq++
	s.setMergeMode(false)
	s.closeModelViewer()
	if group == nil {
		return
	}
	path := group.Path
	s.background(func(ctx context.Context) {
		if err := s.deps.Mod.WatchCharacter(ctx, path); err != nil {
			s.modError(err, "watch-folder", map[string]any{"path": path})
		}
	})
	s.loadListing(true)
}

// loadListing reads the mods of the selected folder. light shows the quick listing, which reads no INI, while
// the full one is scanned.
func (s *Shell) loadListing(light bool) {
	m := &s.mods
	if m.group == nil {
		return
	}
	m.listingSeq++
	seq, path := m.listingSeq, m.group.Path
	s.background(func(ctx context.Context) {
		if light {
			group, err := s.deps.Mod.GetModsLight(ctx, path)
			fairy.Go(func() {
				if err == nil && seq == m.listingSeq && !m.listingFull {
					m.listing = &group
				}
			})
		}
		group, err := s.deps.Mod.GetMods(ctx, path)
		if err != nil {
			s.modError(err, "read-mods", map[string]any{"path": path})
		}
		fairy.Go(func() {
			if err == nil && seq == m.listingSeq {
				m.listing, m.listingFull = &group, true
			}
		})
	})
}

func (s *Shell) refreshListing() { s.loadListing(false) }

// applyListing shows a listing read after an action on the folder at path.
func (s *Shell) applyListing(path string, group mod.FolderGroup) {
	m := &s.mods
	if m.group != nil && m.group.Path == path {
		m.listingSeq++
		m.listing, m.listingFull = &group, true
	}
}

// renameFolderPath follows a folder rename in the expanded, persistent and selected paths.
func (s *Shell) renameFolderPath(oldPath, newPath string) {
	m := &s.mods
	rename := func(path string) string { return replacePathPrefix(path, oldPath, newPath) }
	renameSet := func(set map[string]bool) {
		next := map[string]bool{}
		for path, on := range set {
			next[rename(path)] = on
		}
		clear(set)
		maps.Copy(set, next)
	}
	before := slices.Sorted(maps.Keys(m.persistent))
	renameSet(m.expanded)
	renameSet(m.persistent)
	if after := slices.Sorted(maps.Keys(m.persistent)); !slices.Equal(before, after) {
		s.savePersistentGroups()
	}
	if m.group != nil && platform.SameOrChildPath(oldPath, m.group.Path) {
		group := *m.group
		if platform.SamePathFold(group.Path, oldPath) {
			group.IsEnabled = !group.IsEnabled
		}
		group.Path = rename(group.Path)
		m.group = &group
		m.listingSeq++
		s.background(func(ctx context.Context) {
			if err := s.deps.Mod.WatchCharacter(ctx, group.Path); err != nil {
				s.modError(err, "watch-folder", map[string]any{"path": group.Path})
			}
		})
		s.loadListing(true)
	}
}

func (s *Shell) savePersistentGroups() {
	paths := slices.Sorted(maps.Keys(s.mods.persistent))
	s.background(func(ctx context.Context) {
		if err := s.deps.Mod.SetExpandedGroups(ctx, paths); err != nil {
			s.modError(err, "save-expanded-groups", nil)
		}
	})
}

func replacePathPrefix(path, oldPath, newPath string) string {
	if !platform.SameOrChildPath(oldPath, path) {
		return path
	}
	return newPath + path[len(oldPath):]
}

func parentPath(path string) string {
	parent := filepath.Dir(path)
	if parent == path || parent == "." {
		return ""
	}
	return parent
}

// modSidebarWidth is the stored width of the folder sidebar, clamped as the React screen did.
func (s *Shell) modSidebarWidth() float32 {
	m := &s.mods
	if m.sidebarWidth > 0 {
		return m.sidebarWidth
	}
	if !s.settingsLoaded(setting.KeyModCharacterSidebarWidth) {
		return modSidebarWidthDefault
	}
	width := float32(s.intSetting(setting.KeyModCharacterSidebarWidth))
	if width <= 0 {
		width = modSidebarWidthDefault
	}
	return min(max(width, modSidebarWidthMin), modSidebarWidthMax)
}

func (s *Shell) modPage(cx *fairy.Cx) *fairy.El {
	s.startMods()
	if s.pendingFixReady() {
		s.applyPendingFix()
	}
	s.resolveDownloadTarget()
	s.applyDownloadTarget()
	t := kit.T()
	m := &s.mods
	m.dropTarget = cx.FileDropTarget()
	content := fairy.Col().Key("mod.content").Grow(1).MinW(0).HFull().Clip().Fill(t.Background.Alpha(0.7))
	if m.group != nil {
		content.Child(s.modHeader())
	}
	content.Child(s.modBody(cx))
	if m.group != nil {
		group := m.group
		content.OnFileDrop(func(drop fairy.FileDrop) { s.dropIntoGroup(group.Path, drop.Paths) }).
			DropFill(t.Accent.Alpha(0.08)).DropBorder(t.Accent)
		if m.dropTarget == "mod.content" {
			content.Child(s.modDropHint(group.Name))
		}
	}
	if s.download != nil {
		content.Child(s.downloadOverlay())
	}
	s.showModDialogs(cx)
	return fairy.Row(s.modSidebar(cx), content).WFull().HFull().OnKey(s.modPageKey)
}

// modPageKey leaves merge mode with Escape. Open dialogs and menus take Escape before it reaches the page.
func (s *Shell) modPageKey(k keyboard.KeyEvent) bool {
	if !k.Is(keyboard.KeyEscape, 0) || !s.mods.mergeMode {
		return false
	}
	s.setMergeMode(false)
	return true
}

// folderDropHint covers a sidebar folder while files dragged over it would be added to it.
func folderDropHint(name string) *fairy.El {
	t := kit.T()
	return fairy.Box(fairy.Text(T("page.mod.character-sidebar.add-to-character", "name", name)).FontSize(14).LineHeight(20).
		Bold().Ellipsis()).
		Absolute().
		Inset(0).
		Center().
		PadXY(8, 0).
		Border(2, t.Primary).
		BorderDashed().
		Fill(t.Background.Alpha(0.8)).
		BackdropBlur(8).
		PassThrough()
}

func (s *Shell) modDropHint(name string) *fairy.El {
	return dropOverlay(T("page.mod.dad_section.title", "name", name), T("page.mod.dad_section.description"))
}

// dropOverlay covers a file list while dragged files would be added to it.
func dropOverlay(title, description string) *fairy.El {
	t := kit.T()
	return fairy.Col(
		fairy.Text(title).FontSize(24).LineHeight(32).Bold().TextAlign(fairy.TextCenter),
		fairy.Text(description).FontSize(14).LineHeight(20).Color(t.MutedForeground).TextAlign(fairy.TextCenter).
			MarginEdges(8, 0, 0, 0),
	).Absolute().Inset(0).Center().Border(2, t.Primary).BorderDashed().Fill(t.Background.Alpha(0.8)).
		BackdropBlur(8).PassThrough()
}

func modListingKey(groupPath, modPath string) string { return groupPath + "\x00" + modPath }

func formatModTime(mtime float64, withTime bool) string {
	return formatDateTime(time.UnixMilli(int64(mtime)), withTime)
}
