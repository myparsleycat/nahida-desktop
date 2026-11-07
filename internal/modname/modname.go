// Package modname owns the DISABLED folder-name prefix that 3DMigoto uses to skip a mod folder.
package modname

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// disabledPrefixRE accepts repeated prefixes such as "DISABLED DISABLED " or "disabled_disabled_", and requires a
// final separator so that a name like "disabledFoo" is not treated as disabled.
var disabledPrefixRE = regexp.MustCompile(`(?i)^(?:disabled[\s_]*)+[\s_]+`)

func IsDisabled(name string) bool {
	return disabledPrefixRE.MatchString(strings.TrimSpace(name))
}

func StripDisabled(name string) string {
	return strings.TrimSpace(disabledPrefixRE.ReplaceAllString(strings.TrimSpace(name), ""))
}

// DisabledPrefix returns the prefix exactly as written in name, or "" when name is not disabled.
func DisabledPrefix(name string) string {
	return disabledPrefixRE.FindString(strings.TrimSpace(name))
}

// Siblings counts the folders of one directory by their name without the DISABLED prefix.
type Siblings map[string]int

func NewSiblings(names []string) Siblings {
	siblings := make(Siblings, len(names))
	for _, name := range names {
		siblings[siblingKey(name)]++
	}
	return siblings
}

// EntrySiblings counts every entry except regular files, so folder links such as junctions count as folders.
func EntrySiblings(entries []os.DirEntry) Siblings {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	return NewSiblings(names)
}

// DirSiblings reads the folders of dir; a directory that cannot be read has no known siblings.
func DirSiblings(dir string) Siblings {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Siblings{}
	}
	return EntrySiblings(entries)
}

// Display returns the name shown to the user. The DISABLED prefix is dropped unless another folder of the same
// directory has the same name without it, because the two folders would otherwise look identical.
func (s Siblings) Display(name string) string {
	if s[siblingKey(name)] > 1 {
		return strings.TrimSpace(name)
	}
	return StripDisabled(name)
}

// DisplayName applies Siblings.Display to the folder at path, reading its directory only when it is disabled.
func DisplayName(path string) string {
	path = strings.TrimRight(path, `\/`)
	name := filepath.Base(path)
	if !IsDisabled(name) {
		return strings.TrimSpace(name)
	}
	return DirSiblings(filepath.Dir(path)).Display(name)
}

// Windows folder names are case-insensitive, so "foo" and "DISABLED Foo" are told apart only by the prefix.
func siblingKey(name string) string {
	return strings.ToLower(StripDisabled(name))
}
