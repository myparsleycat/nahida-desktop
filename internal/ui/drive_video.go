package ui

import (
	"context"
	"os"

	"github.com/myparsleycat/fairy"

	"nahida.live/desktop/internal/infra"
)

// driveVideoFiles fetches drive preview videos into the local cache the players read. Like previewVideos, it
// cancels a download nothing asked for since the previous build.
type driveVideoFiles struct {
	gen   int
	files map[string]*driveVideoFile
	// failed keeps the URLs that could not be fetched, so they are reported once and not fetched again.
	failed map[string]bool
}

type driveVideoFile struct {
	path   string
	cancel context.CancelFunc // set while downloading
	gen    int
}

func (f *driveVideoFiles) sweep() {
	for src, file := range f.files {
		if file.gen != f.gen {
			if file.cancel != nil {
				file.cancel()
			}
			delete(f.files, src)
		}
	}
	f.gen++
}

func (f *driveVideoFiles) cancelAll() {
	for src, file := range f.files {
		if file.cancel != nil {
			file.cancel()
			delete(f.files, src)
		}
	}
}

// driveVideoPath returns the cached file of the preview video at src, or "" while it downloads or after it
// failed.
func (s *Shell) driveVideoPath(src string) string {
	f := &s.drive.videos
	if file := f.files[src]; file != nil {
		file.gen = f.gen
		return file.path
	}
	if src == "" || f.failed[src] || s.deps.Drive == nil {
		return ""
	}
	if f.files == nil {
		f.files, f.failed = map[string]*driveVideoFile{}, map[string]bool{}
	}
	ctx, cancel := context.WithCancel(context.Background())
	file := &driveVideoFile{cancel: cancel, gen: f.gen}
	f.files[src] = file
	go func() {
		path, err := s.deps.Drive.PreviewVideo(ctx, src)
		fairy.Go(func() {
			cancel()
			if f.files[src] != file {
				return
			}
			file.cancel = nil
			if err == nil {
				file.path = path
				return
			}
			delete(f.files, src)
			if infra.IsCancellationError(err) {
				return
			}
			f.failed[src] = true
			_ = infra.ReportError(s.deps.Log, err, "Drive.previewVideo", infra.Diagnostic{
				Severity:  infra.DiagnosticWarn,
				Operation: "drive.preview-video",
				Fields:    map[string]any{"url": infra.SanitizeLogURL(src)},
			})
		})
	}()
	return ""
}

// driveVideo plays the preview video at src once its file is cached, or returns nil until then. It fetches
// only while a player is free, so rows past the limit do not download what they cannot play.
func (s *Shell) driveVideo(src string) *fairy.Video {
	if src == "" || !s.driveVideos.room(src) {
		return nil
	}
	path := s.cachedDriveVideo(src)
	if path == "" {
		return nil
	}
	return s.driveVideos.video(src, path, s.deps.Log, "Drive.preview")
}

// cachedDriveVideo is driveVideoPath for a file about to be opened. The cache prunes files no player holds
// open, so a file that is gone is fetched again instead of failing to play.
func (s *Shell) cachedDriveVideo(src string) string {
	path := s.driveVideoPath(src)
	if path == "" || s.driveVideos.players[src] != nil {
		return path
	}
	if _, err := os.Stat(path); err != nil {
		delete(s.drive.videos.files, src)
		return ""
	}
	return path
}

// driveVideoFailed reports whether the preview video at src could not be fetched or played.
func (s *Shell) driveVideoFailed(src string) bool {
	return s.drive.videos.failed[src] || s.driveVideos.failed[src]
}
