package ui

import (
	"context"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/setting"
)

const (
	driveImportDebounce = 500 * time.Millisecond
	driveImportRow      = 32
	driveImportIndent   = 16
	driveModRootPrefix  = "mod:virtual:"
)

var driveImportPath = regexp.MustCompile(`(?i)^/akasha/(link|mod)/[A-Za-z0-9_-]+/?$`)

// driveImport is the import overlay: a nahida.live shared link or collection URL, then the folders to copy.
type driveImport struct {
	view     *driveView
	destID   string
	destName string

	url, password string
	needPassword  bool
	badPassword   bool
	resolving     bool
	// seq invalidates resolves and child loads of an earlier URL.
	seq         int
	resolvedURL string
	source      *driveImportSource

	nodes         map[string]*driveImportNode
	root          string
	expanded      map[string]bool
	selected      map[string]bool
	loadingIDs    map[string]bool
	createFolders bool
}

type driveImportSource struct {
	link   bool
	linkID string
	modID  string
	token  string
	sig    string
}

type driveImportNode struct {
	id, name, parent string
	isDir            bool
	size             int64
	children         []string
	depth            int
	loaded           bool
}

// isDriveImportURL reports a nahida.live shared link or collection URL.
func isDriveImportURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "nahida.live" || host == "www.nahida.live") && driveImportPath.MatchString(u.Path)
}

// openDriveImport opens the import overlay for the listed folder. The shared drive has no import, so it goes to the
// drive first, as the React overlay only showed there.
func (s *Shell) openDriveImport(v *driveView, initial string) {
	if v.share {
		v = &s.drive.drive
		s.navigate(RouteDrive)
	}
	dest := s.driveFolder(v)
	if dest == "" {
		return
	}
	name := ""
	if v.listing != nil && v.listed == dest {
		name = v.listing.content.Name
	}
	s.drive.importer = &driveImport{view: v, destID: dest, destName: name, createFolders: true}
	if name == "" {
		s.loadImportDestination(s.drive.importer)
	}
	s.setImportURL(initial)
}

func (s *Shell) loadImportDestination(im *driveImport) {
	go func() {
		raw, err := s.deps.Drive.GetItem(context.Background(), im.destID)
		if err != nil {
			_ = s.reportDrive(err, "import-destination", map[string]any{"folderId": im.destID})
			return
		}
		name := parseDriveListing(raw).content.Name
		fairy.Go(func() { im.destName = name })
	}()
}

func (im *driveImport) reset() {
	im.seq++
	im.resolvedURL, im.source, im.root = "", nil, ""
	im.nodes, im.expanded, im.selected, im.loadingIDs = nil, nil, nil, nil
	im.needPassword, im.badPassword, im.resolving = false, false, false
}

// setImportURL resolves a valid URL half a second after it stops changing.
func (s *Shell) setImportURL(text string) {
	im := s.drive.importer
	im.url = text
	trimmed := strings.TrimSpace(text)
	if trimmed == im.resolvedURL && s.signedIn() {
		return
	}
	im.reset()
	seq := im.seq
	if !isDriveImportURL(trimmed) || !s.signedIn() {
		return
	}
	time.AfterFunc(driveImportDebounce, func() {
		fairy.Go(func() {
			if s.drive.importer == im && im.seq == seq {
				s.resolveImport(im, seq)
			}
		})
	})
}

