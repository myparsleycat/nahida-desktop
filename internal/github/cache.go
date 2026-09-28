package github

import (
	"context"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// tagRefreshCooldown bounds how often an explicit refresh refetches a list.
const tagRefreshCooldown = time.Minute

type tagEntry struct {
	releases []Release
	fetched  time.Time
}

type tagCache struct {
	mu      sync.Mutex
	entries map[Repo]tagEntry
	group   singleflight.Group
	now     func() time.Time
}

// ReleaseTags returns the release tags of repo (see VersionTags), cached for
// the process lifetime. A refresh refetches only once the cached list is older
// than one minute, and concurrent fetches of the same list share one request.
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

// CachedReleases returns published release metadata. Ordinary reads use the
// process cache; an explicit refresh observes a one-minute cooldown.
func (c *Client) CachedReleases(ctx context.Context, repo Repo, refresh bool) ([]Release, error) {
	if err := repo.Validate(); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errHTTPNotConfigured
	}
	c.tags.mu.Lock()
	entry, found := c.tags.entries[repo]
	now := c.tags.now()
	c.tags.mu.Unlock()
	if found && (!refresh || now.Sub(entry.fetched) < tagRefreshCooldown) {
		return cloneReleases(entry.releases), nil
	}

	// The shared fetch outlives any single caller so one cancellation does not
	// fail every waiter; each caller still stops waiting on its own context.
	fetchCtx := context.WithoutCancel(ctx)
	flight := c.tags.group.DoChan(repo.String(), func() (any, error) {
		c.tags.mu.Lock()
		entry, found := c.tags.entries[repo]
		fresh := found && (!refresh || c.tags.now().Sub(entry.fetched) < tagRefreshCooldown)
		c.tags.mu.Unlock()
		if fresh {
			return entry.releases, nil
		}
		releases, err := c.AllReleases(fetchCtx, repo)
		if err != nil {
			return nil, err
		}
		c.tags.mu.Lock()
		c.tags.entries[repo] = tagEntry{releases: releases, fetched: c.tags.now()}
		c.tags.mu.Unlock()
		return releases, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-flight:
		if result.Err != nil {
			return nil, result.Err
		}
		releases, _ := result.Val.([]Release)
		return cloneReleases(releases), nil
	}
}

func cloneReleases(releases []Release) []Release {
	cloned := slices.Clone(releases)
	for index := range cloned {
		cloned[index].Assets = slices.Clone(cloned[index].Assets)
	}
	return cloned
}
