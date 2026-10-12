package infra

import (
	"os"
	"os/user"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
)

const (
	userNameToken     = "%USERNAME%"
	computerNameToken = "%COMPUTERNAME%"
	// A one-character name would match all over the text even at word
	// boundaries, and replacing it would cost more of the log than it protects.
	minIdentityRunes = 2
)

// usersDirPattern matches the profile folder name below a Users directory with
// any of the separators a log line carries: plain, JSON-escaped or forward. A
// folder name may hold spaces, so the name runs to the next separator; a path
// that ends at the folder loses the rest of its line with it.
var usersDirPattern = regexp.MustCompile(`(?i)([a-z]:(?:\\\\|[\\/])users(?:\\\\|[\\/]))([^\\/"'<>|:*?\r\n\t]+)`)

type logIdentity struct {
	pattern *regexp.Regexp
	token   string
}

var (
	logIdentitiesOnce sync.Once
	logIdentities     []logIdentity
)

// AnonymizeLogText prepares log text for leaving the machine. Records are
// already redacted when they are written; this pass repeats that for lines an
// older build wrote and also removes what the write path keeps: profile folder
// names outside the current home, the account name and the computer name.
//
//wails:ignore
func AnonymizeLogText(value string) string {
	return anonymizeLogText(value, currentHomeNeedles(), currentLogIdentities())
}

func anonymizeLogText(value string, homes []string, identities []logIdentity) string {
	value = redactSecrets(redactUserPaths(value, homes))
	value = usersDirPattern.ReplaceAllString(value, "${1}"+userNameToken)
	for _, identity := range identities {
		value = replaceWholeWord(value, identity)
	}
	return value
}

func currentLogIdentities() []logIdentity {
	logIdentitiesOnce.Do(func() {
		names := []string{os.Getenv("USERNAME")}
		if current, err := user.Current(); err == nil {
			// Windows reports DOMAIN\name; the domain is the computer name on
			// a machine without one and is covered below.
			names = append(names, current.Username[strings.LastIndex(current.Username, `\`)+1:], current.Name)
		}
		hosts := []string{os.Getenv("COMPUTERNAME")}
		if host, err := os.Hostname(); err == nil {
			hosts = append(hosts, host)
		}
		logIdentities = append(newLogIdentities(names, userNameToken), newLogIdentities(hosts, computerNameToken)...)
	})
	return logIdentities
}

func newLogIdentities(names []string, token string) []logIdentity {
	names = lo.Uniq(lo.FilterMap(names, func(name string, _ int) (string, bool) {
		name = strings.TrimSpace(name)
		return strings.ToLower(name), utf8.RuneCountInString(name) >= minIdentityRunes
	}))
	return lo.Map(names, func(name string, _ int) logIdentity {
		return logIdentity{pattern: regexp.MustCompile(`(?i)` + regexp.QuoteMeta(name)), token: token}
	})
}

// replaceWholeWord replaces the matches that are not part of a longer word, so
// an account named "ann" does not rewrite "channel" but still loses every place
// it stands on its own.
func replaceWholeWord(value string, identity logIdentity) string {
	matches := identity.pattern.FindAllStringIndex(value, -1)
	if len(matches) == 0 {
		return value
	}

	var result strings.Builder
	result.Grow(len(value))
	start := 0
	for _, match := range matches {
		before, _ := utf8.DecodeLastRuneInString(value[:match[0]])
		after, _ := utf8.DecodeRuneInString(value[match[1]:])
		if isWordRune(before) || isWordRune(after) {
			continue
		}
		result.WriteString(value[start:match[0]])
		result.WriteString(identity.token)
		start = match[1]
	}
	result.WriteString(value[start:])
	return result.String()
}

func isWordRune(r rune) bool {
	return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r))
}