// resolveImport reads the link or collection behind the URL. seq is 0 when the user asked for it, which starts
// over; the password prompt stays when it was showing.
func (s *Shell) resolveImport(im *driveImport, seq int) {
	trimmed := strings.TrimSpace(im.url)
	if trimmed == "" {
		s.toasts.Show(kit.ToastWarning, T("page.drive.import.url_required"), "")
		return
	}
	if !s.signedIn() {
		s.toasts.Show(kit.ToastWarning, T("page.drive.import.login_required"), "")
		s.startLogin()
		return
	}
	keepPassword := im.needPassword
	if seq == 0 {
		im.reset()
		seq = im.seq
	}
	if seq != im.seq {
		return
	}
	im.resolving, im.needPassword = true, keepPassword
	if !keepPassword {
		im.badPassword = false
	}
	password := im.password
	go func() {
		raw, err := s.deps.Drive.ResolveImportSource(context.Background(), drive.ResolveImportSourceParams{
			URL: trimmed, Password: password,
		})
		code := ""
		if err != nil {
			code = driveErrorCode(err)
		}
		needsPassword := code == "DRIVE_LINK_PASSWORD_REQUIRED" || code == "DRIVE_MOD_PASSWORD_REQUIRED"
		var tried driveImportAttempt
		if needsPassword {
			tried = s.tryImportPasswords(trimmed)
		}
		fairy.Go(func() {
			if s.drive.importer != im || im.seq != seq {
				return
			}
			im.resolving = false
			switch {
			case err == nil:
				s.applyImportSource(im, raw, trimmed)
			case needsPassword && tried.ok:
				im.password = tried.password
				s.applyImportSource(im, tried.result, trimmed)
			case needsPassword:
				im.needPassword = true
				s.toasts.Show(kit.ToastWarning, T("page.drive.import.password_required"), "")
			case code == "DRIVE_LINK_INVALID_PASSWORD" || code == "DRIVE_MOD_INVALID_PASSWORD":
				im.needPassword, im.badPassword = true, true
				s.toasts.Show(kit.ToastError, T("page.drive.import.invalid_password"), "")
			case code == "DRIVE_INVALID_SOURCE_URL" || strings.Contains(err.Error(), "DRIVE_INVALID_SOURCE_URL"):
				s.toasts.Show(kit.ToastError, T("page.drive.import.invalid_url"),
					T("page.drive.import.invalid_url_description"))
			case isDriveNotFound(err):
				im.reset()
				im.password = ""
				s.toasts.Show(kit.ToastError, T("page.drive.import.expired"), err.Error())
			case strings.Contains(err.Error(), "DRIVE_COPY_CANCELED"):
			default:
				s.driveFailed(T("page.drive.import.failed"), err, "import-resolve", map[string]any{
					"url": infra.SanitizeLogURL(trimmed),
				})
			}
		})
	}()
}

type driveImportAttempt struct {
	ok       bool
	password string
	result   any
}

// tryImportPasswords tries the saved passwords at once when the setting allows it; the first that opens the link
// wins. It runs off the UI thread.
func (s *Shell) tryImportPasswords(rawURL string) driveImportAttempt {
	ctx := context.Background()
	values, err := s.deps.Settings.GetMany(
		ctx,
		[]string{setting.KeyDriveAutoTryPasswords, setting.KeyDrivePasswordList},
	)
	if err != nil {
		_ = s.reportDrive(err, "import-passwords", nil)
		return driveImportAttempt{}
	}
	if enabled, _ := values[setting.KeyDriveAutoTryPasswords].(bool); !enabled {
		return driveImportAttempt{}
	}
	var candidates []string
	switch list := values[setting.KeyDrivePasswordList].(type) {
	case []string:
		candidates = list
	case []any:
		for _, item := range list {
			if text, ok := item.(string); ok {
				candidates = append(candidates, text)
			}
		}
	}
	candidates = slices.DeleteFunc(candidates, func(p string) bool { return p == "" })
	if len(candidates) == 0 {
		return driveImportAttempt{}
	}
	results := make(chan driveImportAttempt, len(candidates))
	for _, candidate := range candidates {
		go func() {
			raw, err := s.deps.Drive.ResolveImportSource(ctx, drive.ResolveImportSourceParams{
				URL: rawURL, Password: candidate,
			})
			results <- driveImportAttempt{ok: err == nil, password: candidate, result: raw}
		}()
	}
	for range candidates {
		if attempt := <-results; attempt.ok {
			return attempt
		}
	}
	return driveImportAttempt{}
}

// applyImportSource builds the first level of the tree: the shared folder, or the collections of a mod.
func (s *Shell) applyImportSource(im *driveImport, raw any, resolvedURL string) {
	im.resolvedURL = resolvedURL
	im.needPassword, im.badPassword = false, false
	im.nodes, im.expanded = map[string]*driveImportNode{}, map[string]bool{}
	im.selected, im.loadingIDs = map[string]bool{}, map[string]bool{}
	switch result := raw.(type) {
	case drive.ResolveLinkResult:
		im.source = &driveImportSource{link: true, linkID: result.LinkID, token: result.Token}
		root := result.Parent.ID
		im.nodes[root] = &driveImportNode{id: root, name: result.Parent.Name, isDir: true}
		im.root, im.expanded[root] = root, true
		s.loadImportChildren(im, root)
	case drive.ResolveModResult:
		im.source = &driveImportSource{modID: result.ModID, token: result.Token, sig: result.Sig}
		root := driveModRootPrefix + result.ModID
		rootNode := &driveImportNode{id: root, name: result.ModID, isDir: true, loaded: true}
		im.nodes[root] = rootNode
		im.root, im.expanded[root] = root, true
		for _, collection := range result.ModData.Collections {
			if collection.RootID == "" {
				continue
			}
			rootNode.children = append(rootNode.children, collection.RootID)
			im.nodes[collection.RootID] = &driveImportNode{
				id: collection.RootID, name: collection.Name, parent: root, isDir: true, depth: 1,
			}
			im.expanded[collection.RootID] = true
		}
		if len(rootNode.children) == 0 {
			s.toasts.Show(kit.ToastWarning, T("page.drive.import.no_collections"), "")
		}
		for _, id := range rootNode.children {
			s.loadImportChildren(im, id)
		}
	default:
		im.source = nil
	}
}

