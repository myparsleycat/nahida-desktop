package ui

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/keyboard"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/transfer"
)

// shareRootID is the id the drive service lists the shared drives under.
const shareRootID = "share"

// The refresh intervals of an open folder, as the React screen polled it: often while a recent file is still on
// its way to object storage, seldom while the window is hidden, never while the server is down.
const (
	driveRefreshInterval        = 30 * time.Second
	driveRefreshStoringInterval = 5 * time.Second
	driveRefreshHiddenInterval  = 180 * time.Second
	driveStoringWindow          = 10 * time.Minute
	driveLoadRetries            = 3
	driveSearchDebounce         = 300 * time.Millisecond
	driveSearchPageSize         = 50
	driveSearchMinLength        = 2
	driveTypeSelectTimeout      = 500 * time.Millisecond
)

// driveItem is one file or folder of a listing, read from the server's content record.
type driveItem struct {
	ID       string
	Name     string
	IsDir    bool
	Size     int64
	HasSize  bool
	MimeType string
	ParentID string
	Created  time.Time
	Updated  time.Time
	// Thumb, Cover and Image are the preview image URLs, Video a preview video URL.
	Thumb, Cover, Image, Video string
	// Storing marks a file the server holds but has not put into object storage yet: it has no preview and
	// cannot be downloaded.
	Storing bool
}

func (item driveItem) hasPreview() bool { return item.Image != "" || item.Video != "" }

// driveAncestor is a folder above the listed one. Root marks the top folder, whose parentId is null.
type driveAncestor struct {
	ID, ParentID, Name string
	Root               bool
}

type driveListing struct {
	content   driveItem
	children  []driveItem
	ancestors []driveAncestor
	// parent is the listed folder's parent, nil at the top.
	parent *driveAncestor
}

// driveView is the state of the drive or the shared drive screen.
type driveView struct {
	share bool
	// folder is the requested folder id; "" opens the drive root or the shared drives.
	folder  string
	listing *driveListing
	// listed is the folder id the listing belongs to.
	listed  string
	loading bool
	loadErr error
	// seq invalidates loads and refresh timers started for an earlier folder or listing.
	seq    int
	active bool
	sorted []driveItem
	// sortedFor is the listing, sort and policy sorted holds.
	sortedFor string
	columns   driveColumns

	selected []string
	anchor   int
	// reveal is the item to select and scroll to once the folder shows, as when going up to a parent.
	reveal string

	search        string
	searchApplied string
	searchSeq     int
	results       []driveItem
	resultsFor    string
	resultsCursor string
	resultsMore   bool
	resultsBusy   bool
	resultsErr    error

	typeBuffer string
	typeAt     time.Time
	pickBusy   bool
	// refreshPaused is set when the server went down, which stops the periodic refresh until it is back.
	refreshPaused bool
}

// refreshDrives loads the open folders again: when the server is back after the refresh paused, or on
// window focus, as React refetched its drive queries then.
func (s *Shell) refreshDrives(onlyPaused bool) {
	for _, v := range []*driveView{&s.drive.drive, &s.drive.share} {
		if !v.active || (onlyPaused && !v.refreshPaused) || v.loading {
			continue
		}
		v.refreshPaused = false
		s.loadDriveFolder(v)
	}
}

// driveClipboard holds the items cut or copied for pasting into another folder.
type driveClipboard struct {
	cut   bool
	items []driveItem
}

type driveScreen struct {
	drive, share driveView

	grid           bool
	sortField      string
	sortDesc       bool
	includeSubdirs bool
	clipboard      driveClipboard
	pasting        bool
	// uploads are the last seen statuses of upload transfers, to refresh a folder when one into it completes.
	uploads map[string]transfer.Status

	rename   *driveRename
	mkdir    *driveMkdir
	conflict *driveConflict
	preview  *driveItem
	importer *driveImport
	videos   driveVideoFiles
}

type driveRename struct {
	item      driveItem
	name, ext string
	touched   bool
	busy      bool
}

type driveMkdir struct {
	view    *driveView
	folder  string
	name    string
	touched bool
	busy    bool
}

// driveConflict asks how to upload items whose names already exist in the folder.
type driveConflict struct {
	names  []string
	answer func(strategy drive.UploadConflictStrategy)
}

func newDriveScreen() driveScreen {
	return driveScreen{
		drive:     driveView{anchor: -1},
		share:     driveView{share: true, anchor: -1},
		sortField: "name",
		uploads:   map[string]transfer.Status{},
	}
}

func (s *Shell) driveView(route Route) *driveView {
	if route == RouteShare {
		return &s.drive.share
	}
	return &s.drive.drive
}

// driveFolder is the folder id the view lists: the user's drive root or the shared drives when none is open.
func (s *Shell) driveFolder(v *driveView) string {
	if v.share {
		if v.folder == "" || v.folder == "root" {
			return shareRootID
		}
		return v.folder
	}
	if v.folder == "" || v.folder == "root" {
		if s.session != nil {
			return s.session.Drive.RootID
		}
		return ""
	}
	return v.folder
}

// navigateDrive opens a /drive/drive/<id> or /drive/share/<id> path.
func (s *Shell) navigateDrive(route Route, path string) {
	id := strings.Trim(strings.TrimPrefix(path, string(route)), "/")
	id, _, _ = strings.Cut(id, "/")
	s.openDriveFolder(s.driveView(route), id, "")
}

