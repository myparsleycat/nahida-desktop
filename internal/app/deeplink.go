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
	Kind   string
	ID     string
	IsDir  bool
	Name   string
	Link   *drive.DownloadLink
	Mod    *drive.DownloadModAccess
	Ticket string
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
	if query.Get("v") == "2" {
		ticket := query.Get("ticket")
		// Browsers may add a trailing slash when opening a URL with an empty path.
		if len(query) != 2 || len(query["ticket"]) != 1 || len(query["v"]) != 1 || len(ticket) != 43 ||
			(parsed.Path != "" && parsed.Path != "/") || parsed.Fragment != "" || parsed.User != nil || parsed.Port() != "" ||
			strings.IndexFunc(ticket, func(r rune) bool {
				return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_", r)
			}) != -1 {
			return nil
		}
		return &deepLinkDownload{Kind: "mod-ticket", Ticket: ticket, IsDir: true}
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

const maxAuthDeepLinkValueLength = 128

// parseAuthDeepLink reads nahida://auth?state=<state>&code=<code>, the link the
// web sign-in page opens to finish a login this app started.
func parseAuthDeepLink(value string) (state, code string, ok bool) {
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "nahida") || !strings.EqualFold(parsed.Hostname(), "auth") {
		return "", "", false
	}
	query := parsed.Query()
	state, code = query.Get("state"), query.Get("code")
	if state == "" || code == "" ||
		len(state) > maxAuthDeepLinkValueLength || len(code) > maxAuthDeepLinkValueLength {
		return "", "", false
	}
	return state, code, true
}

// dispatchDeepLinkLogin hands the nahida://auth links found in args to the
// login waiting for one, stopping at the link it accepts. Any page can open
// such a link, so one that no pending login asked for is dropped without a
// trace.
func (rt *runtime) dispatchDeepLinkLogin(args []string) {
	if rt.auth == nil {
		return
	}
	for _, arg := range args {
		if state, code, ok := parseAuthDeepLink(arg); ok && rt.auth.CompleteLogin(state, code) {
			return
		}
	}
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