func (s *Shell) loadImportChildren(im *driveImport, id string) {
	source, seq := im.source, im.seq
	if source == nil || im.loadingIDs[id] {
		return
	}
	im.loadingIDs[id] = true
	go func() {
		var (
			result drive.ListChildrenResult
			err    error
		)
		if source.link {
			result, err = s.deps.Drive.ListLinkChildren(context.Background(), drive.ListLinkChildrenParams{
				LinkID: source.linkID, LinkToken: source.token, ItemID: id,
			})
		} else {
			result, err = s.deps.Drive.ListModChildren(context.Background(), drive.ListModChildrenParams{
				ItemID: id, ModToken: source.token, ModSig: source.sig,
			})
		}
		fairy.Go(func() {
			if s.drive.importer != im || im.seq != seq {
				return
			}
			delete(im.loadingIDs, id)
			parent := im.nodes[id]
			switch {
			case err != nil && isDriveNotFound(err):
				key := "page.drive.import.collection_failed"
				if source.link {
					key = "page.drive.import.link_expired"
				}
				_ = s.reportDrive(err, "import-children", map[string]any{"itemId": id})
				s.toasts.Show(kit.ToastError, T(key), "")
				im.reset()
			case err != nil:
				s.driveFailed(T("page.drive.import.list_failed"), err, "import-children", map[string]any{"itemId": id})
			case parent != nil:
				parent.children = parent.children[:0]
				for _, child := range result.Children {
					if im.nodes[child.ID] == nil {
						node := &driveImportNode{
							id: child.ID, name: child.Name, parent: id, isDir: child.IsDir, depth: parent.depth + 1,
						}
						if child.Size != nil {
							node.size = *child.Size
						}
						im.nodes[child.ID] = node
					}
					parent.children = append(parent.children, child.ID)
				}
				parent.loaded = true
			}
		})
	}()
}

// importVisible lists the nodes in tree order under expanded folders. A mod's own root is left out.
func (im *driveImport) importVisible() []*driveImportNode {
	if im.root == "" {
		return nil
	}
	var stack []string
	if strings.HasPrefix(im.root, driveModRootPrefix) {
		children := im.nodes[im.root].children
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, children[i])
		}
	} else {
		stack = append(stack, im.root)
	}
	var out []*driveImportNode
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := im.nodes[id]
		if node == nil {
			continue
		}
		out = append(out, node)
		if node.isDir && node.loaded && im.expanded[id] {
			for i := len(node.children) - 1; i >= 0; i-- {
				stack = append(stack, node.children[i])
			}
		}
	}
	return out
}

func (im *driveImport) parentOf(id string) string {
	if node := im.nodes[id]; node != nil {
		return node.parent
	}
	return ""
}

// ancestorsOf calls visit for each folder above id, nearest first.
func (im *driveImport) ancestorsOf(id string, visit func(string) bool) {
	seen := map[string]bool{id: true}
	for parent := im.parentOf(id); parent != "" && !seen[parent]; parent = im.parentOf(parent) {
		seen[parent] = true
		if !visit(parent) {
			return
		}
	}
}

func (im *driveImport) hasSelectedAncestor(id string) bool {
	found := false
	im.ancestorsOf(id, func(parent string) bool {
		found = im.selected[parent]
		return !found
	})
	return found
}

// toggleImport selects or deselects a folder with everything under it. A folder under a selected one cannot be
// picked on its own, and picking a folder drops the folders selected under it.
func (im *driveImport) toggleImport(id string) {
	node := im.nodes[id]
	if node == nil || !node.isDir {
		return
	}
	if im.selected[id] {
		delete(im.selected, id)
		return
	}
	if im.hasSelectedAncestor(id) {
		return
	}
	for selected := range im.selected {
		im.ancestorsOf(selected, func(parent string) bool {
			if parent == id {
				delete(im.selected, selected)
				return false
			}
			return true
		})
	}
	im.selected[id] = true
}