// openDriveFolder lists folder id, selecting reveal once it shows.
func (s *Shell) openDriveFolder(v *driveView, id, reveal string) {
	if id == "root" {
		id = ""
	}
	same := s.driveFolder(v) == s.driveFolderOf(v, id)
	v.folder, v.reveal = id, reveal
	if same {
		if reveal != "" {
			s.revealDriveItem(v)
		}
		return
	}
	v.selected, v.anchor = nil, -1
	v.search, v.searchApplied, v.results, v.resultsFor, v.resultsBusy = "", "", nil, "", false
	s.loadDriveFolder(v)
}

func (s *Shell) driveFolderOf(v *driveView, id string) string {
	probe := *v
	probe.folder = id
	return s.driveFolder(&probe)
}

// showDrive loads the folder when the screen appears, and again when it comes back, as the React query refetched
// a folder on mount.
func (s *Shell) showDrive(v *driveView) {
	if v.active {
		return
	}
	v.active = true
	s.loadDriveFolder(v)
}

// hideDrives stops the refresh of the drive screens the window no longer shows.
func (s *Shell) hideDrives(route Route) {
	if route != RouteDrive {
		s.drive.drive.active = false
	}
	if route != RouteShare {
		s.drive.share.active = false
	}
}

// loadDriveFolder lists the view's folder in the background, retrying a failed read three times unless the server
// is known to be down. The current listing stays until the new one arrives.
func (s *Shell) loadDriveFolder(v *driveView) {
	id := s.driveFolder(v)
	if id == "" || s.deps.Drive == nil {
		return
	}
	v.seq++
	seq := v.seq
	v.loading, v.loadErr = true, nil
	down := s.backendDown()
	go func() {
		var (
			raw any
			err error
		)
		for attempt := 0; ; attempt++ {
			raw, err = s.deps.Drive.GetItem(context.Background(), id)
			if err == nil || down || attempt >= driveLoadRetries {
				break
			}
			time.Sleep(min(time.Second<<attempt, 30*time.Second))
		}
		fairy.Go(func() {
			if v.seq != seq {
				return
			}
			v.loading = false
			if err != nil {
				v.loadErr = s.reportDrive(err, "list", map[string]any{"folderId": id})
				return
			}
			v.listing, v.listed, v.sortedFor = parseDriveListing(raw), id, ""
			s.revealDriveItem(v)
			s.scheduleDriveRefresh(v, seq)
		})
	}()
}

func (s *Shell) scheduleDriveRefresh(v *driveView, seq int) {
	if !v.active {
		return
	}
	// While the server is down the listing waits for driveBackendChanged, as React stopped polling then.
	if s.backendDown() {
		v.refreshPaused = true
		return
	}
	interval := driveRefreshInterval
	switch {
	case s.win != nil && (s.win.IsMinimized() || !s.win.IsVisible()):
		interval = driveRefreshHiddenInterval
	case v.listing != nil && hasStoringContent(v.listing.children, time.Now()):
		interval = driveRefreshStoringInterval
	}
	time.AfterFunc(interval, func() {
		fairy.Go(func() {
			if v.seq == seq && v.active {
				s.loadDriveFolder(v)
			}
		})
	})
}

// hasStoringContent reports whether the listing shows a file still on its way to object storage that appeared
// recently enough to watch closely. A file without a readable date counts as new.
func hasStoringContent(items []driveItem, now time.Time) bool {
	return slices.ContainsFunc(items, func(item driveItem) bool {
		return item.Storing && (item.Created.IsZero() || now.Sub(item.Created) < driveStoringWindow)
	})
}

// driveTransfersChanged refreshes an open folder that an upload just finished into, as the React screen did.
func (s *Shell) driveTransfersChanged(snapshots []transfer.Snapshot) {
	d := &s.drive
	previous := d.uploads
	d.uploads = make(map[string]transfer.Status, len(snapshots))
	for _, item := range snapshots {
		if item.Type != "upload" {
			continue
		}
		d.uploads[item.PID] = item.Status
		if item.CurrentID == "" || item.Status != transfer.StatusCompleted ||
			previous[item.PID] == transfer.StatusCompleted {
			continue
		}
		for _, v := range []*driveView{&d.drive, &d.share} {
			if v.active && s.driveFolder(v) == item.CurrentID {
				s.loadDriveFolder(v)
			}
		}
	}
}

// reloadDrives lists the open folders again after a change, and drops search results that may be stale.
func (s *Shell) reloadDrives() {
	for _, v := range []*driveView{&s.drive.drive, &s.drive.share} {
		v.resultsFor = ""
		if v.active {
			s.loadDriveFolder(v)
			s.searchDrive(v)
		}
	}
}

// driveSorted is the listing's children, folders first, in the chosen order.
func (s *Shell) driveSorted(v *driveView) []driveItem {
	if v.listing == nil {
		return nil
	}
	policy := "natural_ignore_spacing"
	if s.settingsLoaded(setting.KeyDriveNameSortPolicy) &&
		s.stringSetting(setting.KeyDriveNameSortPolicy) == "natural" {
		policy = "natural"
	}
	key := v.listed + "\x00" + s.drive.sortField + "\x00" + policy
	if s.drive.sortDesc {
		key += "\x00desc"
	}
	if key == v.sortedFor {
		return v.sorted
	}
	v.sorted = sortDriveItems(
		v.listing.children,
		s.drive.sortField,
		s.drive.sortDesc,
		policy,
		platform.NewNaturalCompare(),
	)
	v.sortedFor = key
	return v.sorted
}

