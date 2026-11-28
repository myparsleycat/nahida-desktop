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

// viewerDownloadPostGap is the shortest time between the rebuilds download progress asks for.
const viewerDownloadPostGap = 100 * time.Millisecond

// viewerLoadShowDelay is how long a load that downloads nothing runs before its chip shows, so that a quick load from
// the cache does not flash one.
const viewerLoadShowDelay = 300 * time.Millisecond

// viewerDownloads holds the running loads of character data and the progress of the files they read. The loads'
// goroutines report into it; the UI thread begins and ends the loads and reads it.
type viewerDownloads struct {
	mu         sync.Mutex
	tasks      [downloadTasks]viewerDownloadState
	posted     time.Time
	generation uint64
}

// viewerDownloadState is a task's running loads and their files by name. fetched is set once a file comes from the
// network rather than the cache. reveal asks for the rebuild that shows the chip once viewerLoadShowDelay has passed
// since started.
type viewerDownloadState struct {
	runs    int
	files   map[string]viewerDownloadFile
	fetched bool
	started time.Time
	reveal  *time.Timer
}

type viewerDownloadFile struct {
	done, size int64
}

// viewerDownloadRun is one load of a task. It reports its files and ends once, on the UI thread.
type viewerDownloadRun struct {
	d          *viewerDownloads
	task       viewerDownloadTask
	post       func(func())
	generation uint64
}

func (d *viewerDownloads) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.tasks {
		if s := &d.tasks[i]; s.reveal != nil {
			s.reveal.Stop()
		}
	}
	d.tasks = [downloadTasks]viewerDownloadState{}
	d.posted = time.Time{}
	d.generation++
}

// begin starts a load of task, whose progress rebuilds through post. The task's files are cleared when no other
// load of it runs.
func (d *viewerDownloads) begin(task viewerDownloadTask, post func(func())) viewerDownloadRun {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := &d.tasks[task]
	if s.runs == 0 {
		s.files, s.fetched, s.started = map[string]viewerDownloadFile{}, false, time.Now()
		if post != nil {
			s.reveal = time.AfterFunc(viewerLoadShowDelay, func() { post(func() {}) })
		}
	}
	s.runs++
	return viewerDownloadRun{d: d, task: task, post: post, generation: d.generation}
}

func (r viewerDownloadRun) end() {
	if r.d == nil {
		return
	}
	r.d.mu.Lock()
	defer r.d.mu.Unlock()
	if r.generation != r.d.generation {
		return
	}
	s := &r.d.tasks[r.task]
	s.runs = max(s.runs-1, 0)
	if s.runs == 0 && s.reveal != nil {
		s.reveal.Stop()
		s.reveal = nil
	}
}

// expect counts a file the load will read before it starts, so the total does not grow as downloads wait their turn.
func (r viewerDownloadRun) expect(file string, size int64) {
	if r.d == nil || file == "" {
		return
	}
	r.d.mu.Lock()
	defer r.d.mu.Unlock()
	if r.generation != r.d.generation {
		return
	}
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
	if s.runs == 0 || r.generation != r.d.generation {
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

// viewerDownloadProgress is what the panel shows of a task: done of total bytes while downloading is set.
type viewerDownloadProgress struct {
	task        viewerDownloadTask
	downloading bool
	done, total int64
}

// width is the width of the chip that shows p.
func (p viewerDownloadProgress) width() float32 {
	if p.downloading {
		return viewerDownloadChipWidth
	}
	return viewerLoadChipWidth
}

// shown lists the tasks with a load running that downloads a file or has run for viewerLoadShowDelay.
func (d *viewerDownloads) shown() []viewerDownloadProgress {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	var out []viewerDownloadProgress
	for task := range downloadTasks {
		s := &d.tasks[task]
		if s.runs == 0 || !s.fetched && now.Sub(s.started) < viewerLoadShowDelay {
			continue
		}
		p := viewerDownloadProgress{task: task, downloading: s.fetched}
		for _, file := range s.files {
			p.done += file.done
			p.total += file.size
		}
		out = append(out, p)
	}
	return out
}

// downloadChip shows the load of p as the task's icon inside a spinner. While the load downloads, a progress ring
// takes the spinner's place beside the percentage, with the sizes in the tooltip, until its files have all arrived.
func downloadChip(t kit.Theme, p viewerDownloadProgress) *fairy.El {
	icon, label := lucide.PersonStanding, T("page.tools.model_viewer.menu.character_motion")
	if p.task == downloadOriginal {
		icon, label = lucide.Database, T("page.tools.model_viewer.menu.original_data")
	}

	value := 1.0
	if p.total > 0 {
		value = float64(p.done) / float64(p.total)
	}

	const ring = viewerStatusRingSize
	var indicator *fairy.El
	if p.downloading && p.done < p.total {
		indicator = kit.ProgressCircle(value, ring, kit.Icon(icon), kit.Label(label)).Color(t.Primary)
	} else {
		indicator = kit.IconSpinner(icon, ring, kit.Label(label))
	}

	chip := fairy.Row(indicator).Key("model-viewer.download."+strconv.Itoa(int(p.task))).W(p.width()).Shrink(0).
		Gap(viewerStatusChipGap).Align(fairy.Center).H(viewerStatusChipHeight).PadXY(viewerStatusChipPadX, 0)
	if !p.downloading {
		return chip.Tooltip(label)
	}
	return chip.Child(
		fairy.Box(viewerStatusText(strconv.Itoa(int(value*100)) + "%").Color(t.MutedForeground)).
			W(viewerStatusPercentWidth).Align(fairy.End),
	).Tooltip(label + "\n" + formatSize(p.done) + " / " + formatSize(p.total))
}

func viewerStatusText(text string) *fairy.El {
	return fairy.Text(text).FontSize(12).LineHeight(16).Medium().NoWrap()
}
