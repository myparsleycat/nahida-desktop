package ui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"
	"github.com/myparsleycat/fairy/svg"

	"nahida.live/desktop/internal/tools/menumaker"
)

// icons.txt lists every lucide-react 1.64.0 icon as "name<TAB>svg body", or "alias<TAB>@name" for a renamed
// icon, so that names stored by the React frontend keep resolving.
//
//go:embed assets/lucide/icons.txt
var lucideCatalogText string

const (
	lucideHead = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" ` +
		`stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">`
	menuMakerDefaultColor = "#ff4fb3"
	lucideSearchLimit     = 80
	iconifyCachedLimit    = 128
	maxMenuMakerVectors   = 4096
)

type lucideCatalog struct {
	names  []string
	bodies map[string]string
}

var lucideIcons = sync.OnceValue(func() lucideCatalog {
	catalog := lucideCatalog{bodies: map[string]string{}}
	aliases := map[string]string{}
	for line := range strings.Lines(lucideCatalogText) {
		name, body, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "\t")
		if !ok {
			continue
		}
		catalog.names = append(catalog.names, name)
		if target, alias := strings.CutPrefix(body, "@"); alias {
			aliases[name] = target
		} else {
			catalog.bodies[name] = body
		}
	}
	for alias, target := range aliases {
		catalog.bodies[alias] = catalog.bodies[target]
	}
	return catalog
})

var menuMakerVectors = struct {
	sync.Mutex
	byKey map[string]*svg.Vector
}{byKey: map[string]*svg.Vector{}}

// menuMakerIconVector returns the drawing of a Lucide or Iconify icon, or nil for an upload or an unknown name.
// Asset rendering calls it off the UI thread.
func menuMakerIconVector(icon menuMakerIcon) *svg.Vector {
	var key, src string
	switch icon.Kind {
	case "lucide":
		body, ok := lucideIcons().bodies[icon.Name]
		if !ok {
			return nil
		}
		key, src = "lucide:"+icon.Name, lucideHead+body+"</svg>"
	case "iconify":
		if icon.SVG == "" {
			return nil
		}
		key, src = "iconify:"+icon.SVG, icon.SVG
	default:
		return nil
	}

	menuMakerVectors.Lock()
	defer menuMakerVectors.Unlock()
	if v, ok := menuMakerVectors.byKey[key]; ok {
		return v
	}
	if len(menuMakerVectors.byKey) >= maxMenuMakerVectors {
		clear(menuMakerVectors.byKey)
	}
	v := svg.Lazy(src)
	menuMakerVectors.byKey[key] = v
	return v
}

var menuMakerIconRules = []struct {
	pattern *regexp.Regexp
	name    string
	color   string
}{
	{regexp.MustCompile(`(?i)hat|cap|crown`), "crown", "#f6be4a"},
	{regexp.MustCompile(`(?i)hair|bang|wig`), "scissors", "#c98c4a"},
	{regexp.MustCompile(`(?i)face|mask|head`), "smile", "#ff8fb3"},
	{regexp.MustCompile(`(?i)eye|iris|pupil`), "eye", "#74d6ff"},
	{regexp.MustCompile(`(?i)glass|spect`), "glasses", "#74d6ff"},
	{regexp.MustCompile(`(?i)dress|skirt|cloth|outfit|costume|body|shirt`), "shirt", "#ff8fb3"},
	{regexp.MustCompile(`(?i)weapon|sword|blade|gun`), "sword", "#dddddd"},
	{regexp.MustCompile(`(?i)color|skin|tone|tint`), "palette", "#ff4fb3"},
	{regexp.MustCompile(`(?i)animation|anim|fx`), "sparkles", "#f6be4a"},
}

