package ui

import (
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/motion"
	"github.com/myparsleycat/fairy/svg"
	"golang.org/x/net/html"
)

const (
	gbGalleryRow       float32 = 110
	gbGalleryGap       float32 = 8
	gbMarkdownImageMax float32 = 1024
)

type gbPreview struct {
	url string
	alt string
}

// gbResolvePreview picks a screenshot URL, preferring the sizes React asked for: the 530px file for cards,
// the original for the gallery and the lightbox.
func gbResolvePreview(p gbPreviewImage, small bool) string {
	candidates := []string{p.File, p.File530, p.File220, p.URL}
	if small {
		candidates = []string{p.File530, p.File220, p.File100, p.File, p.URL}
	}
	for _, c := range candidates {
		if gbAbsoluteURL(c) {
			return c
		}
	}
	for _, c := range candidates {
		if c == "" || p.BaseURL == "" {
			continue
		}
		base, err := url.Parse(strings.TrimSuffix(p.BaseURL, "/") + "/")
		if err != nil {
			return ""
		}
		ref, err := url.Parse(c)
		if err != nil {
			return ""
		}
		return base.ResolveReference(ref).String()
	}
	if gbAbsoluteURL(p.BaseURL) {
		return p.BaseURL
	}
	return ""
}

func gbAbsoluteURL(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func gbSubmissionPreviewURL(media *gbPreviewMedia) string {
	if media == nil || len(media.Screenshots) == 0 {
		return ""
	}
	return gbResolvePreview(media.Screenshots[0], true)
}

func gbPreviewImages(media *gbPreviewMedia) []gbPreview {
	if media == nil {
		return nil
	}
	var out []gbPreview
	for i, shot := range media.Screenshots {
		link := gbResolvePreview(shot, false)
		if link == "" {
			continue
		}
		alt := strings.TrimSpace(shot.Caption)
		if alt == "" {
			alt = "Preview image " + strconv.Itoa(i+1)
		}
		out = append(out, gbPreview{url: link, alt: alt})
	}
	return out
}

func (s *Shell) gbModPanel(mainWidth float32) *fairy.El {
	g := &s.gameBanana
	q := &g.modView
	content := fairy.Col().WFull().Pad(gbContentPad).Gap(32)
	switch {
	case q.loading:
		content.Child(
			fairy.Col(
				fairy.Row(kit.Skeleton().W(80).H(24).Radius(12), kit.Skeleton().W(96).H(24).Radius(12)).Gap(8),
				kit.Skeleton().WPct(2.0/3).H(36),
			).Gap(12),
			gbGalleryGrid(mainWidth, 5, func(_ int, width, height float32) *fairy.El {
				return kit.Skeleton().Size(width, height).Radius(16)
			}),
			fairy.Col(
				kit.Skeleton().WPct(2.0/5).H(20),
				kit.Skeleton().WFull().H(20),
				kit.Skeleton().WPct(11.0/12).H(20),
				kit.Skeleton().WPct(4.0/5).H(20),
				kit.Skeleton().WFull().H(96).Radius(12),
			).Gap(12),
		)
	case q.err != nil:
		content.Child(gbErrorState(q.err))
	case q.data != nil:
		content.Child(s.gbModHeader(q.data), s.gbGallery(q.data, mainWidth), s.gbDescription(q.data))
	}
	if g.mod != nil {
		content.Child(s.gbComments())
	}
	return fairy.Col(content).Key("gamebanana.mod." + g.mod.key()).WFull().HFull().ScrollY()
}

func (s *Shell) gbModHeader(data *gbModOverview) *fairy.El {
	g := &s.gameBanana
	profile := data.Profile
	sel := *g.mod
	key := sel.key()
	access := data.Config.Access
	liked := access["Like_Trash"]
	if profile.AccessorHasLiked != nil {
		liked = *profile.AccessorHasLiked
	}
	if g.likeOverride != nil && g.likeOverride.key == key {
		liked = g.likeOverride.liked
	}
	canToggle := access["Like_Add"] || access["Like_Trash"]
	signedIn := g.session.data != nil && g.session.data.Authenticated
	pending := g.likePending == key

	tooltip := T("page.gamebanana.like_sign_in_required")
	switch {
	case canToggle && liked:
		tooltip = T("page.gamebanana.unlike")
	case canToggle:
		tooltip = T("page.gamebanana.like")
	case signedIn:
		tooltip = T("page.gamebanana.like_unavailable")
	}
	variant := kit.Outline
	if liked {
		variant = kit.Default
	}
	label := T("page.gamebanana.like")
	if liked {
		label = T("page.gamebanana.liked")
	}
	like := kit.Button(gbFormatNumber(profile.LikeCount), func() { s.gbToggleLike(sel, canToggle, signedIn) },
		variant, kit.Small, kit.Icon(lucide.Heart), kit.Loading(pending),
		kit.Disabled((!canToggle && signedIn) || pending), kit.Tooltip(tooltip)).
		Label(label).Checked(liked).Key("gamebanana.like")

	badges := fairy.Row().Gap(8).Wrap()
	for _, name := range []string{profile.Category.Name, profile.Submitter.Name, profile.Game.Name} {
		if name != "" {
			badges.Child(kit.Badge(name, kit.Outline))
		}
	}
	return fairy.Col(
		badges,
		fairy.Row(fairy.Text(profile.Name).FontSize(24).LineHeight(32).SemiBold().Grow(1).MinW(0), like).
			Gap(12).
			Align(fairy.Center).
			Wrap(),
	).Gap(8)
}

func (s *Shell) gbGallery(data *gbModOverview, mainWidth float32) *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	previews := gbPreviewImages(data.Profile.Preview)
	if len(previews) == 0 {
		return fairy.Col(fairy.Icon(lucide.Image).Size(32, 32), fairy.Text(T("page.gamebanana.no_preview_image")).
			FontSize(14).LineHeight(20)).WFull().H((mainWidth - 2*gbContentPad) * 10 / 16).Center().Gap(8).Radius(16).
			Fill(t.Muted.Alpha(0.5)).Color(t.MutedForeground)
	}
	key := g.mod.key()
	return gbGalleryGrid(mainWidth, min(len(previews), gbMaxVisiblePreviews), func(index int, w, h float32) *fairy.El {
		hover := fairy.Local[bool]("gamebanana.gallery.hover:" + strconv.Itoa(index))
		box := fairy.Box().W(w).H(h).Radius(16).Clip().Fill(t.Muted.Alpha(0.3)).HoverFill(t.Muted.Alpha(0.4)).
			Animate(motion.Fast).Cursor(fairy.CursorPointer).Label(previews[index].alt).
			Role(fairy.RoleButton).Focusable().FocusRing(2, t.Ring).
			OnHover(func(in bool) { *hover = in }).OnClick(func() { g.lightbox, g.lightboxKey = index, key })
		if img := s.gbImage(previews[index].url, w, h, fairy.FitCover); img != nil {
			box.Child(fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitCover))
		}
		if index == gbMaxVisiblePreviews-1 && len(previews) > gbMaxVisiblePreviews {
			more := len(previews) - index
			box.Child(
				fairy.Text("+" + strconv.Itoa(more)).FontSize(14).LineHeight(20).SemiBold().Color(fairy.Hex(0xffffff)).
					Absolute().Inset(0).Center().TextAlign(fairy.TextCenter).Fill(fairy.RGBA(0, 0, 0, 0.6)),
			)
		} else {
			shade := fairy.Box().Absolute().Left(0).Right(0).Bottom(0).H(h/2).
				Gradient(0, fairy.RGBA(0, 0, 0, 0.2), fairy.RGBA(0, 0, 0, 0)).Opacity(0).Animate(motion.Fast).
				Role(fairy.RoleHidden)
			if *hover {
				shade.Opacity(1)
			}
			box.Child(shade)
		}
		return box
	}).Key("gamebanana.gallery")
}

