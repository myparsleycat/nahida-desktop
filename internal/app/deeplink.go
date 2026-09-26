package app

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"nahida.live/desktop/internal/drive"
)

const maxJavaScriptSafeInteger = uint64(1<<53 - 1)

func parseNahidaDeepLink(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "nahida") {
		return ""
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "gamebanana":
		return parseGameBananaDeepLink(parsed)
	default:
		return ""
	}
}

func parseGameBananaDeepLink(parsed *url.URL) string {
	if id := gameBananaPathID(parsed.Path); validModID(id) {
		return "/gamebanana?mod=" + id
	}
	if id := parsed.Query().Get("id"); validModID(id) {
		return "/gamebanana?mod=" + id
	}
	source := parsed.Query().Get("url")
	gameBananaURL, err := url.Parse(source)
	if err != nil || (gameBananaURL.Scheme != "http" && gameBananaURL.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(gameBananaURL.Hostname())
	if host != "gamebanana.com" && host != "www.gamebanana.com" {
		return ""
	}
	id := gameBananaSourcePathID(gameBananaURL.Path)
	if !validModID(id) {
		return ""
	}
	return "/gamebanana?mod=" + id
}

func gameBananaPathID(path string) string {
	parts := splitURLPath(path)
	if len(parts) != 2 {
		return ""
	}
	switch strings.ToLower(parts[0]) {
	case "mod", "mods", "open":
		return parts[1]
	default:
		return ""
	}
}

func gameBananaSourcePathID(path string) string {
	parts := splitURLPath(path)
	if len(parts) == 2 && strings.EqualFold(parts[0], "mods") {
		return parts[1]
	}
	return ""
}

func splitURLPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func validModID(value string) bool {
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return false
	}
	id, err := strconv.ParseUint(value, 10, 64)
	return err == nil && id > 0 && id <= maxJavaScriptSafeInteger
}

const (
	downloadDeepLinkVersion = "1"
	maxDeepLinkValueLength  = 4096
)

// deepLinkDownload is a web "download with Nahida Desktop" request carried by
// nahida://download?v=1&kind=<drive|link|mod>&id=<itemId>&dir=<0|1>&name=<name>
// plus linkId/linkToken for a shared link, or token/sig for a mod.
type deepLinkDownload struct {
	Kind  string
	ID    string
	IsDir bool
	Name  string
	Link  *drive.DownloadLink
	Mod   *drive.DownloadModAccess
}

func parseDownloadDeepLink(value string) *deepLinkDownload {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "nahida") || !strings.EqualFold(parsed.Hostname(), "download") {
		return nil
	}
	query := parsed.Query()
	for _, values := range query {
		for _, v := range values {
			if len(v) > maxDeepLinkValueLength {
				return nil
			}
		}
	}
	if query.Get("v") != downloadDeepLinkVersion {
		return nil
	}
	id := strings.TrimSpace(query.Get("id"))
	if id == "" {
		return nil
	}

	download := &deepLinkDownload{
		Kind:  query.Get("kind"),
		ID:    id,
		IsDir: query.Get("dir") != "0",
		Name:  strings.TrimSpace(query.Get("name")),
	}
	switch download.Kind {
	case "drive":
	case "link":
		linkID, token := query.Get("linkId"), query.Get("linkToken")
		if linkID == "" || token == "" {
			return nil
		}
		download.Link = &drive.DownloadLink{LinkID: linkID, Token: token}
	case "mod":
		download.IsDir = true
		download.Mod = &drive.DownloadModAccess{Token: query.Get("token"), Sig: query.Get("sig")}
	default:
		return nil
	}
	return download
}

func nahidaDeepLinkDownload(args []string) *deepLinkDownload {
	for _, arg := range args {
		if download := parseDownloadDeepLink(arg); download != nil {
			return download
		}
	}
	return nil
}

func nahidaDeepLinkRoute(args []string) string {
	for _, arg := range args {
		if route := parseNahidaDeepLink(arg); route != "" {
			return route
		}
	}
	return ""
}

func registerDeepLink(app *application.App, window *Window) {
	if app == nil || window == nil {
		return
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(event *application.ApplicationEvent) {
		if event == nil || event.Context() == nil {
			return
		}
		if route := parseNahidaDeepLink(event.Context().URL()); route != "" {
			window.FocusAndNavigate(route)
		}
	})
}