// sortDriveItems orders folders before files, then by field. Names compare naturally, and first without white
// space under the natural_ignore_spacing policy.
func sortDriveItems(
	items []driveItem,
	field string,
	desc bool,
	policy string,
	compare func(a, b string) int,
) []driveItem {
	sorted := slices.Clone(items)
	sign := 1
	if desc {
		sign = -1
	}
	slices.SortStableFunc(sorted, func(a, b driveItem) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		switch field {
		case "size":
			return sign * cmp.Compare(a.Size, b.Size)
		case "date":
			return sign * a.Updated.Compare(b.Updated)
		}
		if policy == "natural_ignore_spacing" {
			if c := compare(stripSpaces(a.Name), stripSpaces(b.Name)); c != 0 {
				return sign * c
			}
		}
		return sign * compare(a.Name, b.Name)
	})
	return sorted
}

func stripSpaces(text string) string {
	return strings.Join(strings.Fields(text), "")
}

// driveSubdirSearch reports whether the search looks in subfolders. The shared drives list has none to search.
func (s *Shell) driveSubdirSearch(v *driveView) bool {
	return s.drive.includeSubdirs && s.driveFolder(v) != shareRootID
}

// driveDisplay is what the folder shows: the subfolder search results, or the sorted children filtered by the
// in-folder search.
func (s *Shell) driveDisplay(v *driveView) []driveItem {
	if s.driveSubdirSearch(v) {
		if len([]rune(strings.TrimSpace(v.search))) >= driveSearchMinLength {
			if v.resultsFor == "" {
				return nil
			}
			return v.results
		}
		return s.driveSorted(v)
	}
	sorted := s.driveSorted(v)
	query := strings.ToLower(v.search)
	if query == "" {
		return sorted
	}
	type scored struct {
		item  driveItem
		score int
	}
	matches := make([]scored, 0, len(sorted))
	for _, item := range sorted {
		if score := searchScore(newSearchIndex(item.Name), query); score > 0 {
			matches = append(matches, scored{item, score})
		}
	}
	slices.SortStableFunc(matches, func(a, b scored) int { return b.score - a.score })
	out := make([]driveItem, len(matches))
	for i, m := range matches {
		out[i] = m.item
	}
	return out
}

// setDriveSearch changes the search text and, in subfolder mode, searches after a short pause.
func (s *Shell) setDriveSearch(v *driveView, text string) {
	v.search = text
	v.searchSeq++
	seq := v.searchSeq
	time.AfterFunc(driveSearchDebounce, func() {
		fairy.Go(func() {
			if v.searchSeq == seq {
				s.searchDrive(v)
			}
		})
	})
}

// searchDrive runs the subfolder search for the current text, unless it already ran for it.
func (s *Shell) searchDrive(v *driveView) {
	query := strings.TrimSpace(v.search)
	if !s.driveSubdirSearch(v) || len([]rune(query)) < driveSearchMinLength {
		v.searchApplied = ""
		return
	}
	folder := s.driveFolder(v)
	key := folder + "\x00" + query
	if v.resultsFor == key {
		return
	}
	v.searchApplied, v.results, v.resultsFor, v.resultsCursor, v.resultsMore, v.resultsErr = query, nil, "", "", false, nil
	s.fetchDriveResults(v, folder, query, "")
}

func (s *Shell) fetchDriveResults(v *driveView, folder, query, cursor string) {
	key := folder + "\x00" + query
	v.resultsBusy = true
	go func() {
		raw, err := s.deps.Drive.Search(context.Background(), folder, drive.SearchParams{
			Q: query, Limit: driveSearchPageSize, Cursor: cursor,
		})
		fairy.Go(func() {
			if v.searchApplied != query || s.driveFolder(v) != folder {
				return
			}
			v.resultsBusy = false
			if err != nil {
				err = s.reportDrive(err, "search", map[string]any{"folderId": folder})
				if cursor != "" {
					s.toasts.Show(kit.ToastError, T("page.drive.title"), driveSearchError(err))
					return
				}
				v.resultsErr, v.resultsFor = err, key
				return
			}
			items, next := parseDriveSearch(raw)
			if cursor == "" {
				v.results = items
			} else {
				v.results = append(v.results, items...)
			}
			v.resultsFor, v.resultsCursor, v.resultsMore = key, next, next != ""
		})
	}()
}

// driveSearchError names a failed subfolder search, as the search service being down or the raw message.
func driveSearchError(err error) string {
	var apiErr *drive.DriveAPIError
	if errors.As(err, &apiErr) && (apiErr.Status == 502 || apiErr.Status == 503 || apiErr.Status == 504 ||
		apiErr.Code == "DRIVE_BACKEND_UNAVAILABLE") {
		return T("page.drive.head_buttons.search_unavailable")
	}
	if strings.Contains(err.Error(), "DRIVE_BACKEND_UNAVAILABLE") {
		return T("page.drive.head_buttons.search_unavailable")
	}
	return err.Error()
}