func gbGalleryGrid(mainWidth float32, count int, tile func(int, float32, float32) *fairy.El) *fairy.El {
	// The React grid: five columns, the first picture three columns by four rows, the next two two by two
	// on its right, then a row of single cells whose last one counts the pictures left over.
	unit := max((mainWidth-2*gbContentPad-4*gbGalleryGap)/5, 0)
	bigW := 3*unit + 2*gbGalleryGap
	sideW := 2*unit + gbGalleryGap
	bigH := 4*gbGalleryRow + 3*gbGalleryGap
	sideH := 2*gbGalleryRow + gbGalleryGap
	top := fairy.Row(tile(0, bigW, bigH)).Gap(gbGalleryGap)
	if count > 1 {
		side := fairy.Col(tile(1, sideW, sideH)).Gap(gbGalleryGap)
		if count > 2 {
			side.Child(tile(2, sideW, sideH))
		}
		top.Child(side)
	}
	gallery := fairy.Col(top).Gap(gbGalleryGap)
	if count > 3 {
		bottom := fairy.Row().Gap(gbGalleryGap)
		for i := 3; i < count; i++ {
			bottom.Child(tile(i, unit, gbGalleryRow))
		}
		gallery.Child(bottom)
	}
	return gallery
}

func (s *Shell) gbDescription(data *gbModOverview) *fairy.El {
	source := data.markdown
	if strings.TrimSpace(source) == "" {
		return gbEmptyState(T("page.gamebanana.no_description"))
	}
	return kit.Markdown(source, kit.MarkdownOptions{
		OnLink: s.gbOpenExternal,
		Images: func(src string) *fairy.Image {
			return s.gbImage(src, gbMarkdownImageMax, gbMarkdownImageMax, fairy.FitContain)
		},
		Selectable: true,
	}).WFull()
}