// partlySelected reports a folder above a selected one.
func (im *driveImport) partlySelected() map[string]bool {
	out := map[string]bool{}
	for id := range im.selected {
		im.ancestorsOf(id, func(parent string) bool {
			out[parent] = true
			return true
		})
	}
	return out
}

// startDriveImport closes the overlay and copies the selected folders; the copy continues as a transfer, which the
// transfer screen shows and cancels. React closed its overlay here too, so its in-dialog progress and cancel never
// showed.
func (s *Shell) startDriveImport(im *driveImport) {
	if im.source == nil || len(im.selected) == 0 || im.resolving {
		return
	}
	var ids []string
	for id := range im.selected {
		if !im.hasSelectedAncestor(id) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	createFolders := im.createFolders
	params := drive.CopyFromURLParams{
		URL:                     strings.TrimSpace(im.url),
		Password:                im.password,
		DestinationID:           im.destID,
		CreateCollectionFolders: &createFolders,
		SelectedIDs:             ids,
	}
	s.drive.importer = nil
	go func() {
		result, err := s.deps.Drive.CopyFromURL(context.Background(), params)
		fairy.Go(func() {
			if err == nil {
				s.reloadDrives()
				s.toasts.Show(kit.ToastSuccess, T("page.drive.import.success", "count", result.Copied), "")
				return
			}
			switch code := driveErrorCode(err); {
			case code == "DRIVE_LINK_PASSWORD_REQUIRED" || code == "DRIVE_MOD_PASSWORD_REQUIRED":
				s.toasts.Show(kit.ToastWarning, T("page.drive.import.password_required"), "")
			case code == "DRIVE_LINK_INVALID_PASSWORD" || code == "DRIVE_MOD_INVALID_PASSWORD":
				s.toasts.Show(kit.ToastError, T("page.drive.import.invalid_password"), "")
			case isDriveNotFound(err):
				s.toasts.Show(kit.ToastError, T("page.drive.import.expired"), "")
			case code == "DRIVE_COPY_CANCELED" || infra.IsCancellationError(err):
			default:
				s.toasts.Show(kit.ToastError, T("page.drive.import.failed"), err.Error())
			}
		})
	}()
}

func (s *Shell) showDriveImport(cx *fairy.Cx, v *driveView) {
	t := kit.T()
	im := s.drive.importer
	if im.view != v {
		return
	}
	closeOverlay := func() {
		if s.drive.importer == im {
			s.drive.importer = nil
		}
	}
	tree := im.source != nil
	width := float32(384)
	if tree {
		width = 1024
	}
	destName := im.destName
	if destName == "" {
		destName = T("page.drive.import.loading_folder")
	}
	head := fairy.Row(
		fairy.Text(T("page.drive.import.title")).FontSize(15).SemiBold().Grow(1).MinW(0),
		kit.IconButton(lucide.X, closeOverlay, kit.Ghost, kit.Small, kit.Tooltip(T("g.close"))).Label(T("g.close")),
	).PadXY(16, 8).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border)
	var body *fairy.El
	if tree {
		body = s.driveImportTree(im, destName)
	} else {
		body = s.driveImportURL(im, destName)
	}
	viewWidth, viewHeight := cx.Size()
	width = max(min(width, viewWidth-32), 1)
	height := max(min(720, viewHeight-titleBarHeight-32), 1)
	panel := fairy.Col(head, body).W(width).MaxH(height).Radius(t.RadiusXl()).Border(1, t.Border).
		Clip().Shadow(0, 20, 50, 0, shadowColor(t)).Role(fairy.RoleDialog).Label(T("page.drive.import.title")).
		Enter(fairy.SlideY(12)).OnClick(func() {})
	// The React import overlay was bg-background/80 over backdrop-blur-lg.
	glass(panel, t.Background.Alpha(0.8), t.Card, 16)
	cx.Overlay(fairy.Box(panel).Key("drive.import").WFull().HFull().Center().
		PadEdges(16+titleBarHeight, 16, 16, 16).Fill(t.Scrim).Enter(fairy.Fade()), closeOverlay)
}

