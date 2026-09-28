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
	tags    []string
	fetched time.Time
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
		return slices.Clone(entry.tags), nil
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
			return entry.tags, nil
		}
		releases, err := c.Releases(fetchCtx, repo)
		if err != nil {
			return nil, err
		}
		tags := VersionTags(releases)
		c.tags.mu.Lock()
		c.tags.entries[repo] = tagEntry{tags: tags, fetched: c.tags.now()}
		c.tags.mu.Unlock()
		return tags, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-flight:
		if result.Err != nil {
			return nil, result.Err
		}
		tags, _ := result.Val.([]string)
		return slices.Clone(tags), nil
	}
}