func (s *Shell) gbLightbox(cx *fairy.Cx) {
	g := &s.gameBanana
	if g.lightbox < 0 || g.mod == nil || g.lightboxKey != g.mod.key() || g.modView.data == nil {
		return
	}
	previews := gbPreviewImages(g.modView.data.Profile.Preview)
	if g.lightbox >= len(previews) {
		return
	}
	index := g.lightbox
	windowW, windowH := cx.Size()
	var content *fairy.El
	if img := s.gbImage(previews[index].url, windowW*0.8, windowH*0.8, fairy.FitContain); img != nil {
		content = fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitContain).Radius(6).Label(previews[index].alt)
	} else {
		content = kit.Spinner(32).Color(fairy.Hex(0xffffff))
	}
	lightbox{
		key: "gamebanana.lightbox", label: previews[index].alt, content: content, index: index, count: len(previews),
		step:  func(delta int) { g.lightbox = (index + delta + len(previews)) % len(previews) },
		close: func() { g.lightbox = -1 },
	}.show(cx)
}

func (s *Shell) gbComments() *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	seen := map[gbNumber]bool{}
	var comments []gbPost
	for _, page := range g.posts {
		for _, post := range page.Records {
			if post.Status == "3" || seen[post.ID] {
				continue
			}
			seen[post.ID] = true
			comments = append(comments, post)
		}
	}
	total := gbNumber(len(comments))
	if len(g.posts) > 0 {
		total = g.posts[0].Metadata.RecordCount
	}
	sorts := []kit.Item{
		{Value: "popular", Label: T("page.gamebanana.comment_sort.popular")},
		{Value: "newest", Label: T("page.gamebanana.comment_sort.newest")},
	}
	header := fairy.Row(
		fairy.Text(T("page.gamebanana.comments")).FontSize(16).SemiBold(),
		kit.Badge(gbFormatNumber(total), kit.Outline),
		fairy.Spacer(),
		kit.Select("gamebanana.comment-sort", g.commentSort, sorts, func(v string) { g.commentSort = v },
			kit.Small).W(128),
	).Gap(8).Align(fairy.Center)
	section := fairy.Col(header).Gap(16)
	initial := len(g.posts) == 0 && g.postsLoading
	switch {
	case initial:
		section.Child(fairy.Col(kit.Skeleton().WFull().H(96).Radius(12), kit.Skeleton().WFull().H(96).Radius(12),
			kit.Skeleton().WFull().H(96).Radius(12)).Gap(12))
	case g.postsErr != nil:
		section.Child(gbErrorState(g.postsErr))
	case len(comments) == 0 && !g.postsLoading:
		section.Child(gbEmptyState(T("page.gamebanana.no_comments")))
	}
	if g.postsErr == nil {
		for _, post := range comments {
			section.Child(s.gbComment(post))
		}
		if last := len(g.posts) - 1; last >= 0 && !g.posts[last].Metadata.IsComplete {
			section.Child(fairy.Row(kit.Button(T("page.gamebanana.load_more_comments"), s.gbLoadMorePosts,
				kit.Outline, kit.Loading(g.postsLoading), kit.Disabled(g.postsLoading))).Justify(fairy.JustifyCenter))
		}
	}
	return section.Color(t.Foreground)
}