// Selection.

func (v *driveView) isSelected(id string) bool { return slices.Contains(v.selected, id) }

// selectedItems returns the selected items of items in selection order.
func (v *driveView) selectedItems(items []driveItem) []driveItem {
	out := make([]driveItem, 0, len(v.selected))
	for _, id := range v.selected {
		if i := slices.IndexFunc(items, func(item driveItem) bool { return item.ID == id }); i >= 0 {
			out = append(out, items[i])
		}
	}
	return out
}

// clickDriveItem selects as the React list did: Shift selects the range from the last clicked item, adding to the
// selection with Ctrl; Ctrl toggles the item; a plain click selects it alone.
func clickDriveItem(v *driveView, items []driveItem, index int, mods keyboard.Mods) {
	item := items[index]
	shift, ctrl := mods&keyboard.ModShift != 0, mods&keyboard.ModCtrl != 0
	switch {
	case shift && v.anchor >= 0 && v.anchor < len(items):
		lo, hi := min(v.anchor, index), max(v.anchor, index)
		var picked []string
		if ctrl {
			picked = slices.Clone(v.selected)
		}
		for _, it := range items[lo : hi+1] {
			if !slices.Contains(picked, it.ID) {
				picked = append(picked, it.ID)
			}
		}
		v.selected = picked
	case ctrl:
		if i := slices.Index(v.selected, item.ID); i >= 0 {
			v.selected = slices.Delete(slices.Clone(v.selected), i, i+1)
		} else {
			v.selected = append(slices.Clone(v.selected), item.ID)
		}
		v.anchor = index
	default:
		v.selected, v.anchor = []string{item.ID}, index
	}
}

func (s *Shell) selectDriveIndex(v *driveView, items []driveItem, index int) {
	v.selected, v.anchor = []string{items[index].ID}, index
	if s.win != nil && !s.drive.grid {
		s.win.ScrollToRow(driveListKey+v.listed, index)
	}
}

// revealDriveItem selects the pending reveal item once it is in the listing.
func (s *Shell) revealDriveItem(v *driveView) {
	if v.reveal == "" || v.listing == nil || v.listed != s.driveFolder(v) {
		return
	}
	items := s.driveDisplay(v)
	if i := slices.IndexFunc(items, func(item driveItem) bool { return item.ID == v.reveal }); i >= 0 {
		v.reveal = ""
		v.selected, v.anchor = []string{items[i].ID}, i
		if s.win != nil && !s.drive.grid {
			s.win.ScrollToRowInstant(driveListKey+v.listed, i)
		}
	}
}

// Navigation.

// driveAncestors is the path to the listed folder, starting at the drive or the shared drives.
func (s *Shell) driveAncestors(v *driveView) []driveAncestor {
	var ancestors []driveAncestor
	if v.listing != nil {
		ancestors = v.listing.ancestors
	}
	crumbs := make([]driveAncestor, 0, len(ancestors)+1)
	switch {
	case v.share:
		crumbs = append(crumbs, driveAncestor{ID: shareRootID, Name: T("page.drive.share_drive"), Root: true})
	case len(ancestors) > 0 && ancestors[0].Root:
		crumbs = append(crumbs, driveAncestor{ID: ancestors[0].ID, Name: T("page.drive.title"), Root: true})
		ancestors = ancestors[1:]
	default:
		root := ""
		if s.session != nil {
			root = s.session.Drive.RootID
		}
		crumbs = append(crumbs, driveAncestor{ID: root, Name: T("page.drive.title"), Root: true})
	}
	return append(crumbs, ancestors...)
}

// openDriveCrumb opens a folder on the current path, selecting the folder the user came from.
func (s *Shell) openDriveCrumb(v *driveView, crumbs []driveAncestor, id string) {
	reveal := ""
	if i := slices.IndexFunc(crumbs, func(c driveAncestor) bool { return c.ID == id }); i >= 0 && i+1 < len(crumbs) {
		reveal = crumbs[i+1].ID
	}
	s.openDriveFolder(v, id, reveal)
}

// driveBack opens the parent of the listed folder.
func (s *Shell) driveBack(v *driveView, crumbs []driveAncestor) {
	current := crumbs[len(crumbs)-1]
	parent := current.ParentID
	if current.Root || parent == "" {
		parent = s.driveFolderOf(v, "")
	}
	if parent != "" {
		s.openDriveCrumb(v, crumbs, parent)
	}
}

// driveUp opens the parent folder with Ctrl+Up, selecting the folder left.
func (s *Shell) driveUp(v *driveView) {
	if v.listing == nil || v.listing.parent == nil {
		s.toasts.Show(kit.ToastWarning, T("page.drive.toast.no_parent"), "")
		return
	}
	parent := v.listing.parent.ID
	if v.share && v.listing.parent.Root {
		parent = shareRootID
	}
	s.openDriveFolder(v, parent, s.driveFolder(v))
}

// openDriveItem opens a folder or downloads a file. Text files did nothing in the React screen, whose text viewer
// was never finished.
func (s *Shell) openDriveItem(v *driveView, item driveItem) {
	switch {
	case item.IsDir:
		s.openDriveFolder(v, item.ID, "")
	case strings.HasPrefix(item.MimeType, "text"):
	default:
		s.downloadDriveItems([]driveItem{item})
	}
}