// suggestMenuMakerIcon picks an icon from the words in the slot's sections, variables and command lists.
func suggestMenuMakerIcon(slot menumaker.MenuMakerSlot) menuMakerIcon {
	var words []string
	for _, handler := range slot.Handlers {
		words = append(words, handler.Section)
		words = append(words, menumaker.AssignmentVariables(handler)...)
		words = append(words, handler.CommandLists...)
	}
	searchable := strings.Join(words, " ")
	for _, rule := range menuMakerIconRules {
		if rule.pattern.MatchString(searchable) {
			return menuMakerIcon{Kind: "lucide", Name: rule.name, Color: rule.color}
		}
	}
	return menuMakerIcon{Kind: "lucide", Name: "circle-dot", Color: menuMakerDefaultColor}
}

type menuMakerIconResult struct {
	iconify bool
	name    string
	svg     string
}

type menuMakerIconPicker struct {
	slotID  string
	query   string
	prefix  string
	online  bool
	results []menuMakerIconResult
}

func searchWords(query string) []string { return strings.Fields(strings.ToLower(query)) }

func matchesWords(name string, words []string) bool {
	name = strings.ToLower(name)
	for _, word := range words {
		if !strings.Contains(name, word) {
			return false
		}
	}
	return true
}

func searchLucideIcons(query string) []menuMakerIconResult {
	words := searchWords(query)
	var out []menuMakerIconResult
	for _, name := range lucideIcons().names {
		if len(out) == lucideSearchLimit {
			break
		}
		if matchesWords(name, words) {
			out = append(out, menuMakerIconResult{name: name})
		}
	}
	return out
}

func (m *menuMakerTool) searchCachedIconify(query string) []menuMakerIconResult {
	words := searchWords(query)
	var out []menuMakerIconResult
	for _, prefix := range slices.Sorted(maps.Keys(m.iconify)) {
		icons := m.iconify[prefix]
		for _, name := range slices.Sorted(maps.Keys(icons)) {
			if len(out) == iconifyCachedLimit {
				return out
			}
			if matchesWords(name, words) {
				out = append(out, menuMakerIconResult{iconify: true, name: name, svg: icons[name]})
			}
		}
	}
	return out
}

func (m *menuMakerTool) iconifyStats() (count, size int, prefixes []string) {
	for prefix, icons := range m.iconify {
		if len(icons) > 0 {
			prefixes = append(prefixes, prefix)
		}
		for name, svg := range icons {
			count++
			size += len(name) + len(svg)
		}
	}
	slices.Sort(prefixes)
	return count, size, prefixes
}

func (m *menuMakerTool) refreshIconResults() {
	p := m.iconPicker
	if p == nil {
		return
	}
	p.results = append(searchLucideIcons(p.query), m.searchCachedIconify(p.query)...)
}

func (s *Shell) openMenuMakerIconPicker(slotID string) {
	m := &s.tools.menuMaker
	m.iconPicker = &menuMakerIconPicker{slotID: slotID}
	if m.iconify != nil {
		m.refreshIconResults()
		return
	}
	m.iconify = map[string]map[string]string{}
	m.refreshIconResults()
	s.background(func(ctx context.Context) {
		rows, err := s.deps.Settings.RendererStateWithPrefix(ctx, menuMakerIconifyKey)
		cache := map[string]map[string]string{}
		for key, raw := range rows {
			var icons map[string]string
			if json.Unmarshal([]byte(raw), &icons) == nil {
				cache[strings.TrimPrefix(key, menuMakerIconifyKey)] = icons
			}
		}
		fairy.Go(func() {
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.load-iconify-cache", T("page.tools.menu_maker.icon_offline"), nil)
			}
			for prefix, icons := range cache {
				if m.iconify[prefix] == nil {
					m.iconify[prefix] = icons
				} else {
					maps.Copy(m.iconify[prefix], icons)
				}
			}
			m.refreshIconResults()
		})
	})
}

// cacheIconify adds icons to the cache and stores the collections they belong to.
func (s *Shell) cacheIconify(icons []menumaker.MenuMakerIconifyIcon) {
	m := &s.tools.menuMaker
	changed := map[string]bool{}
	for _, icon := range icons {
		prefix, _, ok := strings.Cut(icon.Name, ":")
		if !ok || !menumaker.IsIconifyPrefix(prefix) {
			continue
		}
		if m.iconify[prefix] == nil {
			m.iconify[prefix] = map[string]string{}
		}
		m.iconify[prefix][icon.Name] = icon.SVG
		changed[prefix] = true
	}
	for prefix := range changed {
		s.storeIconifyPrefix(prefix)
	}
}