func (s *Shell) gbComment(post gbPost) *fairy.El {
	t := kit.T()
	name := T("g.unknown")
	avatarURL := ""
	if post.Poster != nil {
		name, avatarURL = post.Poster.Name, post.Poster.AvatarURL
	}
	avatar := fairy.Box().Size(24, 24).Radius(12).Clip().Fill(t.Muted).Shrink(0).MarginEdges(2, 0, 0, 0)
	if img := s.gbImage(avatarURL, 24, 24, fairy.FitCover); img != nil {
		avatar.Child(fairy.ImageEl(img).WFull().HFull().Fit(fairy.FitCover))
	} else {
		avatar.Child(fairy.Text(gbInitials(name)).FontSize(12).LineHeight(16).Medium().Color(t.MutedForeground)).
			Center()
	}
	head := fairy.Row(fairy.Text(name).Medium()).Gap(8).Align(fairy.Center).Wrap()
	if post.DateAdded != nil {
		head.Child(
			fairy.Text(gbFormatDate(int64(*post.DateAdded))).FontSize(12).LineHeight(16).Color(t.MutedForeground),
		)
	}
	replies := gbNumber(0)
	if post.ReplyCount != nil {
		replies = *post.ReplyCount
	}
	stamps := gbNumber(0)
	for _, stamp := range post.Stamps {
		if stamp.Count != nil {
			stamps += *stamp.Count
		}
	}
	return fairy.Row(avatar, fairy.Col(
		head,
		fairy.Row(
			fairy.Row(
				fairy.Icon(lucide.MessageSquare).Size(14, 14),
				fairy.Text(
					T("page.gamebanana.comment_replies", "count", gbFormatNumber(replies)),
				).FontSize(12).LineHeight(16),
			).Gap(4).Align(fairy.Center),
			fairy.Text(T("page.gamebanana.comment_stamps", "count", gbFormatNumber(stamps))).
				FontSize(12).
				LineHeight(16),
		).Gap(8).Wrap().FontSize(12).Color(t.MutedForeground),
		fairy.Text(post.body).FontSize(14).LineHeight(24).MarginEdges(12, 0, 0, 0),
	).Gap(4).Grow(1).MinW(0)).Key("gamebanana.comment." + strconv.Itoa(int(post.ID))).Gap(12).Align(fairy.Start)
}

func gbInitials(name string) string {
	var out strings.Builder
	for i, part := range strings.Fields(name) {
		if i == 2 {
			break
		}
		for _, r := range part {
			out.WriteString(strings.ToUpper(string(r)))
			break
		}
	}
	if out.Len() == 0 {
		return "?"
	}
	return out.String()
}

