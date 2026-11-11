package ui

import (
	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/infra"
)

// maxPreviewVideos bounds the decoders one screen keeps open; previews past it show their placeholder.
const maxPreviewVideos = 8

// previewVideos owns the muted, looping preview players of one screen. Virtual lists build and drop rows
// without telling the screen, so a player is closed by the first view build after one in which nothing asked
// for it. Rows that scrolled out keep decoding, unseen, until then.
type previewVideos struct {
	gen     int
	players map[string]*previewPlayer
	// failed keeps the keys whose file could not be played, so they are reported once and not reopened.
	failed map[string]bool
}

type previewPlayer struct {
	video *fairy.Video
	gen   int
}

// sweep closes the players nothing asked for since the previous sweep. The shell calls it as each build starts.
func (p *previewVideos) sweep() {
	for key, player := range p.players {
		if player.gen != p.gen {
			player.video.Close()
			delete(p.players, key)
		}
	}
	p.gen++
}

func (p *previewVideos) closeAll() {
	for _, player := range p.players {
		player.video.Close()
	}
	clear(p.players)
}

// video returns the player for the local file at path, opening it when there is room. key names the file's
// version, so a replaced file is opened again. It returns nil when the file cannot be played or the screen
// already has maxPreviewVideos players.
func (p *previewVideos) video(key, path string, log *infra.Log, where string) *fairy.Video {
	return p.open(key, path, log, where, maxPreviewVideos)
}

// room reports whether video would return a player for key now.
func (p *previewVideos) room(key string) bool {
	return p.players[key] != nil || (!p.failed[key] && len(p.players) < maxPreviewVideos)
}

// pinned is video without the limit, for the preview the user opened on its own.
func (p *previewVideos) pinned(key, path string, log *infra.Log, where string) *fairy.Video {
	return p.open(key, path, log, where, -1)
}

func (p *previewVideos) open(key, path string, log *infra.Log, where string, limit int) *fairy.Video {
	if player := p.players[key]; player != nil {
		player.gen = p.gen
		return player.video
	}
	if p.failed[key] || (limit >= 0 && len(p.players) >= limit) {
		return nil
	}
	if p.players == nil {
		p.players, p.failed = map[string]*previewPlayer{}, map[string]bool{}
	}
	fail := func(err error) {
		if player := p.players[key]; player != nil {
			player.video.Close()
			delete(p.players, key)
		}
		p.failed[key] = true
		_ = infra.ReportError(log, err, where, infra.Diagnostic{
			Severity:  infra.DiagnosticWarn,
			Operation: "preview.video",
			Fields:    map[string]any{"path": path},
		})
	}
	video, err := fairy.OpenVideo(path, fairy.VideoOptions{AutoPlay: true, Loop: true, Muted: true, OnError: fail})
	if err != nil {
		fail(err)
		return nil
	}
	p.players[key] = &previewPlayer{video: video, gen: p.gen}
	return video
}