func (s *Shell) storeIconifyPrefix(prefix string) {
	icons := s.tools.menuMaker.iconify[prefix]
	if len(icons) == 0 {
		s.setRendererState(menuMakerIconifyKey+prefix, nil)
		return
	}
	data, err := json.Marshal(icons)
	if err != nil {
		return
	}
	value := string(data)
	s.setRendererState(menuMakerIconifyKey+prefix, &value)
}

func (s *Shell) searchIconifyOnline() {
	m := &s.tools.menuMaker
	p := m.iconPicker
	if p == nil || p.online || strings.TrimSpace(p.query) == "" {
		return
	}
	p.online = true
	query := p.query
	go func() {
		icons, err := s.deps.Tools.MenuMakerSearchIconify(context.Background(), query)
		fairy.Go(func() {
			p.online = false
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.iconify-search", T("page.tools.menu_maker.icon_offline"),
					map[string]any{"query": query})
			} else {
				s.cacheIconify(icons)
			}
			if m.iconPicker != p || p.query != query {
				return
			}
			p.results = searchLucideIcons(query)
			if err != nil {
				p.results = append(p.results, m.searchCachedIconify(query)...)
				return
			}
			for _, icon := range icons {
				p.results = append(p.results, menuMakerIconResult{iconify: true, name: icon.Name, svg: icon.SVG})
			}
		})
	}()
}

func (s *Shell) downloadIconifyCollection() {
	m := &s.tools.menuMaker
	p := m.iconPicker
	if p == nil || p.online || strings.TrimSpace(p.prefix) == "" {
		return
	}
	p.online = true
	prefix := p.prefix
	go func() {
		icons, err := s.deps.Tools.MenuMakerIconifyCollection(context.Background(), prefix)
		fairy.Go(func() {
			p.online = false
			if err != nil {
				s.menuMakerFailed(err, "menu-maker.iconify-collection", T("page.tools.menu_maker.icon_offline"),
					map[string]any{"prefix": prefix})
				return
			}
			s.cacheIconify(icons)
			m.refreshIconResults()
			s.toasts.Show(kit.ToastSuccess, T("page.tools.menu_maker.collection_cached", "count", len(icons)), "")
		})
	}()
}

func (s *Shell) toggleIconifyFavorite(prefix string) {
	m := &s.tools.menuMaker
	if i := slices.Index(m.favorites, prefix); i >= 0 {
		m.favorites = slices.Delete(m.favorites, i, i+1)
	} else {
		m.favorites = append(m.favorites, prefix)
	}
	data, err := json.Marshal(m.favorites)
	if err == nil {
		value := string(data)
		s.setRendererState(menuMakerFavoritesKey, &value)
	}
}

func (s *Shell) deleteIconifyPrefix(prefix string) {
	m := &s.tools.menuMaker
	delete(m.iconify, prefix)
	s.storeIconifyPrefix(prefix)
	m.refreshIconResults()
}

func (s *Shell) clearIconifyCache() {
	m := &s.tools.menuMaker
	for prefix := range m.iconify {
		delete(m.iconify, prefix)
		s.storeIconifyPrefix(prefix)
	}
	m.refreshIconResults()
}

func (s *Shell) pickMenuMakerIcon(result menuMakerIconResult) {
	m := &s.tools.menuMaker
	p := m.iconPicker
	icon := menuMakerIcon{Kind: "lucide", Name: result.name, Color: menuMakerDefaultColor}
	if result.iconify {
		svg, ok := menumaker.SanitizeIconifySVG(result.svg)
		if !ok {
			return
		}
		icon = menuMakerIcon{Kind: "iconify", Name: result.name, Color: menuMakerDefaultColor, SVG: svg}
	}
	m.iconPicker = nil
	s.menuMakerUpdateSlot(p.slotID, func(slot *menuMakerSlot) { slot.Icon, slot.image = icon, nil })
}

