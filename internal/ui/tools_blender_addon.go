package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/kit"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/blenderlink"
	"nahida.live/desktop/internal/setting"
)

const (
	blenderAddonPrefix = "page.tools.blender_link.addon."
	// blenderAddonPollInterval is how often the open page looks again for Blender starting or quitting.
	blenderAddonPollInterval = 4 * time.Second
)

// blenderAddonTool holds the Blender releases found on this computer; requested is set once the page asked for
// them, and busy is the release whose add-on is being installed or removed.
type blenderAddonTool struct {
	targets   []blenderlink.AddonTarget
	requested bool
	loading   bool
	// again asks for another listing once the running one ends, which may predate the change that asked.
	again   bool
	polling bool
	// failed keeps a listing that keeps failing from showing a toast on every poll.
	failed bool
	busy   string
	// links is the set of connected Blender releases the listing last saw, and running the set of running ones.
	links   string
	running string
}

func (s *Shell) refreshBlenderAddons() {
	a := &s.tools.blenderLink.addon
	m := s.blenderLinkManager()
	a.requested = true
	if m == nil {
		return
	}
	if a.loading {
		a.again = true
		return
	}
	a.loading, a.again = true, false
	go func() {
		targets, err := m.AddonTargets(context.Background())
		fairy.Go(func() {
			a.loading = false
			if err != nil {
				if !a.failed {
					s.toasts.Show(kit.ToastError, T(blenderAddonPrefix+"load_failed"), err.Error())
				}
				a.failed = true
			} else {
				a.targets, a.failed = targets, false
				var running []string
				for _, target := range targets {
					if target.Running {
						running = append(running, target.Version)
					}
				}
				slices.Sort(running)
				a.running = strings.Join(running, ",")
			}
			if a.again {
				s.refreshBlenderAddons()
			}
		})
	}()
}

func (s *Shell) blenderLinkPageOpen() bool {
	return s.router.Current() == RouteTools && s.tools.active == toolBlenderLink
}

// pollBlenderAddons checks every interval, while the page is open and the window is shown, which releases run,
// and lists the releases again when that changed.
func (s *Shell) pollBlenderAddons() {
	a := &s.tools.blenderLink.addon
	m := s.blenderLinkManager()
	if a.polling || m == nil {
		return
	}
	a.polling = true
	time.AfterFunc(blenderAddonPollInterval, func() {
		fairy.Go(func() {
			if !s.blenderLinkPageOpen() {
				a.polling = false
				return
			}
			hidden := s.win != nil && (s.win.IsMinimized() || !s.win.IsVisible())
			if hidden || a.busy != "" || a.loading {
				a.polling = false
				s.pollBlenderAddons()
				return
			}
			go func() {
				running, err := m.RunningBlenders(context.Background())
				fairy.Go(func() {
					a.polling = false
					if err == nil && strings.Join(running, ",") != a.running {
						s.refreshBlenderAddons()
					}
					s.pollBlenderAddons()
				})
			}()
		})
	})
}

// blenderLinksChanged lists the releases again when a Blender release connects or disconnects, and forgets the
// choices of connections that ended.
func (s *Shell) blenderLinksChanged(state blenderlink.State) {
	b := &s.tools.blenderLink
	versions := make([]string, 0, len(state.Links))
	for _, link := range state.Links {
		versions = append(versions, blenderlink.Series(link.BlenderVersion))
	}
	slices.Sort(versions)
	if links := strings.Join(slices.Compact(versions), ","); links != b.addon.links {
		b.addon.links = links
		if b.addon.requested {
			s.refreshBlenderAddons()
		}
	}
	for session := range b.choices {
		if !slices.ContainsFunc(state.Links, func(link blenderlink.LinkState) bool { return link.Session == session }) {
			delete(b.choices, session)
		}
	}
}

// blenderAddonOlder reports whether an add-on version is older than the bundled one.
func blenderAddonOlder(version string) bool {
	return semver.Compare("v"+version, "v"+blenderlink.AddonVersion()) < 0
}

