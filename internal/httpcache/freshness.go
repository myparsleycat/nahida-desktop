package httpcache

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxHeuristicLifetime caps the freshness guessed from Last-Modified, as Firefox does.
const maxHeuristicLifetime = 7 * 24 * time.Hour

// storedHeaders are the response headers an entry keeps: the ones freshness, validation and callers read.
var storedHeaders = []string{
	"Age", "Cache-Control", "Content-Type", "Date", "ETag", "Expires", "Last-Modified", "Pragma", "Vary",
}

// cacheControl parses the Cache-Control directives of header into lower-case names and unquoted values.
func cacheControl(header http.Header) map[string]string {
	directives := map[string]string{}
	for _, line := range header.Values("Cache-Control") {
		for part := range strings.SplitSeq(line, ",") {
			name, value, _ := strings.Cut(strings.TrimSpace(part), "=")
			if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
				directives[name] = strings.Trim(strings.TrimSpace(value), `"`)
			}
		}
	}
	return directives
}

func seconds(value string) (time.Duration, bool) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(min(n, int64(time.Duration(1<<62)/time.Second))) * time.Second, true
}

func headerTime(header http.Header, name string) (time.Time, bool) {
	value := header.Get(name)
	if value == "" {
		return time.Time{}, false
	}
	t, err := http.ParseTime(value)
	return t, err == nil
}

// storable reports whether a private cache may keep a response with this status and header (RFC 9111 §3).
// Only 200 responses are kept: they are the only ones the image loaders use.
func storable(status int, header http.Header) bool {
	if status != http.StatusOK {
		return false
	}
	if _, noStore := cacheControl(header)["no-store"]; noStore {
		return false
	}
	for _, vary := range header.Values("Vary") {
		for field := range strings.SplitSeq(vary, ",") {
			if strings.TrimSpace(field) == "*" {
				return false
			}
		}
	}
	return true
}

// lifetime is the freshness lifetime of e (RFC 9111 §4.2.1): max-age, then Expires minus Date, then a tenth of
// the time since Last-Modified, capped at maxHeuristicLifetime. s-maxage is for shared caches and is ignored.
func (e *entry) lifetime() time.Duration {
	if maxAge, ok := cacheControl(e.Header)["max-age"]; ok {
		d, _ := seconds(maxAge)
		return d
	}
	date, ok := headerTime(e.Header, "Date")
	if !ok {
		date = e.ResponseTime
	}
	if e.Header.Get("Expires") != "" {
		expires, ok := headerTime(e.Header, "Expires")
		if !ok || !expires.After(date) {
			return 0
		}
		return expires.Sub(date)
	}
	if modified, ok := headerTime(e.Header, "Last-Modified"); ok && date.After(modified) {
		return min(date.Sub(modified)/10, maxHeuristicLifetime)
	}
	return 0
}

// age is the current age of e at now (RFC 9111 §4.2.3).
func (e *entry) age(now time.Time) time.Duration {
	date, ok := headerTime(e.Header, "Date")
	if !ok {
		date = e.ResponseTime
	}
	apparent := max(0, e.ResponseTime.Sub(date))
	ageValue, _ := seconds(e.Header.Get("Age"))
	corrected := ageValue + max(0, e.ResponseTime.Sub(e.RequestTime))
	return max(apparent, corrected) + max(0, now.Sub(e.ResponseTime))
}

// noCache reports whether e must be validated before every use: Cache-Control no-cache, or Pragma no-cache
// without Cache-Control.
func (e *entry) noCache() bool {
	directives := cacheControl(e.Header)
	if _, ok := directives["no-cache"]; ok {
		return true
	}
	return len(directives) == 0 && strings.Contains(strings.ToLower(e.Header.Get("Pragma")), "no-cache")
}

func (e *entry) fresh(now time.Time) bool {
	return !e.noCache() && e.age(now) < e.lifetime()
}

// withinStaleWhileRevalidate reports whether stale e may be used while it is revalidated in the background.
func (e *entry) withinStaleWhileRevalidate(now time.Time) bool {
	if e.noCache() || !e.servableOnError() {
		return false
	}
	window, ok := seconds(cacheControl(e.Header)["stale-while-revalidate"])
	return ok && e.age(now) < e.lifetime()+window
}

// servableOnError reports whether stale e may be used when it cannot be validated (RFC 9111 §4.2.4).
func (e *entry) servableOnError() bool {
	directives := cacheControl(e.Header)
	_, mustRevalidate := directives["must-revalidate"]
	return !mustRevalidate && !e.noCache()
}

// refresh merges the headers of a 304 response into e (RFC 9111 §4.3.4) and restarts its age.
func (e *entry) refresh(header http.Header, requested, received time.Time) {
	for _, name := range storedHeaders {
		values := header.Values(name)
		switch {
		case len(values) > 0 && name != "Content-Type":
			e.Header[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		case name == "Age" || name == "Date":
			// The stored ones describe the earlier response and would age the refreshed entry.
			e.Header.Del(name)
		}
	}
	e.RequestTime, e.ResponseTime = requested, received
}

func keptHeaders(header http.Header) http.Header {
	kept := http.Header{}
	for _, name := range storedHeaders {
		if values := header.Values(name); len(values) > 0 {
			kept[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}
	return kept
}