func (s *Shell) driveImportURL(im *driveImport, destName string) *fairy.El {
	t := kit.T()
	label := func(icon *svg.Vector, text string) *fairy.El {
		row := fairy.Row().Gap(6).Align(fairy.Center).FontSize(13).Medium()
		if icon != nil {
			row.Child(fairy.Icon(icon).Size(12, 12))
		}
		return row.Child(fairy.Text(text))
	}
	hint := func(text string, color fairy.Color) *fairy.El { return fairy.Text(text).FontSize(12).Color(color) }
	urlInput := fairy.Input(im.url, s.setImportURL).Key("drive.import.url").
		Placeholder(T("page.drive.import.url_placeholder")).PlaceholderColor(t.MutedForeground).
		Label(T("page.drive.import.url_label")).AutoFocus().OnSubmit(func(string) {
		if !im.resolving {
			s.resolveImport(im, 0)
		}
	})
	col := fairy.Col(fairy.Col(
		label(lucide.Link, T("page.drive.import.url_label")),
		driveField(urlInput, false).H(36),
		hint(T("page.drive.import.url_hint"), t.MutedForeground),
	).Gap(6)).Pad(16).Gap(16)
	if im.needPassword {
		password := fairy.Input(im.password, func(text string) { im.password, im.badPassword = text, false }).
			Key("drive.import.password").Password().Placeholder(T("page.drive.import.password_placeholder")).
			PlaceholderColor(t.MutedForeground).Label(T("page.drive.import.password_label")).AutoFocus().
			OnSubmit(func(string) {
				if !im.resolving {
					s.resolveImport(im, 0)
				}
			})
		authenticate := T("page.drive.import.authenticate")
		section := fairy.Col(
			label(nil, T("page.drive.import.password_label")),
			fairy.Row(
				driveField(password, im.badPassword).H(36).Grow(1).MinW(0),
				kit.IconButton(lucide.Check, func() { s.resolveImport(im, 0) }, kit.Outline,
					kit.Disabled(strings.TrimSpace(im.password) == "" || im.resolving), kit.Loading(im.resolving),
					kit.Tooltip(authenticate)).Label(authenticate),
			).Gap(6).Align(fairy.Center),
		).Gap(6)
		if im.badPassword {
			section.Child(hint(T("page.drive.import.invalid_password"), t.Destructive))
		}
		col.Child(section.Child(hint(T("page.drive.import.password_hint"), t.MutedForeground)))
	}
	col.Child(fairy.Col(
		label(nil, T("page.drive.import.destination_label")),
		fairy.Row(
			fairy.Icon(lucide.Folder).Size(16, 16).Color(driveFolderColor).Shrink(0),
			fairy.Text(destName).Ellipsis().MinW(0).Grow(1),
		).H(36).PadXY(10, 0).Gap(8).Align(fairy.Center).Radius(t.RadiusMd()).Border(1, t.Border).Fill(t.Muted),
	).Gap(6))
	if im.resolving {
		col.Child(fairy.Row(fairy.Spinner().Size(14, 14), fairy.Text(T("page.drive.import.resolving"))).Gap(8).
			Align(fairy.Center).FontSize(13).Color(t.MutedForeground))
	}
	return col
}

