package menumaker

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"nahida.live/desktop/internal/infra"
)

const (
	iconifyAPI           = "https://api.iconify.design"
	iconifyCollectionURL = "https://cdn.jsdelivr.net/npm/@iconify-json/%s/icons.json"
	iconifySearchLimit   = 64
	maxIconifySearch     = 1 << 20
	maxIconifySVG        = 256 << 10
	maxIconifyCollection = 64 << 20
)

var (
	ErrIconifyUnavailable = errors.New("MENU_MAKER_ICONIFY_UNAVAILABLE")
	ErrIconifyPrefix      = errors.New("MENU_MAKER_ICONIFY_PREFIX")

	iconifyPrefixPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	iconifyNamePattern   = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*:[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

	unsafeSVGTag      = regexp.MustCompile(`(?i)<(?:script|foreignObject|iframe|object|embed|image|use|style|link|a)\b`)
	unsafeSVGHandler  = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	unsafeSVGRef      = regexp.MustCompile(`(?i)\s(?:href|src)\s*=\s*["']\s*(?:javascript:|data:|https?:|//)`)
	unsafeSVGURL      = regexp.MustCompile(`(?i)url\s*\(`)
	svgStart          = regexp.MustCompile(`(?i)^\s*<svg\b`)
	safeSVGTags       = []string{"svg", "g", "path", "circle", "ellipse", "line", "polyline", "polygon", "rect"}
	safeSVGAttributes = []string{
		"xmlns", "viewBox", "width", "height", "fill", "fill-opacity", "stroke", "stroke-width", "stroke-linecap",
		"stroke-linejoin", "stroke-opacity", "d", "cx", "cy", "r", "rx", "ry", "x", "y", "x1", "x2", "y1", "y2",
		"points", "transform",
	}
)

// MenuMakerIconifyIcon is an Iconify icon whose SVG passed SanitizeIconifySVG.
type MenuMakerIconifyIcon struct {
	Name string `json:"name"`
	SVG  string `json:"svg"`
}

// IsIconifyPrefix reports whether prefix names an Iconify collection.
func IsIconifyPrefix(prefix string) bool { return iconifyPrefixPattern.MatchString(prefix) }

// SearchIconify asks the Iconify API for icons matching query and downloads their SVG. Icons whose SVG cannot
// be fetched or is unsafe are left out.
func (m *MenuMaker) SearchIconify(ctx context.Context, query string) ([]MenuMakerIconifyIcon, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	searchURL := fmt.Sprintf("%s/search?query=%s&limit=%d", iconifyAPI, url.QueryEscape(query), iconifySearchLimit)
	body, err := m.fetchIconify(ctx, searchURL, maxIconifySearch)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Icons []string `json:"icons"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode search: %w", ErrIconifyUnavailable, err)
	}
	names := slices.DeleteFunc(payload.Icons, func(name string) bool { return !iconifyNamePattern.MatchString(name) })

	icons := make([]MenuMakerIconifyIcon, len(names))
	var failed sync.Once
	var firstErr error
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(8)
	for i, name := range names {
		group.Go(func() error {
			prefix, icon, _ := strings.Cut(name, ":")
			svgURL := fmt.Sprintf("%s/%s/%s.svg", iconifyAPI, url.PathEscape(prefix), url.PathEscape(icon))
			data, err := m.fetchIconify(groupCtx, svgURL, maxIconifySVG)
			if err != nil {
				if groupCtx.Err() != nil {
					return groupCtx.Err()
				}
				failed.Do(func() { firstErr = err })
				return nil
			}
			if svg, ok := SanitizeIconifySVG(string(data)); ok {
				icons[i] = MenuMakerIconifyIcon{Name: name, SVG: svg}
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	icons = slices.DeleteFunc(icons, func(icon MenuMakerIconifyIcon) bool { return icon.SVG == "" })
	if len(icons) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return icons, nil
}

// IconifyCollection downloads every icon of the Iconify collection prefix.
func (m *MenuMaker) IconifyCollection(ctx context.Context, prefix string) ([]MenuMakerIconifyIcon, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if !IsIconifyPrefix(prefix) {
		return nil, ErrIconifyPrefix
	}
	body, err := m.fetchIconify(ctx, fmt.Sprintf(iconifyCollectionURL, prefix), maxIconifyCollection)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Width  *float64 `json:"width"`
		Height *float64 `json:"height"`
		Icons  map[string]struct {
			Body string `json:"body"`
		} `json:"icons"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Icons == nil {
		return nil, fmt.Errorf("%w: invalid collection %s: %w", ErrIconifyUnavailable, prefix, err)
	}
	width := 24.0
	if payload.Width != nil {
		width = *payload.Width
	}
	height := width
	if payload.Height != nil {
		height = *payload.Height
	}

	icons := make([]MenuMakerIconifyIcon, 0, len(payload.Icons))
	for name, icon := range payload.Icons {
		if icon.Body == "" {
			continue
		}
		raw := fmt.Sprintf(
			`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g">%s</svg>`,
			width,
			height,
			icon.Body,
		)
		if svg, ok := SanitizeIconifySVG(raw); ok {
			icons = append(icons, MenuMakerIconifyIcon{Name: prefix + ":" + name, SVG: svg})
		}
	}
	slices.SortFunc(icons, func(a, b MenuMakerIconifyIcon) int { return strings.Compare(a.Name, b.Name) })
	return icons, nil
}

func (m *MenuMaker) fetchIconify(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	if m.http == nil {
		return nil, fmt.Errorf("%w: no HTTP client", ErrIconifyUnavailable)
	}
	resp, err := m.http.Fetch(ctx, rawURL, infra.FetchOptions{DisableHTTPErrors: true})
	if err != nil {
		return nil, infra.AnnotateError(err, infra.Diagnostic{
			Operation: "menu-maker.iconify", Stage: "request",
			Fields: map[string]any{"url": infra.SanitizeLogURL(rawURL)},
		})
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s returned %d", ErrIconifyUnavailable, infra.SanitizeLogURL(rawURL),
			resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrIconifyUnavailable,
			infra.SanitizeLogURL(rawURL), limit)
	}
	return data, nil
}

