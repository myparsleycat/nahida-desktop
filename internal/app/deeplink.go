package app

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
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