// Actions.

func (s *Shell) reportDrive(err error, action string, fields map[string]any) error {
	return infra.ReportError(s.deps.Log, err, "Drive", infra.Diagnostic{Operation: "drive." + action, Fields: fields})
}

func (s *Shell) driveFailed(title string, err error, action string, fields map[string]any) {
	err = s.reportDrive(err, action, fields)
	s.toasts.Show(kit.ToastError, title, err.Error())
}

var driveCodePattern = regexp.MustCompile(`\b(DRIVE_[A-Z0-9_]+)\b`)

// driveErrorCode is the domain code of a drive failure, from the error or its message.
func driveErrorCode(err error) string {
	var apiErr *drive.DriveAPIError
	if errors.As(err, &apiErr) && apiErr.Code != "" {
		return apiErr.Code
	}
	if match := driveCodePattern.FindStringSubmatch(err.Error()); match != nil {
		return match[1]
	}
	return ""
}

// isDriveNotFound reports a link or item that expired or no longer exists.
func isDriveNotFound(err error) bool {
	code := driveErrorCode(err)
	message := strings.ToLower(err.Error())
	return strings.Contains(code, "NOT_FOUND") || strings.Contains(code, "EXPIRED") ||
		strings.Contains(message, "not found") || strings.Contains(message, "expired") ||
		strings.Contains(message, "404")
}

// downloadDriveItems downloads items, leaving out files not yet in object storage.
func (s *Shell) downloadDriveItems(items []driveItem) {
	stored := slices.DeleteFunc(slices.Clone(items), func(item driveItem) bool { return item.Storing })
	if len(stored) < len(items) {
		s.toasts.Show(kit.ToastInfo, T("page.drive.storing_download_excluded"), "")
	}
	if len(stored) == 0 {
		return
	}
	params := drive.StartDownloadParams{Source: "drive"}
	ids := make([]string, 0, len(stored))
	for _, item := range stored {
		download := drive.DownloadItem{ID: item.ID, IsDir: item.IsDir, Name: item.Name}
		if item.HasSize {
			download.Size = &item.Size
		}
		params.Items = append(params.Items, download)
		ids = append(ids, item.ID)
	}
	go func() {
		_, err := s.deps.Drive.StartDownload(context.Background(), params)
		if err != nil && !infra.IsCancellationError(err) {
			fairy.Go(func() {
				s.driveFailed(T("page.drive.context_menu.download"), err, "download", map[string]any{"ids": ids})
			})
		}
	}()
}

// pickDriveUpload asks for files or a folder and uploads them into the listed folder.
func (s *Shell) pickDriveUpload(v *driveView, directory bool) {
	folder := s.driveFolder(v)
	if !canUploadTo(folder) || v.pickBusy {
		return
	}
	v.pickBusy = true
	title, property := T("page.drive.head_buttons.dropdown_menu.make_new.upload_file"), "openFile"
	if directory {
		title, property = T("page.drive.head_buttons.dropdown_menu.make_new.upload_dir"), "openDirectory"
	}
	go func() {
		result, err := s.deps.Dialog.ShowOpenDialog(platform.OpenDialogOptions{
			Title: title, Properties: []string{property, "multiSelections"}, Role: "drive-upload",
		})
		fairy.Go(func() {
			v.pickBusy = false
			switch {
			case err != nil:
				s.driveFailed(title, err, "pick-upload", map[string]any{"folderId": folder})
			case !result.Canceled && len(result.FilePaths) > 0:
				s.uploadDrivePaths(folder, result.FilePaths)
			}
		})
	}()
}

func canUploadTo(folder string) bool {
	return folder != "" && folder != shareRootID && folder != "root"
}

// uploadDrivePaths uploads local paths into folder, asking first about files the drive does not accept and about
// names that already exist there.
func (s *Shell) uploadDrivePaths(folder string, paths []string) {
	if len(paths) == 0 || !canUploadTo(folder) {
		return
	}
	fields := map[string]any{"folderId": folder, "paths": paths}
	go func() {
		check, err := s.deps.Drive.GetUploadConflicts(context.Background(), drive.GetUploadConflictsParams{
			DestID: folder, Paths: paths,
		})
		fairy.Go(func() {
			if err != nil {
				s.driveUploadFailed(err, "upload-check", fields)
				return
			}
			start := func(strategy drive.UploadConflictStrategy) {
				go func() {
					_, err := s.deps.Drive.StartUpload(context.Background(), drive.StartUploadParams{
						DestID: folder, Paths: check.SelectedPaths, ConflictStrategy: strategy,
					})
					if err != nil {
						fairy.Go(func() { s.driveUploadFailed(err, "upload", fields) })
					}
				}()
			}
			askConflicts := func() {
				if len(check.Conflicts) == 0 {
					start(drive.UploadConflictSuffix)
					return
				}
				s.drive.conflict = &driveConflict{names: check.Conflicts, answer: start}
			}
			if len(check.SkippedExtensions) == 0 {
				askConflicts()
				return
			}
			s.Confirm(Confirmation{
				Title:        T("page.drive.dialog.unsupported_extensions.title"),
				Description:  T("page.drive.dialog.unsupported_extensions.description"),
				Body:         func() *fairy.El { return driveExtensionList(check.SkippedExtensions) },
				ConfirmLabel: T("page.drive.dialog.unsupported_extensions.action_proceed"),
				OnResult: func(confirmed bool) {
					if confirmed {
						askConflicts()
					}
				},
			})
		})
	}()
}

