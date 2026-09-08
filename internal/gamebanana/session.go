package gamebanana

import "context"

func (g *GameBanana) persistManualCookie(ctx context.Context, revision uint64, cookie string) bool {
	applied, err := g.updateCookie(ctx, revision, cookie)
	g.reportRecovery(err, "save-manual-cookie")
	return applied && err == nil
}

// cookieMu serializes persistent and in-memory state; network calls never hold it.
func (g *GameBanana) cookieSnapshot(ctx context.Context) (string, uint64, error) {
	g.cookieMu.Lock()
	defer g.cookieMu.Unlock()
	cookie, err := g.loadCookieLocked(ctx)
	return cookie, g.cookieRevision, err
}

func (g *GameBanana) updateCookie(ctx context.Context, revision uint64, cookie string) (bool, error) {
	g.cookieMu.Lock()
	defer g.cookieMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if revision != g.cookieRevision {
		return false, nil
	}
	if cookie == "" {
		return true, g.removeCookieLocked(ctx)
	}
	if g.cookieLoaded && cookie == g.sessionCookie {
		return true, nil
	}
	return true, g.saveCookieLocked(ctx, cookie)
}

// Invalidate pending responses before starting remote logout.
func (g *GameBanana) takeCookie(ctx context.Context) (string, error) {
	g.cookieMu.Lock()
	defer g.cookieMu.Unlock()
	cookie, err := g.loadCookieLocked(ctx)
	if err != nil {
		g.cookieRevision++
		return "", err
	}
	return cookie, g.removeCookieLocked(ctx)
}
