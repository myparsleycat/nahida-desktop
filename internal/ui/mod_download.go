package ui

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"

	"github.com/samber/lo"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/transfer"
)

// downloadTarget is the folder a download was matched to, selected once its game lists it.
type downloadTarget struct {
	id    string
	game  string
	group mod.FolderGroup
}

// markDownloadPick records that the user chose a folder for the open download, which a pending or later
// automatic match must not override.
func (s *Shell) markDownloadPick() {
	if s.download != nil {
		s.mods.downloadPicked = s.download.id
	}
}

// resolveDownloadTarget matches the download handed to the mod screen to a folder by its names, when the
// setting allows it for the download's source, as the React route did.
func (s *Shell) resolveDownloadTarget() {
	m := &s.mods
	d := s.download
	if d == nil || m.downloadResolved[d.id] {
		return
	}
	if !s.settingsLoaded(setting.KeyModAutoResolveDownloadTarget, setting.KeyModAutoResolveDownloadTargetSources) {
		return
	}
	source := cmp.Or(d.source, "gamebanana")
	if !s.boolSetting(setting.KeyModAutoResolveDownloadTarget) ||
		!slices.Contains(s.stringsSetting(setting.KeyModAutoResolveDownloadTargetSources), source) {
		return
	}
	if d.suggestedName == "" && len(d.suggestedNames) == 0 && d.targetName == "" && d.importerKey == "" {
		return
	}

	var gameFilter *string
	if d.importerKey != "" {
		if !m.gamesLoaded {
			return
		}
		game, ok := lo.Find(m.games, func(g mod.GameConfig) bool { return importerOf(g) == d.importerKey })
		if !ok {
			return
		}
		gameFilter = &game.Game
	}

	m.downloadResolved[d.id] = true
	id, primary := d.id, d.targetName
	fallbacks := d.suggestedNames
	if len(fallbacks) == 0 && d.suggestedName != "" {
		fallbacks = []string{d.suggestedName}
	}
	s.background(func(ctx context.Context) {
		result, err := s.matchDownloadTarget(ctx, primary, fallbacks, gameFilter)
		fairy.Go(func() {
			if s.download == nil || s.download.id != id || m.downloadPicked == id {
				return
			}
			if err != nil {
				s.modError(err, "resolve-download-target", map[string]any{
					"selectionId": id, "target": primary, "names": fallbacks,
				})
				return
			}
			switch {
			case result != nil:
				game := result.Game
				if gameFilter != nil {
					game = *gameFilter
				}
				s.selectGame(game, true)
				m.downloadTarget = &downloadTarget{id: id, game: game, group: result.Group}
				s.applyDownloadTarget()
			case gameFilter != nil:
				s.selectGame(*gameFilter, true)
			}
		})
	})
}