func (s *Shell) driveUploadFailed(err error, action string, fields map[string]any) {
	if strings.Contains(err.Error(), "NO_UPLOADABLE_FILES") {
		s.toasts.Show(kit.ToastWarning, T("page.drive.toast.no_uploadable_files"), "")
		return
	}
	s.driveFailed(T("page.drive.head_buttons.dropdown_menu.make_new.upload_file"), err, action, fields)
}

// openDriveRename starts renaming item, with the extension of a file in its own field.
func (s *Shell) openDriveRename(item driveItem) {
	r := &driveRename{item: item, name: item.Name}
	if dot := strings.LastIndex(item.Name, "."); !item.IsDir && dot >= 0 {
		r.name, r.ext = item.Name[:dot], item.Name[dot:]
	}
	s.drive.rename = r
}

func driveRenameError(r *driveRename) string {
	if strings.TrimSpace(r.name) == "" {
		return T("page.drive.dialog.rename.#.0")
	}
	return driveNameError(r.name + r.ext)
}

// driveNameError checks a name the way the React ValidateName did before it reached the server.
func driveNameError(name string) string {
	const prefix = "page.drive.dialog.common."
	switch {
	case strings.HasPrefix(name, " "):
		return T(prefix + "name_leading_space")
	case strings.HasSuffix(name, " "):
		return T(prefix + "name_trailing_space")
	case utf8.RuneCountInString(name) > 255:
		return T(prefix + "name_too_long")
	}
	return ""
}

func (s *Shell) submitDriveRename() {
	r := s.drive.rename
	if r == nil || r.busy {
		return
	}
	r.touched = true
	if driveRenameError(r) != "" {
		return
	}
	r.busy = true
	name, item := r.name+r.ext, r.item
	go func() {
		_, err := s.deps.Drive.Rename(context.Background(), item.ID, name)
		fairy.Go(func() {
			r.busy = false
			if err != nil {
				s.driveNameFailed(err, item.IsDir, T("page.drive.dialog.rename.title"), "rename",
					map[string]any{"id": item.ID, "name": name})
				return
			}
			if s.drive.rename == r {
				s.drive.rename = nil
			}
			s.reloadDrives()
		})
	}()
}

func (s *Shell) driveNameFailed(err error, dir bool, title, action string, fields map[string]any) {
	if errors.Is(err, platform.ErrInvalidWindowsFilename) || strings.Contains(err.Error(), "INVALID_WINDOWS_FILENAME") {
		key := "page.drive.dialog.common.invalid_file_name"
		if dir {
			key = "page.drive.dialog.common.invalid_dir_name"
		}
		s.toasts.Show(kit.ToastWarning, T(key), "")
		return
	}
	s.driveFailed(title, err, action, fields)
}

func (s *Shell) openDriveMkdir(v *driveView) {
	s.drive.mkdir = &driveMkdir{view: v, folder: s.driveFolder(v)}
}

func (s *Shell) driveMkdirError(m *driveMkdir) string {
	if strings.TrimSpace(m.name) == "" {
		return T("page.drive.dialog.create_dir.#.0")
	}
	if err := driveNameError(m.name); err != "" {
		return err
	}
	if m.view.listing != nil && slices.ContainsFunc(m.view.listing.children, func(item driveItem) bool {
		return item.IsDir && item.Name == m.name
	}) {
		return T("page.drive.dialog.create_dir.#.2")
	}
	return ""
}

func (s *Shell) submitDriveMkdir() {
	m := s.drive.mkdir
	if m == nil || m.busy {
		return
	}
	m.touched = true
	if s.driveMkdirError(m) != "" {
		return
	}
	m.busy = true
	name, folder := m.name, m.folder
	go func() {
		_, err := s.deps.Drive.CreateDir(context.Background(), folder, name)
		fairy.Go(func() {
			m.busy = false
			if err != nil {
				s.driveNameFailed(err, true, T("page.drive.dialog.create_dir.title"), "create-dir",
					map[string]any{"folderId": folder, "name": name})
				return
			}
			if s.drive.mkdir == m {
				s.drive.mkdir = nil
			}
			s.reloadDrives()
		})
	}()
}

// deleteDriveItems deletes the items after asking in the drive, and moves them to the trash at once in the
// shared drive, as the React context menu did.
func (s *Shell) deleteDriveItems(v *driveView, items []driveItem) {
	if len(items) == 0 {
		return
	}
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	run := func(action string) {
		outcome, err := s.deps.Drive.DeleteItems(context.Background(), ids, action)
		fairy.Go(func() { s.driveDeleted(v, ids, action, outcome, err) })
	}
	if v.share {
		go run("trash")
		return
	}
	// The dialog stays open with a busy button until the deletion ends, as the React dialog did; the outcome
	// shows as a toast.
	s.Confirm(Confirmation{
		Title:        T("page.drive.dialog.delete_items.title"),
		Description:  T("page.drive.dialog.delete_items.description"),
		ConfirmLabel: T("page.drive.dialog.delete_items.action"),
		Danger:       true,
		Run: func() error {
			run("delete")
			return nil
		},
	})
}