func (s *Shell) gbFilesSidebar() *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	q := &g.modView
	stats := fairy.Col().WFull().Pad(12).BorderEdges(0, 0, 1, 0, t.Border).Shrink(0)
	switch {
	case q.loading:
		stats.Child(fairy.Col(
			fairy.Row(kit.Skeleton().Grow(1).H(80), kit.Skeleton().Grow(1).H(80)).Gap(12),
			fairy.Row(kit.Skeleton().Grow(1).H(80), kit.Skeleton().Grow(1).H(80)).Gap(12),
		).Gap(12))
	case q.err != nil:
		stats.Child(gbErrorState(q.err))
	default:
		var likes, posts, views, downloads gbNumber
		if q.data != nil {
			p := q.data.Profile
			likes, posts, views, downloads = p.LikeCount, p.PostCount, p.ViewCount, p.DownloadCount
		}
		stat := func(icon *svg.Vector, label string, value gbNumber) *fairy.El {
			return fairy.Col(
				fairy.Row(fairy.Icon(icon).Size(16, 16), fairy.Text(label).FontSize(12).LineHeight(16)).
					Gap(8).
					Align(fairy.Center).
					Color(t.MutedForeground),
				fairy.Text(gbFormatNumber(value)).FontSize(18).LineHeight(28).SemiBold(),
			).Grow(1).Basis(0).Pad(12).Gap(8).Radius(6).Fill(t.Muted.Alpha(0.5))
		}
		stats.Child(fairy.Col(
			fairy.Row(stat(lucide.Heart, T("page.gamebanana.stats.likes"), likes),
				stat(lucide.MessageSquare, T("page.gamebanana.stats.posts"), posts)).Gap(12),
			fairy.Row(stat(lucide.Users, T("page.gamebanana.stats.views"), views),
				stat(lucide.FileArchive, T("page.gamebanana.stats.downloads"), downloads)).Gap(12),
		).Gap(12))
	}

	var files []gbFile
	if q.data != nil {
		files = q.data.Profile.Files
	}
	head := fairy.Row(
		fairy.Text(T("page.gamebanana.files")).FontSize(14).LineHeight(20).Medium().Color(t.MutedForeground),
		fairy.Spacer(),
		kit.Badge(gbFormatNumber(gbNumber(len(files))), kit.Outline),
	).PadEdges(12, 16, 8, 16).Align(fairy.Center).Shrink(0)

	list := fairy.Col().PadEdges(0, 12, 12, 12).Gap(2)
	switch {
	case q.loading:
		for range 3 {
			list.Child(kit.Skeleton().WFull().H(64))
		}
	case q.err != nil:
		list.Child(gbErrorState(q.err))
	case len(files) == 0:
		list.Child(gbEmptyState(T("page.gamebanana.no_files")))
	default:
		itemID := int(q.data.Profile.ID)
		for _, file := range files {
			list.Child(s.gbFileRow(itemID, file))
		}
	}
	return fairy.Col(stats, head, fairy.Col(list).Key("gamebanana.files").Grow(1).MinH(0).WFull().ScrollY())
}

func (s *Shell) gbFileRow(itemID int, file gbFile) *fairy.El {
	t := kit.T()
	g := &s.gameBanana
	fileID := int(file.ID)
	pending := g.pendingFile == fileID
	busy := g.pendingFile != 0
	meta := fairy.Row(
		fairy.Text(T("page.gamebanana.file_downloads", "count", gbFormatNumber(file.DownloadCount))).
			FontSize(12).
			LineHeight(16),
		fairy.Text(gbFormatDate(int64(file.DateAdded))).FontSize(12).LineHeight(16),
	).Gap(12).Wrap().FontSize(12).Color(t.MutedForeground).MarginEdges(8, 0, 0, 0)
	row := fairy.Row().Key("gamebanana.file."+strconv.Itoa(fileID)).WFull().PadXY(12, 10).Gap(8).Align(fairy.Start).
		Radius(6).Animate(motion.Fast).Label(file.File).Disabled(busy)
	if pending {
		row.Child(kit.Spinner(16).Color(t.MutedForeground).Shrink(0))
	}
	row.Child(
		fairy.Col(fairy.Text(file.File).FontSize(14).LineHeight(20).Medium().MaxH(40).Clip(), meta).Grow(1).MinW(0),
	)
	if busy {
		return row.Opacity(0.7).Cursor(fairy.CursorWait)
	}
	return row.HoverFill(t.Muted.Alpha(0.5)).Cursor(fairy.CursorPointer).
		OnClick(func() { s.gbDownload(itemID, fileID) })
}

