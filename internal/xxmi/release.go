package xxmi

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"nahida.live/desktop/internal/github"
)

var releaseSignaturePattern = regexp.MustCompile(
	`(?m)^## Signature[\r\n]+- ((?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{4}|[A-Za-z0-9+/]{3}=|[A-Za-z0-9+/]{2}==))$`,
)

type ReleaseInfo struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	PublishedAt string `json:"publishedAt"`
	Prerelease  bool   `json:"prerelease"`
	Signed      bool   `json:"signed"`
	Notes       string `json:"notes"`
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
		return ""
	}
	end := strings.Index(body, "## Signature")
	if end < 0 || end <= start {
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
	return x.listReleases(ctx, pkg, false)
}

func (x *XXMI) listReleases(ctx context.Context, pkg string, refresh bool) ([]ReleaseInfo, error) {
	var repo github.Repo
	switch pkg {
	case "xxmi-libs":
		repo = libsRepo
	case "gi-fps-unlocker":
		repo = github.Repo{Owner: "SpectrumQT", Name: "GI-FPS-Unlocker-Package"}
	default:
		key, ok := strings.CutPrefix(pkg, "importer:")
		if !ok {
			return nil, fmt.Errorf("unknown XXMI package %q", pkg)
		}
		spec, ok := lookupImporterPackage(key)
		if !ok {
			return nil, fmt.Errorf("unknown XXMI package %q", pkg)
		}
		repo = spec.repo
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
		if release.Draft || strings.EqualFold(release.TagName, "main") ||
			strings.EqualFold(release.TagName, "master") || (release.Prerelease && !includePrereleases) {
			continue
		}
		info := releaseInfo(release)
		if info.Version != "" {
			out = append(out, info)
		}
	}
	return out, nil
}