func (s *Shell) driveDeleted(v *driveView, ids []string, action string, outcome drive.BatchDeletionOutcome, err error) {
	title := T("page.drive.dialog.delete_items.#.toast.error")
	if action == "trash" {
		title = T("page.drive.context_menu.trash")
	}
	fields := map[string]any{"ids": ids, "action": action}
	switch {
	case err != nil:
		s.driveFailed(title, err, "delete", fields)
		return
	case len(outcome.AcceptedIDs) == 0 && action == "delete":
		message := outcome.ErrorMessage
		if message == "" {
			message = title
		}
		s.driveFailed(title, errors.New(message), "delete", fields)
		return
	case outcome.ErrorMessage != "":
		s.toasts.Show(kit.ToastWarning, T("page.drive.dialog.delete_items.#.toast.partial",
			"accepted", len(outcome.AcceptedIDs), "failed", len(outcome.RequestedIDs)-len(outcome.AcceptedIDs)),
			outcome.ErrorMessage)
	}
	s.dropDriveClipboard(outcome.AcceptedIDs)
	v.selected, v.anchor = nil, -1
	s.reloadDrives()
}

// setDriveClipboard keeps the items for pasting; cut items move, copied ones are copied.
func (s *Shell) setDriveClipboard(items []driveItem, cut bool) {
	if len(items) == 0 {
		return
	}
	s.drive.clipboard = driveClipboard{cut: cut, items: slices.Clone(items)}
}

func (s *Shell) dropDriveClipboard(ids []string) {
	c := &s.drive.clipboard
	c.items = slices.DeleteFunc(c.items, func(item driveItem) bool { return slices.Contains(ids, item.ID) })
}

// pasteDrive moves or copies the clipboard into folder, one paste at a time.
func (s *Shell) pasteDrive(folder string) {
	d := &s.drive
	if d.pasting || len(d.clipboard.items) == 0 || folder == "" || folder == shareRootID {
		return
	}
	d.pasting = true
	clip := d.clipboard
	ids := make([]string, len(clip.items))
	for i, item := range clip.items {
		ids[i] = item.ID
	}
	action, prefix := "copy", "page.drive.clipboard.copy_"
	if clip.cut {
		action, prefix = "move", "page.drive.clipboard.move_"
	}
	toast := s.toasts.Loading(T(prefix+"loading"), "")
	go func() {
		var err error
		if clip.cut {
			_, err = s.deps.Drive.MoveMany(context.Background(), ids, folder)
		} else {
			_, err = s.deps.Drive.CopyMany(context.Background(), ids, folder)
		}
		fairy.Go(func() {
			d.pasting = false
			if err != nil {
				err = s.reportDrive(err, action, map[string]any{"ids": ids, "folderId": folder})
				s.toasts.Update(toast, kit.ToastError, T(prefix+"error", "message", err.Error()), "")
				return
			}
			if clip.cut {
				d.clipboard = driveClipboard{}
			}
			s.toasts.Update(toast, kit.ToastSuccess, T(prefix+"success"), "")
			s.reloadDrives()
		})
	}()
}

// Keys.

const (
	driveListKey   = "drive.list:"
	driveSearchKey = "drive.search"
	driveAreaKey   = "drive.content"
)

func (s *Shell) driveDialogOpen() bool {
	d := &s.drive
	return d.rename != nil || d.mkdir != nil || d.conflict != nil || d.preview != nil || d.importer != nil ||
		len(s.confirms) > 0 || len(s.paths) > 0
}

