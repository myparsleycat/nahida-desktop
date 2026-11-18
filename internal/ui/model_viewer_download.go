package ui

import (
	"strconv"
	"sync"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
)

// viewerDownloadTask is a kind of load whose character data downloads the progress panel shows.
type viewerDownloadTask int

const (
	downloadMotion viewerDownloadTask = iota
	downloadOriginal
	downloadTasks
)

const (
	// viewerDownloadPostGap is the shortest time between the rebuilds download progress asks for.
	viewerDownloadPostGap = 100 * time.Millisecond
	viewerDownloadPanelW  = 240
)

// viewerDownloads holds the progress of the character data files the viewer's loads read. The loads' goroutines
// report into it; the UI thread begins and ends the loads and reads it.
type viewerDownloads struct {
	mu     sync.Mutex
	tasks  [downloadTasks]viewerDownloadState
	posted time.Time
}

// viewerDownloadState is a task's running loads and their files by name. fetched is set once a file comes from the
// network rather than the cache.
type viewerDownloadState struct {
	runs    int
	files   map[string]viewerDownloadFile
	fetched bool
}

type viewerDownloadFile struct {
	done, size int64
}

// viewerDownloadRun is one load of a task. It reports its files and ends once, on the UI thread.
type viewerDownloadRun struct {
	d    *viewerDownloads
	task viewerDownloadTask
	post func(func())
}

// begin starts a load of task, whose progress rebuilds through post. The task's files are cleared when no other
// load of it runs.
func (d *viewerDownloads) begin(task viewerDownloadTask, post func(func())) viewerDownloadRun {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := &d.tasks[task]
	if s.runs == 0 {
		s.files, s.fetched = map[string]viewerDownloadFile{}, false
	}
	s.runs++
	return viewerDownloadRun{d: d, task: task, post: post}
}

func (r viewerDownloadRun) end() {
	if r.d == nil {
		return
	}
	r.d.mu.Lock()
	defer r.d.mu.Unlock()
	s := &r.d.tasks[r.task]
	s.runs = max(s.runs-1, 0)
}

// expect counts a file the load will read before it starts, so the total does not grow as downloads wait their turn.
func (r viewerDownloadRun) expect(file string, size int64) {
	if r.d == nil || file == "" {
		return
	}
	r.d.mu.Lock()
	defer r.d.mu.Unlock()
	s := &r.d.tasks[r.task]
	if _, ok := s.files[file]; !ok && s.runs > 0 {
		s.files[file] = viewerDownloadFile{size: size}
	}
}

// report takes a file's progress from a load's goroutine and asks for a rebuild while the panel shows it.
func (r viewerDownloadRun) report(file string, done, size int64, cached bool) {
	if r.d == nil {
		return
	}
	r.d.mu.Lock()
	s := &r.d.tasks[r.task]
	if s.runs == 0 {
		r.d.mu.Unlock()
		return
	}
	s.files[file] = viewerDownloadFile{done: min(max(done, 0), size), size: size}
	started := !cached && !s.fetched
	s.fetched = s.fetched || !cached
	now := time.Now()
	post := s.fetched && (started || done >= size || now.Sub(r.d.posted) >= viewerDownloadPostGap)
	if post {
		r.d.posted = now
	}
	r.d.mu.Unlock()
	if post && r.post != nil {
		r.post(func() {})
	}
}

// viewerDownloadProgress is what the panel shows of a task: done of total bytes.
type viewerDownloadProgress struct {
	task        viewerDownloadTask
	done, total int64
}

// shown lists the tasks with a load running that downloads a file.
func (d *viewerDownloads) shown() []viewerDownloadProgress {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []viewerDownloadProgress
	for task := range downloadTasks {
		s := &d.tasks[task]
		if s.runs == 0 || !s.fetched {
			continue
		}
		p := viewerDownloadProgress{task: task}
		for _, file := range s.files {
			p.done += file.done
			p.total += file.size
		}
		out = append(out, p)
	}
	return out
}

// downloadPanel shows the progress of the character data being downloaded, one row per task, or nil when nothing
// downloads. A row whose files have all arrived shows a spinner while its load finishes.
func (v *ModelViewer) downloadPanel(t kit.Theme) *fairy.El {
	shown := v.downloads.shown()
	if len(shown) == 0 {
		return nil
	}
	panel := fairy.Col().Key("model-viewer.downloads").Gap(12).W(viewerDownloadPanelW).PadXY(12, 10).Radius(12).
		Color(t.PopoverForeground).Border(1, t.Border).Shadow(0, 8, 24, 0, shadowColor(&t)).
		Label(T("page.tools.model_viewer.asset_downloads"))
	for _, p := range shown {
		icon, label := lucide.PersonStanding, T("page.tools.model_viewer.menu.character_motion")
		if p.task == downloadOriginal {
			icon, label = lucide.Database, T("page.tools.model_viewer.menu.original_data")
		}
		value := 1.0
		if p.total > 0 {
			value = float64(p.done) / float64(p.total)
		}
		status := kit.Spinner(12)
		if p.done < p.total {
			status = fairy.Text(strconv.Itoa(int(value*100)) + "%").FontSize(12).LineHeight(16).Medium().
				Color(t.MutedForeground)
		}
		panel.Child(fairy.Col(
			fairy.Row(
				fairy.Icon(icon).Size(14, 14).Shrink(0),
				fairy.Text(label).FontSize(12).LineHeight(16).Medium().Ellipsis().MinW(0).Grow(1),
				status.Shrink(0),
			).Gap(6).Align(fairy.Center),
			kit.Progress(value),
			fairy.Text(formatSize(p.done)+" / "+formatSize(p.total)).FontSize(11).LineHeight(14).
				Color(t.MutedForeground),
		).Gap(6))
	}
	return glass(panel, opacity(t.Popover, t.SurfaceAlpha), t.Popover, t.SurfaceBlur)
}