func (s *Shell) driveImportTree(im *driveImport, destName string) *fairy.El {
	t := kit.T()
	small := func(text string) *fairy.El { return fairy.Text(text).FontSize(12).Color(t.MutedForeground) }
	count := T("page.drive.import.selected_count", "count", len(im.selected))
	body := fairy.Col(
		fairy.Col(small(T("page.drive.import.url_label")),
			fairy.Text(im.url).FontSize(12).Ellipsis().MinW(0).TooltipWhenTruncated()).Gap(4),
		fairy.Col(small(T("page.drive.import.destination_label")), fairy.Row(
			fairy.Icon(lucide.Folder).Size(16, 16).Color(driveFolderColor).Shrink(0),
			fairy.Text(destName).FontSize(13).Ellipsis().MinW(0),
		).Gap(8).Align(fairy.Center)).Gap(4),
	).Grow(1).Pad(16).Gap(16)
	if !im.source.link {
		body.Child(kit.Checkbox(im.createFolders, func(on bool) { im.createFolders = on },
			kit.Label(T("page.drive.import.create_collection_folder"))))
	}
	body.Child(fairy.Row(small(T("page.drive.import.selected_label")), fairy.Spacer(),
		fairy.Text(count).FontSize(12).Medium()).Align(fairy.Center))
	settings := fairy.Col(
		fairy.Col(fairy.Text(T("page.drive.import.settings_title")).FontSize(14).Medium(),
			small(T("page.drive.import.settings_description"))).Gap(2).PadXY(16, 12).
			BorderEdges(0, 0, 1, 0, t.Border),
		body,
		fairy.Row(
			kit.Button(T("g.previous"), im.reset, kit.Outline, kit.Icon(lucide.ArrowLeft)).Grow(1),
			kit.Button(T("page.drive.import.action"), func() { s.startDriveImport(im) }, kit.Icon(lucide.Download),
				kit.Disabled(len(im.selected) == 0 || im.resolving)).Grow(1),
		).Gap(8).Pad(12).BorderEdges(1, 0, 0, 0, t.Border),
	).W(320).Shrink(0).BorderEdges(0, 1, 0, 0, t.Border)

	var list *fairy.El
	nodes := im.importVisible()
	switch {
	case im.resolving:
		list = fairy.Row(fairy.Spinner().Size(14, 14), small(T("page.drive.import.tree_loading"))).Gap(8).
			Grow(1).WFull().Center()
	case len(nodes) == 0:
		list = fairy.Col(small(T("page.drive.import.tree_empty"))).Grow(1).WFull().Center()
	default:
		partly := im.partlySelected()
		list = fairy.VirtualList(len(nodes), driveImportRow, func(i int) *fairy.El {
			return s.driveImportRow(im, nodes[i], partly[nodes[i].id])
		}).Key("drive.import.tree").Grow(1).MinH(0).WFull().Pad(8)
	}
	return fairy.Row(settings, fairy.Col(
		fairy.Row(fairy.Text(T("page.drive.import.tree_title")).FontSize(14).Medium(), fairy.Spacer(), small(count)).
			PadXY(16, 10).Align(fairy.Center).BorderEdges(0, 0, 1, 0, t.Border),
		list,
	).Grow(1).MinW(0)).WFull().H(560).MinH(0)
}

func (s *Shell) driveImportRow(im *driveImport, node *driveImportNode, partly bool) *fairy.El {
	t := kit.T()
	row := fairy.Row().Key("drive.import.node:"+node.id).H(driveImportRow).WFull().Gap(8).Align(fairy.Center).
		PadEdges(0, 8, 0, float32(node.depth*driveImportIndent+8)).HoverFill(hoverFill(t))
	expander := fairy.Box().Size(20, 20).Shrink(0).Center().Radius(4)
	switch {
	case im.loadingIDs[node.id]:
		expander.Child(fairy.Spinner().Size(12, 12))
	case node.isDir:
		icon := lucide.ChevronRight
		if im.expanded[node.id] {
			icon = lucide.ChevronDown
		}
		id := node.id
		expander.Child(fairy.Icon(icon).Size(12, 12)).HoverFill(hoverFill(t)).Cursor(fairy.CursorPointer).
			Label(node.name).Expanded(im.expanded[id]).OnClick(func() {
			if im.expanded[id] {
				delete(im.expanded, id)
				return
			}
			im.expanded[id] = true
			if n := im.nodes[id]; n != nil && !n.loaded {
				s.loadImportChildren(im, id)
			}
		})
	}
	row.Child(expander)
	checked := node.isDir && im.selected[node.id]
	switch {
	case !node.isDir:
		row.Child(kit.Checkbox(false, func(bool) {}, kit.Disabled(true)))
	case partly && !checked:
		row.Child(
			fairy.Box(fairy.Icon(lucide.Minus).Size(12, 12).Color(t.AccentForeground)).Size(16, 16).Radius(4).Center().
				Fill(t.Accent).
				MarginEdges(2, 0, 0, 0).Cursor(fairy.CursorPointer).Role(fairy.RoleCheckBox).Label(node.name).
				OnClick(func() { im.toggleImport(node.id) }),
		)
	default:
		row.Child(kit.Checkbox(checked, func(bool) { im.toggleImport(node.id) }).Label(node.name))
	}
	if checked {
		row.Fill(accentSoft(t))
	}
	icon, color := lucide.File, t.MutedForeground
	if node.isDir {
		icon, color = lucide.Folder, driveFolderColor
	}
	name := fairy.Text(node.name).FontSize(13).Ellipsis().MinW(0)
	if !node.isDir {
		name.Color(t.MutedForeground)
	}
	row.Child(fairy.Row(fairy.Icon(icon).Size(16, 16).Color(color).Shrink(0), name).Grow(1).MinW(0).Gap(8).
		Align(fairy.Center))
	if !node.isDir {
		row.Child(fairy.Text(formatSize(node.size)).FontSize(12).Color(t.MutedForeground).NoWrap().Shrink(0))
	}
	return row
}
