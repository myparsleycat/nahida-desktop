// Package github reads public GitHub release metadata and downloads release
// files through the application's network policy and shared core-rate gate.
package github

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	apiBaseURL = "https://api.github.com"
	webBaseURL = "https://github.com"
)

var (
	ErrInvalidRepo = errors.New("invalid GitHub repository")

	repoPartRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// Repo identifies a public GitHub repository.
type Repo struct {
	Owner string
	Name  string
}

func (r Repo) String() string { return r.Owner + "/" + r.Name }

// Validate rejects owner or name segments that could escape the repository path.
func (r Repo) Validate() error {
	for _, part := range []string{r.Owner, r.Name} {
		if !repoPartRE.MatchString(part) || part == "." || part == ".." {
			return fmt.Errorf("%w: %q", ErrInvalidRepo, r.String())
		}
	}
	return nil
}

func (r Repo) apiURL(suffix string) string {
	return apiBaseURL + "/repos/" + r.String() + "/" + suffix
}

func (r Repo) webURL() string { return webBaseURL + "/" + r.String() }

type Release struct {
	TagName     string  `json:"tag_name"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
	PublishedAt string  `json:"published_at"`
	ZipballURL  string  `json:"zipball_url"`
	Assets      []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	// Digest is GitHub's "sha256:<hex>" asset digest; older assets have none.
	Digest string `json:"digest"`
}

// ReleaseFileURL is the public download URL of a release asset by tag and name.
func ReleaseFileURL(repo Repo, tag, name string) string {
	return repo.webURL() + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}

// TagArchiveURL is the public source archive URL of a tag.
func TagArchiveURL(repo Repo, tag string) string {
	return repo.webURL() + "/archive/refs/tags/" + url.PathEscape(tag) + ".zip"
}

// VersionTags projects releases to their tags, dropping empty tags and rolling
// branch releases named after the default branch.
func VersionTags(releases []Release) []string {
	tags := make([]string, 0, len(releases))
	for _, release := range releases {
		tag := strings.TrimSpace(release.TagName)
		if tag == "" || strings.EqualFold(tag, "main") || strings.EqualFold(tag, "master") {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}