// SanitizeIconifySVG keeps the drawing elements and presentation attributes of an SVG and rejects markup that
// could load or run anything. Elements outside the allowed set are dropped with their content.
func SanitizeIconifySVG(svg string) (string, bool) {
	if !svgStart.MatchString(svg) || unsafeSVGTag.MatchString(svg) || unsafeSVGHandler.MatchString(svg) ||
		unsafeSVGRef.MatchString(svg) || unsafeSVGURL.MatchString(svg) {
		return "", false
	}

	decoder := xml.NewDecoder(strings.NewReader(svg))
	decoder.Strict = false
	var out strings.Builder
	skip, depth, root := 0, 0, false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", false
		}
		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if skip > 0 || !slices.Contains(safeSVGTags, t.Name.Local) || (depth == 1) != (t.Name.Local == "svg") {
				skip++
				continue
			}
			root = root || depth == 1
			out.WriteString("<" + t.Name.Local)
			for _, attr := range t.Attr {
				// Prefixed attributes such as xlink:href and xmlns:xlink are never kept.
				if attr.Name.Space != "" || !slices.Contains(safeSVGAttributes, attr.Name.Local) {
					continue
				}
				fmt.Fprintf(&out, ` %s="%s"`, attr.Name.Local, html.EscapeString(attr.Value))
			}
			out.WriteString(">")
		case xml.EndElement:
			depth--
			if skip > 0 {
				skip--
				continue
			}
			out.WriteString("</" + t.Name.Local + ">")
		}
	}
	if !root {
		return "", false
	}
	return out.String(), true
}
