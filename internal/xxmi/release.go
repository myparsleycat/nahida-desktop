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

func (x *XXMI) listReleases(ctx context.Context, pkg string, refresh bool) ([]ReleaseInfo, error) {
	var repo github.Repo
	var importer *importerPackageSpec
	switch pkg {
	case "xxmi-libs":
		repo = libsRepo
	case "gi-fps-unlocker":
		repo = github.Repo{Owner: "SpectrumQT", Name: "GI-FPS-Unlocker-Package"}
	default:
		if provider, ok := lookupOverlayPackage(pkg); ok {
			repo = provider.repo
			break
		}
		spec, ok := parseImporterPackageID(pkg)
		if !ok {
			return nil, fmt.Errorf("unknown XXMI package %q", pkg)
		}
		repo, importer = spec.repo, &spec
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
