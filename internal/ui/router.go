package ui

import "strings"

// Route is a top-level screen. Its value is the React frontend's path, which the start page setting and
// fn:navi events still use.
type Route string

const (
	RouteTransfer    Route = "/transfer"
	RouteGameBanana  Route = "/gamebanana"
	RouteMod         Route = "/mod"
	RouteTools       Route = "/tools"
	RouteAgent       Route = "/agent"
	RouteXXMI        Route = "/xxmi"
	RouteDrive       Route = "/drive/drive"
	RouteShare       Route = "/drive/share"
	RouteBackup      Route = "/backup"
	RouteSetting     Route = "/setting"
	RouteDevelopment Route = "/development"
	RouteAuth        Route = "/auth"
)

var routes = []Route{
	RouteTransfer, RouteGameBanana, RouteMod, RouteTools, RouteAgent, RouteXXMI,
	RouteDrive, RouteShare, RouteBackup, RouteSetting, RouteDevelopment, RouteAuth,
}

// RouteOf returns the route a frontend path such as "/drive/share/abc" or "/setting/gen" belongs to.
func RouteOf(path string) (Route, bool) {
	for _, r := range routes {
		if path == string(r) || strings.HasPrefix(path, string(r)+"/") {
			return r, true
		}
	}
	return "", false
}

// requiresSession reports whether the route needs a signed-in nahida.live session.
func (r Route) requiresSession() bool {
	return r == RouteDrive || r == RouteShare || r == RouteBackup
}

// Router keeps the current route and the back and forward history. History entries are locations, frontend paths
// such as /setting/network, because React gave each settings section, tool, XXMI page and drive folder a route of
// its own, so the back button stepped through them too.
type Router struct {
	current  Route
	location string
	// settled is false between Navigate and the first Record, which names where the route opened.
	settled       bool
	back, forward []string
}

const historyLimit = 50

func NewRouter(start Route) Router { return Router{current: start, location: string(start)} }

func (r *Router) Current() Route { return r.current }

func (r *Router) Location() string { return r.location }

// Navigate opens to and clears the forward history. Opening the current route does nothing.
func (r *Router) Navigate(to Route) {
	if to == r.current {
		return
	}
	r.push(string(to))
	r.settled = false
}

// Record notes the location the current screen shows. A new location of the current route becomes a history
// entry, except the first one after Navigate, which replaces the bare route it opened at.
func (r *Router) Record(location string) {
	if route, ok := RouteOf(location); !ok || route != r.current {
		return
	}
	switch {
	case location == r.location:
	case !r.settled:
		r.location = location
	default:
		r.push(location)
	}
	r.settled = true
}

func (r *Router) push(location string) {
	r.back = append(r.back, r.location)
	if len(r.back) > historyLimit {
		r.back = r.back[len(r.back)-historyLimit:]
	}
	r.forward = r.forward[:0]
	r.moveTo(location)
}

func (r *Router) moveTo(location string) {
	r.location, r.settled = location, true
	r.current, _ = RouteOf(location)
}

func (r *Router) Back() bool {
	if len(r.back) == 0 {
		return false
	}
	r.forward = append(r.forward, r.location)
	r.moveTo(r.back[len(r.back)-1])
	r.back = r.back[:len(r.back)-1]
	return true
}

func (r *Router) Forward() bool {
	if len(r.forward) == 0 {
		return false
	}
	r.back = append(r.back, r.location)
	r.moveTo(r.forward[len(r.forward)-1])
	r.forward = r.forward[:len(r.forward)-1]
	return true
}
