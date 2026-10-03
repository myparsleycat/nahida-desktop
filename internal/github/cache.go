package github

import (
	"context"

	"nahida.live/desktop/internal/infra"
)

// ReleaseTags reads stable release tags through the shared persistent metadata cache.
func (c *Client) ReleaseTags(ctx context.Context, repo Repo, refresh bool) ([]string, error) {
	releases, err := c.CachedReleases(ctx, repo, refresh)
	if err != nil {
		return nil, err
	}
	stable := releases[:0]
	for _, release := range releases {
		if !release.Prerelease {
			stable = append(stable, release)
		}
	}
	return VersionTags(stable), nil
}

// CachedReleases shares successful metadata for an hour, including across
// restarts. Explicit refreshes still obey the one-minute and failure cooldowns.
func (c *Client) CachedReleases(ctx context.Context, repo Repo, refresh bool) ([]Release, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errHTTPNotConfigured
	}
	return c.AllReleases(infra.WithGitHubRefresh(ctx, refresh), repo)
}