// driveKey handles the drive screen's keys as the React screen's window listener did.
func (s *Shell) driveKey(v *driveView, k keyboard.KeyEvent) bool {
	if k.Is('F', keyboard.ModCtrl) {
		if !s.driveDialogOpen() && s.win != nil {
			s.win.Focus(driveSearchKey)
			s.win.EditInput(driveSearchKey, fairy.EditSelectAll)
		}
		return true
	}
	items := s.driveDisplay(v)
	if s.win != nil && s.win.Focused(driveSearchKey) {
		switch {
		case k.Is(keyboard.KeyDown, 0):
			s.win.Focus(driveAreaKey)
			if len(items) > 0 {
				s.selectDriveIndex(v, items, 0)
			}
			return true
		case k.Is(keyboard.KeyEscape, 0):
			s.win.Focus(driveAreaKey)
			s.setDriveSearch(v, "")
			return true
		}
		return false
	}
	if s.driveDialogOpen() {
		return false
	}
	folder := s.driveFolder(v)
	current := -1
	if len(v.selected) > 0 {
		current = slices.IndexFunc(items, func(item driveItem) bool { return item.ID == v.selected[0] })
	}
	ctrl := k.Mods&keyboard.ModCtrl != 0
	switch {
	case k.Is(keyboard.KeyEnter, 0):
		if len(v.selected) == 1 && current >= 0 {
			if items[current].IsDir {
				s.openDriveFolder(v, items[current].ID, "")
			} else {
				s.downloadDriveItems([]driveItem{items[current]})
			}
		}
	case k.Is(keyboard.KeyF1+1, 0):
		if len(v.selected) == 1 && current >= 0 {
			s.openDriveRename(items[current])
		}
	case k.Is(keyboard.KeyDown, keyboard.ModCtrl):
		if len(v.selected) == 1 && current >= 0 && items[current].IsDir {
			s.openDriveFolder(v, items[current].ID, "")
		}
	case k.Is(keyboard.KeyUp, keyboard.ModCtrl):
		s.driveUp(v)
	case k.Is(keyboard.KeyDown, 0):
		if len(items) > 0 {
			s.selectDriveIndex(v, items, min(current+1, len(items)-1))
		}
	case k.Is(keyboard.KeyUp, 0):
		if len(items) > 0 {
			s.selectDriveIndex(v, items, max(current-1, 0))
		}
	case k.Is(keyboard.KeyEscape, 0):
		v.selected, v.anchor = nil, -1
		s.drive.clipboard = driveClipboard{}
	case k.Is(keyboard.KeyA, keyboard.ModCtrl):
		if v.listing != nil {
			v.selected = v.selected[:0]
			for _, item := range v.listing.children {
				v.selected = append(v.selected, item.ID)
			}
		}
	case k.Is(keyboard.KeyC, keyboard.ModCtrl):
		s.setDriveClipboard(v.selectedItems(items), false)
	case k.Is(keyboard.KeyX, keyboard.ModCtrl):
		s.setDriveClipboard(v.selectedItems(items), true)
	case k.Is(keyboard.KeyV, keyboard.ModCtrl):
		s.pasteDrive(folder)
	case !ctrl && k.Mods&keyboard.ModAlt == 0 && isTypeSelectKey(k.Text):
		now := time.Now()
		if now.Sub(v.typeAt) > driveTypeSelectTimeout {
			v.typeBuffer = ""
		}
		v.typeBuffer, v.typeAt = v.typeBuffer+strings.ToLower(k.Text), now
		if i := slices.IndexFunc(items, func(item driveItem) bool {
			return strings.HasPrefix(strings.ToLower(item.Name), v.typeBuffer)
		}); i >= 0 {
			s.selectDriveIndex(v, items, i)
		}
	default:
		return false
	}
	return true
}

func isTypeSelectKey(text string) bool {
	if len(text) != 1 {
		return false
	}
	c := text[0]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// Parsing. The drive service returns the server's JSON or CBOR record as maps.

func parseDriveListing(raw any) *driveListing {
	record, _ := raw.(map[string]any)
	listing := &driveListing{}
	if content, ok := record["content"].(map[string]any); ok {
		listing.content = parseDriveItem(content)
	}
	listing.children = parseDriveItems(record["children"])
	if ancestors, ok := record["ancestors"].([]any); ok {
		for _, entry := range ancestors {
			if a, ok := parseDriveAncestor(entry); ok {
				listing.ancestors = append(listing.ancestors, a)
			}
		}
	}
	if parent, ok := parseDriveAncestor(record["parent"]); ok {
		listing.parent = &parent
	}
	return listing
}

func parseDriveSearch(raw any) ([]driveItem, string) {
	record, _ := raw.(map[string]any)
	next, _ := record["nextCursor"].(string)
	return parseDriveItems(record["items"]), next
}

func parseDriveItems(raw any) []driveItem {
	entries, _ := raw.([]any)
	items := make([]driveItem, 0, len(entries))
	for _, entry := range entries {
		if record, ok := entry.(map[string]any); ok {
			if item := parseDriveItem(record); item.ID != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

func parseDriveAncestor(raw any) (driveAncestor, bool) {
	record, ok := raw.(map[string]any)
	if !ok {
		return driveAncestor{}, false
	}
	id, _ := record["id"].(string)
	name, _ := record["name"].(string)
	parent, _ := record["parentId"].(string)
	return driveAncestor{ID: id, ParentID: parent, Name: name, Root: record["parentId"] == nil}, id != ""
}

func parseDriveItem(record map[string]any) driveItem {
	item := driveItem{}
	item.ID, _ = record["id"].(string)
	item.Name, _ = record["name"].(string)
	item.IsDir, _ = record["isDir"].(bool)
	item.MimeType, _ = record["mimeType"].(string)
	item.ParentID, _ = record["parentId"].(string)
	item.Storing, _ = record["storing"].(bool)
	item.Size, item.HasSize = asInt64(record["size"])
	item.Created = asTime(record["createdAt"])
	item.Updated = asTime(record["updatedAt"])
	if preview, ok := record["preview"].(map[string]any); ok {
		if img, ok := preview["img"].(map[string]any); ok {
			item.Image, _ = img["default"].(string)
			item.Cover, _ = img["cover"].(string)
			item.Thumb, _ = img["thumbnail"].(string)
		}
		if video, ok := preview["video"].(map[string]any); ok {
			item.Video, _ = video["default"].(string)
		}
	}
	return item
}

func asInt64(value any) (int64, bool) {
	switch n := value.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case uint64:
		return int64(min(n, uint64(1<<63-1))), true
	case int:
		return int64(n), true
	case string:
		if v, ok := parseInt(n); ok {
			return int64(v), true
		}
	}
	return 0, false
}

func asTime(value any) time.Time {
	switch v := value.(type) {
	case time.Time:
		return v
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
	}
	return time.Time{}
}
