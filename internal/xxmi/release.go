package xxmi

import (
	"cmp"
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

var releaseSignaturePattern = regexp.MustCompile(
	`(?m)^## Signature[\r\n]+- ((?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{4}|[A-Za-z0-9+/]{3}=|[A-Za-z0-9+/]{2}==))\r?$`,
)

type ReleaseInfo struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	PublishedAt string `json:"publishedAt"`
	Prerelease  bool   `json:"prerelease"`
	Signed      bool   `json:"signed"`
	// Attested reports a release that publishes a build provenance bundle, which the install verifies in place of
	// a signature.
	Attested bool   `json:"attested"`
	Notes    string `json:"notes"`
}

// Verifiable reports whether the release installs without the user accepting an unverified package.
func (r ReleaseInfo) Verifiable() bool {
	return r.Signed || r.Attested
}

func releaseSignature(body string) string {
	match := releaseSignaturePattern.FindStringSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func releaseNotes(body string) string {
	body = strings.ReplaceAll(body, "## Warning", "")
	start := strings.Index(body, "##")
	if start < 0 {
		return strings.TrimSpace(body)
	}
	end := strings.Index(body, "## Signature")
	if end < 0 {
		end = len(body)
	}
	if end <= start {
		return ""
	}
	return strings.TrimSpace(body[start:end])
}

func releaseInfo(release github.Release) ReleaseInfo {
	return ReleaseInfo{
		Version: normalizeVersion(release.TagName), Tag: release.TagName,
		PublishedAt: release.PublishedAt, Prerelease: release.Prerelease,
		Signed: releaseSignature(release.Body) != "", Notes: releaseNotes(release.Body),
	}
}

func (x *XXMI) ListReleases(ctx context.Context, pkg string) ([]ReleaseInfo, error) {
	ctx = infra.WithGitHubOperation(ctx, "xxmi-list-releases")
	releases, err := x.listReleases(infra.WithGitHubStaleFallback(ctx), pkg, false)
	if err != nil {
		return nil, infra.ReportError(x.log, err, "XXMI.ListReleases", infra.Diagnostic{
			Operation: "list-releases", Stage: "release-metadata", Fields: map[string]any{"package": pkg},
		})
	}
	return releases, nil
}

// packageRepo returns the repository a package is released from, and the importer package it is, if any.
func packageRepo(pkg string) (github.Repo, *importerPackageSpec, error) {
	switch pkg {
	case "xxmi-libs":
		return libsRepo, nil, nil
	case "gi-fps-unlocker":
		return github.Repo{Owner: "SpectrumQT", Name: "GI-FPS-Unlocker-Package"}, nil, nil
	}
	if provider, ok := lookupOverlayPackage(pkg); ok {
		return provider.repo, nil, nil
	}
	spec, ok := parseImporterPackageID(pkg)
	if !ok {
		return github.Repo{}, nil, fmt.Errorf("unknown XXMI package %q", pkg)
	}
	return spec.repo, &spec, nil
}

// publishedPrerelease reports whether GitHub lists version of pkg as a prerelease. The release list leaves
// prereleases out unless they are enabled, so a deployed one otherwise reads as a stable release above latest.
func (x *XXMI) publishedPrerelease(ctx context.Context, pkg, version string) (bool, error) {
	if version == "" {
		return false, nil
	}
	repo, _, err := packageRepo(pkg)
	if err != nil {
		return false, err
	}
	releases, err := x.github.CachedReleases(infra.WithGitHubStaleFallback(ctx), repo, false)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(releases, func(release github.Release) bool {
		return release.Prerelease && normalizeVersion(release.TagName) == version
	}), nil
}

func (x *XXMI) listReleases(ctx context.Context, pkg string, refresh bool) ([]ReleaseInfo, error) {
	repo, importer, err := packageRepo(pkg)
	if err != nil {
		return nil, err
	}
	releases, err := x.github.CachedReleases(ctx, repo, refresh)
	if err != nil {
		return nil, err
	}
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	includePrereleases := false
	if client != nil {
		value, err := client.Settings.GetValue(ctx, "xxmi_include_prereleases")
		if err != nil {
			return nil, err
		}
		includePrereleases = value != nil && *value == "true"
	}
	out := make([]ReleaseInfo, 0, len(releases))
	for _, release := range releases {
		if strings.EqualFold(release.TagName, "main") ||
			strings.EqualFold(release.TagName, "master") || (release.Prerelease && !includePrereleases) {
			continue
		}
		info := releaseInfo(release)
		if importer != nil && importer.attestation != nil {
			info.Signed = false
			info.Attested = releaseAssetURL(release, fmt.Sprintf(importer.attestation.bundleFormat, info.Version)) != ""
		}
		if info.Version != "" {
			out = append(out, info)
		}
	}

	// Callers take the first release as the latest one, but GitHub does not list releases by version: a release
	// edited after a newer one was published can come first.
	slices.SortStableFunc(out, func(a, b ReleaseInfo) int {
		_, _, validA := releaseSemver(a.Version)
		_, _, validB := releaseSemver(b.Version)
		if validA != validB {
			if validA {
				return -1
			}
			return 1
		}
		order, _ := compareReleaseVersions(b.Version, a.Version)
		return order
	})
	return out, nil
}

// releaseSemver returns version as a semantic version and the revision of a fourth numeric component, which a
// package source appends to the official release it builds on, as in 1.2.2.1; the revision is -1 without one.
func releaseSemver(version string) (base string, revision int, ok bool) {
	parts := strings.Split(version, ".")
	if len(parts) != 4 || strings.ContainsAny(parts[2], "-+") {
		return "v" + version, -1, semver.IsValid("v" + version)
	}
	revision, err := strconv.Atoi(parts[3])
	if err != nil || revision < 0 || strings.ContainsAny(parts[3], "+-") {
		return "", 0, false
	}
	base = "v" + strings.Join(parts[:3], ".")
	return base, revision, semver.IsValid(base)
}

// compareReleaseVersions orders two versions by semantic version and then by revision. ok is false when either is
// not a version releaseSemver reads.
func compareReleaseVersions(a, b string) (order int, ok bool) {
	baseA, revisionA, okA := releaseSemver(a)
	baseB, revisionB, okB := releaseSemver(b)
	if !okA || !okB {
		return 0, false
	}
	if order := semver.Compare(baseA, baseB); order != 0 {
		return order, true
	}
	return cmp.Compare(revisionA, revisionB), true
}