var (
	gbBlankLines = regexp.MustCompile(`\n{3,}`)
	gbSpaces     = regexp.MustCompile(`[ \t]{2,}`)
)

// gbHTMLToText keeps the text of a comment with its line and paragraph breaks.
func gbHTMLToText(source string) string {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			out.WriteString(n.Data)
		case n.Type == html.ElementNode && n.Data == "br":
			out.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && (n.Data == "p" || n.Data == "div" || n.Data == "li") {
			out.WriteString("\n\n")
		}
	}
	walk(root)
	text := gbBlankLines.ReplaceAllString(out.String(), "\n\n")
	return strings.TrimSpace(gbSpaces.ReplaceAllString(text, " "))
}

// gbHTMLToMarkdown turns a mod description into the Markdown the kit renders: headings, paragraphs, line
// breaks, emphasis, lists, links and images. Only http, https and mailto links survive, as the React
// sanitizer allowed; scripts, styles and embedded frames are dropped.
func gbHTMLToMarkdown(source string) string {
	root, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(gbBlankLines.ReplaceAllString(gbMarkdownOf(root, 0), "\n\n"))
}

// gbMarkdownOf writes the Markdown of the children of n, which sit in listDepth lists. It keeps what the
// React panel rendered from the sanitized HTML: headings, numbered and nested lists, quotes, rules, code,
// strikethrough, tables, links and images.
func gbMarkdownOf(n *html.Node, listDepth int) string {
	m := &gbMarkdown{}
	m.children(n, listDepth)
	return m.out.String()
}

type gbMarkdown struct {
	out strings.Builder
}

func (m *gbMarkdown) children(n *html.Node, listDepth int) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		m.walk(c, listDepth)
	}
}

// block ends the current paragraph with a blank line.
func (m *gbMarkdown) block() {
	text := m.out.String()
	if text != "" && !strings.HasSuffix(text, "\n\n") {
		if strings.HasSuffix(text, "\n") {
			m.out.WriteString("\n")
		} else {
			m.out.WriteString("\n\n")
		}
	}
}

func (m *gbMarkdown) walk(n *html.Node, listDepth int) {
	if n.Type == html.TextNode {
		m.out.WriteString(gbEscapeMarkdown(strings.Join(strings.Fields(n.Data), " "), n.Data))
		return
	}
	if n.Type != html.ElementNode && n.Type != html.DocumentNode {
		return
	}
	switch n.Data {
	case "script", "style", "iframe", "object", "embed", "head":
	case "br":
		m.out.WriteString("  \n")
	case "h1", "h2", "h3", "h4", "h5", "h6":
		m.block()
		m.out.WriteString(strings.Repeat("#", int(n.Data[1]-'0')) + " ")
		m.children(n, listDepth)
		m.block()
	case "p", "div", "center", "section", "article":
		m.block()
		m.children(n, listDepth)
		m.block()
	case "blockquote":
		m.block()
		for line := range strings.SplitSeq(strings.TrimSpace(gbMarkdownOf(n, 0)), "\n") {
			m.out.WriteString(strings.TrimRight("> "+line, " ") + "\n")
		}
		m.block()
	case "hr":
		m.block()
		m.out.WriteString("---")
		m.block()
	case "pre":
		text := strings.TrimRight(gbTextContent(n), "\n")
		fence := "```"
		for strings.Contains(text, fence) {
			fence += "`"
		}
		m.block()
		m.out.WriteString(fence + "\n" + text + "\n" + fence)
		m.block()
	case "code", "kbd", "samp":
		text := strings.Join(strings.Fields(gbTextContent(n)), " ")
		if text == "" {
			return
		}
		mark := "`"
		if strings.Contains(text, "`") {
			mark = "``"
		}
		m.out.WriteString(mark + text + mark)
	case "ul", "ol":
		m.list(n, listDepth+1)
	case "li":
		m.item(n, listDepth, "- ")
	case "table":
		m.table(n)
	case "strong", "b":
		gbWrapInline(&m.out, "**", func() { m.children(n, listDepth) })
	case "em", "i":
		gbWrapInline(&m.out, "*", func() { m.children(n, listDepth) })
	case "s", "strike", "del":
		gbWrapInline(&m.out, "~~", func() { m.children(n, listDepth) })
	case "a":
		href := gbAttr(n, "href")
		if !gbSafeLink(href) {
			m.children(n, listDepth)
			return
		}
		m.out.WriteString("[")
		m.children(n, listDepth)
		m.out.WriteString("](" + href + ")")
	case "img":
		if src := gbAttr(n, "src"); gbAbsoluteURL(src) {
			m.block()
			m.out.WriteString("![" + gbAttr(n, "alt") + "](" + src + ")")
			m.block()
		}
	default:
		m.children(n, listDepth)
	}
}