func formatIconBytes(size int) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	return fmt.Sprintf("%.1f KiB", float64(size)/1024)
}

func (s *Shell) menuMakerIconPickerDialog(cx *fairy.Cx) {
	m := &s.tools.menuMaker
	p := m.iconPicker
	if p == nil {
		return
	}
	t := kit.T()
	const prefix = "page.tools.menu_maker."
	search := fairy.Row(
		kit.TextField(p.query, func(value string) {
			p.query = value
			m.refreshIconResults()
		}, kit.Placeholder(T(prefix+"icon_search")), kit.OnSubmit(func(string) { s.searchIconifyOnline() })).
			Key("menu-maker.icon.query").Label(T(prefix+"icon_search")).AutoFocus().Grow(1).MinW(0),
		kit.Button(T(prefix+"online_search"), s.searchIconifyOnline, kit.Outline, kit.Icon(lucide.Search),
			kit.Loading(p.online), kit.Disabled(strings.TrimSpace(p.query) == "" || p.online)),
	).Gap(8).Align(fairy.Center)
	collection := fairy.Row(
		kit.TextField(p.prefix, func(value string) { p.prefix = value }, kit.Small,
			kit.Placeholder(T(prefix+"collection_prefix"))).
			Key("menu-maker.icon.prefix").Label(T(prefix+"collection_prefix")).Grow(1).MinW(0),
		kit.Button(T(prefix+"download_collection"), s.downloadIconifyCollection, kit.Outline, kit.Small,
			kit.Disabled(strings.TrimSpace(p.prefix) == "" || p.online)),
	).Gap(8).Align(fairy.Center)

	count, size, prefixes := m.iconifyStats()
	cache := fairy.Row(
		fairy.Text(T(prefix+"icon_cache", "count", count, "size", formatIconBytes(size))),
	).Wrap().Gap(4).RowGap(4).Align(fairy.Center).FontSize(11).Color(t.MutedForeground)
	for _, name := range prefixes {
		star := lucide.Star
		variant := kit.Outline
		if slices.Contains(m.favorites, name) {
			variant = kit.Secondary
		}
		cache.Child(fairy.Row(
			kit.Button(name, func() { s.toggleIconifyFavorite(name) }, variant, kit.Small, kit.Icon(star)),
			kit.IconButton(lucide.X, func() { s.deleteIconifyPrefix(name) }, kit.Outline, kit.Small,
				kit.Tooltip(T("g.delete"))).Label(T("g.delete")+" "+name),
		).Gap(2))
	}
	if count > 0 {
		cache.Child(fairy.Box().Grow(1), kit.Button(T(prefix+"clear_cache"), s.clearIconifyCache, kit.Ghost, kit.Small))
	}

	grid := fairy.Row().Wrap().Gap(8).RowGap(8)
	for _, result := range p.results {
		icon := menuMakerIcon{Kind: "lucide", Name: result.name}
		if result.iconify {
			icon = menuMakerIcon{Kind: "iconify", Name: result.name, SVG: result.svg}
		}
		cell := fairy.Box().Size(72, 72).Center().Radius(t.RadiusMd()).Border(1, t.Border).HoverFill(hoverFill(t)).
			Cursor(fairy.CursorPointer).Role(fairy.RoleButton).Focusable().Label(result.name).
			OnClick(func() { s.pickMenuMakerIcon(result) })
		if vec := menuMakerIconVector(icon); vec != nil {
			cell.Child(fairy.Icon(vec).Size(24, 24).Color(t.Foreground))
		}
		grid.Child(cell.Key("menu-maker.icon." + result.name))
	}

	kit.ShowDialog(cx, kit.DialogProps{
		Title: T(prefix + "icon_picker"), Width: 672, Key: "menu-maker.icon-picker",
		OnClose: func() { m.iconPicker = nil },
		Body: fairy.Col(
			search,
			fairy.Text(T(prefix+"online_policy")).FontSize(12).Color(t.MutedForeground).Wrap(),
			collection,
			cache,
			fairy.Col(grid).H(400).ScrollY(),
		).Gap(8),
	})
}