// blenderAddonUpdatable reports whether the link row may offer to update the add-on of a release series.
func (s *Shell) blenderAddonUpdatable(series string) bool {
	for _, target := range s.tools.blenderLink.addon.targets {
		if target.Version == series {
			state, _, _ := blenderAddonShown(target)
			return target.Blocked() == "" && blenderAddonAction(state) != ""
		}
	}
	return false
}

func (s *Shell) blenderAddonSection() *fairy.El {
	a := &s.tools.blenderLink.addon
	t := kit.T()
	if !a.requested {
		s.refreshBlenderAddons()
	}
	s.pollBlenderAddons()

	var rows []*fairy.El
	if len(a.targets) == 0 && !a.loading {
		rows = append(rows, fairy.Text(T(blenderAddonPrefix+"none")).FontSize(14).LineHeight(20).
			Color(t.MutedForeground).Wrap())
	}
	for _, target := range a.targets {
		rows = append(rows, s.blenderAddonRow(target))
	}
	rows = append(rows, fairy.Row(kit.Button(T(blenderAddonPrefix+"refresh"), s.refreshBlenderAddons,
		kit.Small, kit.Outline, kit.Icon(lucide.RefreshCw), kit.Loading(a.loading),
		kit.Disabled(a.loading || a.busy != ""))))
	return toolSection(T(blenderAddonPrefix+"title", "version", blenderlink.AddonVersion()), rows...)
}

// blenderAddonShown is the add-on state and version a release's row shows: the extension's, or the legacy copy's
// when that is the only copy the app installed.
func blenderAddonShown(target blenderlink.AddonTarget) (blenderlink.AddonState, string, bool) {
	if target.State == blenderlink.AddonMissing && target.LegacyCopy {
		return target.LegacyState, target.LegacyVersion, true
	}
	return target.State, target.InstalledVersion, false
}

func (s *Shell) blenderAddonRow(target blenderlink.AddonTarget) *fairy.El {
	t := kit.T()
	a := &s.tools.blenderLink.addon
	state, version, legacyOnly := blenderAddonShown(target)
	var status string
	switch state {
	case blenderlink.AddonMissing:
		status = T(blenderAddonPrefix + "state_missing")
	case blenderlink.AddonCurrent:
		status = T(blenderAddonPrefix+"state_current", "version", version)
	case blenderlink.AddonOutdated:
		status = T(blenderAddonPrefix+"state_outdated", "version", version)
	case blenderlink.AddonNewer:
		status = T(blenderAddonPrefix+"state_newer", "version", version)
	case blenderlink.AddonModified:
		status = T(blenderAddonPrefix + "state_modified")
	case blenderlink.AddonForeign:
		status = T(blenderAddonPrefix + "state_foreign")
	case blenderlink.AddonLinked:
		status = T(blenderAddonPrefix + "state_linked")
	}
	info := fairy.Col(
		fairy.Text("Blender "+target.Version).FontSize(14).LineHeight(20).Medium(),
		fairy.Text(status).FontSize(13).LineHeight(18).Color(t.MutedForeground),
	).Gap(2).Grow(1).MinW(0)

	blocked := target.Blocked()
	var notes []string
	if legacyOnly {
		notes = append(notes, T(blenderAddonPrefix+"legacy_note"))
	}
	switch {
	case blocked == blenderlink.AddonBlockLinked && target.State != blenderlink.AddonLinked:
		notes = append(notes, T(blenderAddonPrefix+"linked_conflict", "path", target.Conflict))
	case blocked == blenderlink.AddonBlockConflict:
		notes = append(notes, T(blenderAddonPrefix+"conflict", "path", target.Conflict))
	case blocked == blenderlink.AddonBlockRunning:
		notes = append(notes, T(blenderAddonPrefix+"close_blender", "version", target.Version))
	case blocked == "" && target.Executable == "":
		notes = append(notes, T(blenderAddonPrefix+"no_executable"))
	}
	for _, note := range notes {
		info.Child(fairy.Row(
			fairy.Icon(lucide.TriangleAlert).Size(14, 14).Color(t.MutedForeground).Shrink(0),
			fairy.Text(note).FontSize(13).LineHeight(18).Color(t.MutedForeground).Wrap().MinW(0),
		).Gap(6).Align(fairy.Start))
	}

	busy := a.busy == target.Version
	controls := fairy.Row().Gap(8).Align(fairy.Center).Shrink(0)
	if label := blenderAddonAction(state); label != "" {
		controls.Child(kit.Button(T(blenderAddonPrefix+label), func() { s.installBlenderAddon(target.Version) },
			kit.Small, kit.Icon(lucide.Download), kit.Loading(busy), kit.Disabled(a.busy != "" || blocked != "")))
	}
	owned := target.State != blenderlink.AddonMissing && target.State != blenderlink.AddonForeign &&
		target.State != blenderlink.AddonLinked
	if owned || target.LegacyCopy {
		controls.Child(kit.Button(T(blenderAddonPrefix+"remove"), func() { s.removeBlenderAddon(target.Version) },
			kit.Small, kit.Outline, kit.Disabled(a.busy != "" || target.Running)))
	}
	return fairy.Row(info, controls).Gap(12).Align(fairy.Center)
}