// matchDownloadTarget tries the primary name, then the suggested names, which count only when they all point
// at the same folder.
func (s *Shell) matchDownloadTarget(
	ctx context.Context,
	primary string,
	fallbacks []string,
	gameFilter *string,
) (*mod.DownloadTarget, error) {
	if primary != "" {
		result, err := s.deps.Mod.ResolveDownloadTarget(ctx, primary, gameFilter)
		if err != nil || result != nil {
			return result, err
		}
	}
	results := make([]*mod.DownloadTarget, 0, len(fallbacks))
	for _, name := range fallbacks {
		result, err := s.deps.Mod.ResolveDownloadTarget(ctx, name, gameFilter)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	if len(results) == 0 || results[0] == nil {
		return nil, nil
	}
	first := results[0]
	same := lo.EveryBy(results, func(r *mod.DownloadTarget) bool {
		return r != nil && r.Game == first.Game && platform.SamePathFold(r.Group.Path, first.Group.Path)
	})
	if !same {
		return nil, nil
	}
	return first, nil
}

// applyDownloadTarget selects the matched folder once its game is open and lists it, opening its parent.
func (s *Shell) applyDownloadTarget() {
	m := &s.mods
	target := m.downloadTarget
	if target == nil {
		return
	}
	if s.download == nil || s.download.id != target.id || m.downloadPicked == target.id {
		m.downloadTarget = nil
		return
	}
	if m.game != target.game ||
		!lo.SomeBy(
			m.characters,
			func(g mod.FolderGroup) bool { return platform.SameOrChildPath(g.Path, target.group.Path) },
		) {
		return
	}
	if parent := parentPath(target.group.Path); parent != "" &&
		lo.SomeBy(m.characters, func(g mod.FolderGroup) bool { return platform.SamePathFold(g.Path, parent) }) {
		m.expanded[parent] = true
	}
	m.downloadTarget = nil
	group := target.group
	s.selectGroup(&group)
}

// modDownload is a download writing into the selected folder: a folder it creates, or a mod it replaces.
type modDownload struct {
	path  string
	pid   string
	start int64
}

func isActiveModDownload(snapshot transfer.Snapshot) bool {
	switch snapshot.Status {
	case transfer.StatusPending, transfer.StatusPreparing, transfer.StatusProgress, transfer.StatusPaused:
		return snapshot.Type == "download"
	default:
		return false
	}
}

// modDownloadsIn lists the folders active downloads create directly below groupPath, each once.
func (s *Shell) modDownloadsIn(groupPath string) []modDownload {
	var out []modDownload
	for _, snapshot := range s.transfers.items {
		if !isActiveModDownload(snapshot) {
			continue
		}
		for _, target := range snapshot.DestinationTargets {
			if target.Kind != transfer.DestinationDirectory ||
				!platform.SamePathFold(parentPath(strings.TrimRight(target.Path, `\/`)), groupPath) ||
				lo.ContainsBy(out, func(d modDownload) bool { return platform.SamePathFold(d.path, target.Path) }) {
				continue
			}
			out = append(out, modDownload{path: target.Path, pid: snapshot.PID, start: snapshot.StartTime})
		}
	}
	return out
}

// modDownloadTransfer returns the active download writing to modPath.
func (s *Shell) modDownloadTransfer(modPath string) *transfer.Snapshot {
	for i := range s.transfers.items {
		snapshot := &s.transfers.items[i]
		if isActiveModDownload(*snapshot) && slices.ContainsFunc(snapshot.DestinationPaths, func(path string) bool {
			return platform.SamePathFold(strings.TrimRight(path, `\/`), strings.TrimRight(modPath, `\/`))
		}) {
			return snapshot
		}
	}
	return nil
}

func downloadIdentity(downloads []modDownload) string {
	return strings.Join(lo.Map(downloads, func(d modDownload, _ int) string {
		return d.pid + "\x00" + strings.ToLower(d.path)
	}), "\x01")
}

// withDownloads marks the mods that downloads are writing and adds a placeholder for each folder a download
// creates, as useModsWithDownloadPlaceholders did. The returned set holds the lowercased paths of both.
func withDownloads(mods []mod.ModInfo, downloads []modDownload) ([]mod.ModInfo, map[string]bool, map[string]bool) {
	if len(downloads) == 0 {
		return mods, nil, nil
	}
	downloading, placeholders := map[string]bool{}, map[string]bool{}
	for _, d := range downloads {
		downloading[strings.ToLower(d.path)] = true
	}
	installed := lo.SliceToMap(mods, func(info mod.ModInfo) (string, bool) { return strings.ToLower(info.Path), true })
	names := append(
		lo.Map(mods, func(info mod.ModInfo, _ int) string { return parentBase(info.Path) }),
		lo.Map(downloads, func(d modDownload, _ int) string { return parentBase(d.path) })...,
	)
	out := slices.Clone(mods)
	for _, d := range downloads {
		key := strings.ToLower(d.path)
		if installed[key] {
			continue
		}
		placeholders[key] = true
		out = append(out, mod.ModInfo{
			ID:    "download:" + d.pid + ":" + key,
			Name:  siblingDisplayName(parentBase(d.path), names),
			Path:  d.path,
			Mtime: float64(d.start),
		})
	}
	return out, downloading, placeholders
}

// siblingDisplayName keeps the DISABLED prefix in a name only when a sibling has the same name without it.
func siblingDisplayName(name string, siblings []string) string {
	key := strings.ToLower(modname.StripDisabled(name))
	twins := lo.CountBy(siblings, func(sibling string) bool {
		return strings.ToLower(modname.StripDisabled(sibling)) == key
	})
	if twins > 1 {
		return strings.TrimSpace(name)
	}
	return modname.StripDisabled(name)
}

func (s *Shell) isModDownloading(path string) bool {
	return s.mods.downloading[strings.ToLower(path)] || s.modDownloadTransfer(path) != nil
}

func (s *Shell) isDownloadPlaceholder(path string) bool {
	return s.mods.placeholders[strings.ToLower(path)]
}

// modDownloadOverlay covers a card or row while a download writes to its folder, with the transfer's state
// and progress.
func modDownloadOverlay(snapshot *transfer.Snapshot, row bool) *fairy.El {
	t := kit.T()
	var progress *float64
	if snapshot.TotalSize > 0 {
		value := snapshot.Progress
		progress = &value
	}
	status, pulse, speed := "downloading", progress == nil, snapshot.Speed
	switch {
	case snapshot.Status == transfer.StatusPending:
		status, pulse, speed = "queued", true, 0
	case snapshot.Status == transfer.StatusPreparing:
		status, pulse, speed = "preparing", true, 0
	case snapshot.Status == transfer.StatusPaused:
		status, pulse, speed = "paused", false, 0
	case progress != nil && *progress >= 100:
		status, pulse, speed = "finalizing", false, 0
	}

	icon := fairy.Spinner().Size(16, 16).Shrink(0)
	if status == "paused" {
		icon = fairy.Icon(lucide.Pause).Size(16, 16).Shrink(0)
	}
	numbers := fairy.Row().Gap(12).Shrink(0).FontSize(12).Color(t.MutedForeground)
	if speed > 0 {
		numbers.Child(fairy.Text(formatSize(int64(speed)) + "/s"))
	}
	percent := "--%"
	if progress != nil {
		percent = itoa(int(math.Round(*progress))) + "%"
	}
	numbers.Child(fairy.Text(percent))
	bar := kit.Progress(lo.FromPtr(progress) / 100).WFull()
	if pulse {
		bar.Pulse(2, 0.5)
	}
	width, gap := float32(384), float32(8)
	if row {
		width, gap = 576, 6
	}
	body := fairy.Col(
		fairy.Row(
			fairy.Row(icon, fairy.Text(T("page.mod.download_overlay."+status)).Ellipsis().MinW(0)).Gap(8).
				Align(fairy.Center).MinW(0),
			numbers,
		).Gap(12).Align(fairy.Center).Justify(fairy.JustifyBetween).FontSize(14).Medium(),
		bar,
	).WFull().MaxW(width).Gap(gap)
	return fairy.Box(body).Absolute().Inset(0).Center().PadXY(16, 0).Fill(t.Muted.Alpha(0.88)).
		Cursor(fairy.CursorWait).Role(fairy.RoleProgressBar).Label(T("page.mod.download_overlay." + status))
}
