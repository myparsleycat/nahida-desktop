package reshade

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

var (
	repo      = github.Repo{Owner: "crosire", Name: "reshade"}
	versionRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// minVersion is the oldest release known to keep its settings beside a module named *.asi. Older
// ones look for them beside the game executable, which this app never writes to.
const minVersion = "6.5.0"

func supportedVersion(version string) bool {
	return versionRE.MatchString(version) && semver.Compare("v"+version, "v"+minVersion) >= 0
}

func containsVersion(versions []string, version string) bool {
	return slices.Contains(versions, version)
}

func sortVersions(versions []string) {
	slices.SortFunc(versions, func(a, b string) int { return semver.Compare("v"+b, "v"+a) })
}

// versions lists the supported ReShade versions, newest first. The repository tags versions without
// publishing releases.
func (r *ReShade) versions(ctx context.Context, refresh bool) ([]string, error) {
	if r.github == nil {
		return nil, errors.New("GitHub client is not configured")
	}
	ctx = infra.WithGitHubOperation(ctx, "reshade-list-versions")
	ctx = infra.WithGitHubRefresh(infra.WithGitHubStaleFallback(ctx), refresh)
	tags, err := r.github.Tags(ctx, repo)
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(tags))
	for _, tag := range tags {
		version := strings.TrimPrefix(tag, "v")
		if supportedVersion(version) && !containsVersion(versions, version) {
			versions = append(versions, version)
		}
	}
	sortVersions(versions)
	return versions, nil
}

func (r *ReShade) pinnedVersion(ctx context.Context) (string, error) {
	client, err := r.settings()
	if err != nil {
		return "", err
	}
	value, err := client.Settings.GetValue(ctx, versionKey)
	if err != nil || value == nil {
		return "", err
	}
	if !supportedVersion(*value) {
		return "", nil
	}
	return *value, nil
}

// targetVersion resolves the version to install: the pinned one, or the latest. Without the version
// list it falls back to the newest installed version, unless the caller asked for the list itself.
func (r *ReShade) targetVersion(ctx context.Context, requireLatest bool) (string, error) {
	pinned, err := r.pinnedVersion(ctx)
	if err != nil || pinned != "" {
		return pinned, err
	}
	versions, listErr := r.versions(ctx, false)
	if listErr == nil && len(versions) > 0 {
		return versions[0], nil
	}
	if listErr == nil {
		listErr = errors.New("no supported ReShade version is published")
	}
	if requireLatest || infra.IsCancellationError(listErr) {
		return "", listErr
	}
	installed, err := r.installedVersions()
	if err != nil {
		return "", err
	}
	if len(installed) == 0 {
		return "", listErr
	}
	return installed[0], nil
}