func blenderAddonAction(state blenderlink.AddonState) string {
	switch state {
	case blenderlink.AddonMissing:
		return "install"
	case blenderlink.AddonOutdated:
		return "update"
	case blenderlink.AddonModified, blenderlink.AddonForeign:
		return "reinstall"
	case blenderlink.AddonCurrent, blenderlink.AddonNewer, blenderlink.AddonLinked:
		return ""
	}
	return ""
}

func (s *Shell) installBlenderAddon(version string) {
	a := &s.tools.blenderLink.addon
	m := s.blenderLinkManager()
	if m == nil || a.busy != "" {
		return
	}
	a.busy = version
	go func() {
		result, err := m.InstallAddon(context.Background(), version)
		fairy.Go(func() {
			a.busy = ""
			s.refreshBlenderAddons()
			if err != nil {
				s.toasts.Show(kit.ToastError, T(blenderAddonPrefix+"install_failed"), blenderAddonError(err))
				return
			}
			message := blenderAddonPrefix + "installed"
			switch {
			case result.Restart:
				message = blenderAddonPrefix + "installed_restart"
			case result.ManualEnable:
				message = blenderAddonPrefix + "installed_manual"
			case result.Legacy:
				message = blenderAddonPrefix + "installed_legacy"
			}
			s.toasts.Show(kit.ToastSuccess, T(message, "version", version), "")
			if s.settingsLoaded(setting.KeyModelViewerBlenderLink) &&
				!s.boolSetting(setting.KeyModelViewerBlenderLink) {
				s.setSetting(setting.KeyModelViewerBlenderLink, true)
			}
		})
	}()
}

func (s *Shell) removeBlenderAddon(version string) {
	a := &s.tools.blenderLink.addon
	m := s.blenderLinkManager()
	if m == nil || a.busy != "" {
		return
	}
	a.busy = version
	go func() {
		err := m.RemoveAddon(context.Background(), version)
		fairy.Go(func() {
			a.busy = ""
			s.refreshBlenderAddons()
			if err != nil {
				s.toasts.Show(kit.ToastError, T(blenderAddonPrefix+"remove_failed"), blenderAddonError(err))
				return
			}
			s.toasts.Show(kit.ToastSuccess, T(blenderAddonPrefix+"removed", "version", version), "")
		})
	}()
}

func blenderAddonError(err error) string {
	switch {
	case errors.Is(err, blenderlink.ErrBlenderRunning):
		return T(blenderAddonPrefix + "running_error")
	case errors.Is(err, blenderlink.ErrAddonConflict):
		return T(blenderAddonPrefix + "conflict_error")
	case errors.Is(err, blenderlink.ErrAddonLinked):
		return T(blenderAddonPrefix + "linked_error")
	case errors.Is(err, blenderlink.ErrAddonNewer):
		return T(blenderAddonPrefix + "newer_error")
	}
	return err.Error()
}