// list writes the items of a ul or ol, numbering an ol from its start attribute.
func (m *gbMarkdown) list(n *html.Node, listDepth int) {
	m.block()
	number := 1
	if start, err := strconv.Atoi(gbAttr(n, "start")); err == nil {
		number = start
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.Data != "li" {
			m.walk(c, listDepth)
			continue
		}
		marker := "- "
		if n.Data == "ol" {
			marker = strconv.Itoa(number) + ". "
			number++
		}
		m.item(c, listDepth, marker)
	}
	m.block()
}

func (m *gbMarkdown) item(n *html.Node, listDepth int, marker string) {
	if text := m.out.String(); text != "" && !strings.HasSuffix(text, "\n") {
		m.out.WriteString("\n")
	}
	m.out.WriteString(strings.Repeat("   ", max(listDepth-1, 0)) + marker)
	m.children(n, listDepth)
	m.out.WriteString("\n")
}

// table writes a GFM table; its first row is the header, as Markdown tables need one.
func (m *gbMarkdown) table(n *html.Node) {
	var rows [][]string
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data != "tr" {
				collect(c)
				continue
			}
			var row []string
			for cell := c.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.Type == html.ElementNode && (cell.Data == "td" || cell.Data == "th") {
					// Text escapes its pipes already, so they do not split the cell.
					row = append(row, strings.Join(strings.Fields(gbMarkdownOf(cell, 0)), " "))
				}
			}
			rows = append(rows, row)
		}
	}
	collect(n)
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	if columns == 0 {
		return
	}
	m.block()
	writeRow := func(cells []string) {
		m.out.WriteString("|")
		for i := range columns {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			m.out.WriteString(" " + cell + " |")
		}
		m.out.WriteString("\n")
	}
	writeRow(rows[0])
	writeRow(slices.Repeat([]string{"---"}, columns))
	for _, row := range rows[1:] {
		writeRow(row)
	}
	m.block()
}

func gbTextContent(n *html.Node) string {
	var out strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			out.WriteString(n.Data)
		case n.Type == html.ElementNode && n.Data == "br":
			out.WriteString("\n")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out.String()
}

func gbWrapInline(out *strings.Builder, mark string, inner func()) {
	start := out.Len()
	out.WriteString(mark)
	inner()
	if out.Len() == start+len(mark) {
		text := out.String()[:start]
		out.Reset()
		out.WriteString(text)
		return
	}
	out.WriteString(mark)
}

func gbAttr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func gbSafeLink(href string) bool {
	u, err := url.Parse(href)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto") &&
		!strings.ContainsAny(href, " ()")
}

// gbEscapeMarkdown keeps text from being read as Markdown syntax. Collapsing whitespace keeps the
// separating space the original text had at either end.
func gbEscapeMarkdown(collapsed, original string) string {
	if collapsed == "" {
		if original != "" && strings.TrimSpace(original) == "" {
			return " "
		}
		return ""
	}
	var out strings.Builder
	if strings.TrimLeft(original, " \t\r\n") != original {
		out.WriteString(" ")
	}
	for _, r := range collapsed {
		if strings.ContainsRune("\\`*_[]#<>|", r) {
			out.WriteRune('\\')
		}
		out.WriteRune(r)
	}
	if strings.TrimRight(original, " \t\r\n") != original {
		out.WriteString(" ")
	}
	return out.String()
}
