package blenderlink

import (
	"cmp"
	"embed"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	addonID     = "nahida_live_link"
	addonSource = "addon/" + addonID
	// addonRecord is the file in the installed add-on folder that lists the version and files the app wrote.
	addonRecord = ".nahida-install.json"
)

//go:embed addon/nahida_live_link/*.py addon/nahida_live_link/blender_manifest.toml
var addonFS embed.FS

var manifestVersionRE = regexp.MustCompile(`(?m)^version\s*=\s*"([^"]+)"`)

var bundledAddonVersion = sync.OnceValue(func() string {
	data, err := addonFS.ReadFile(addonSource + "/blender_manifest.toml")
	if err != nil {
		return ""
	}
	if match := manifestVersionRE.FindSubmatch(data); match != nil {
		return string(match[1])
	}
	return ""
})

// AddonVersion is the version of the add-on the app installs, as its blender_manifest.toml declares it.
func AddonVersion() string {
	return bundledAddonVersion()
}

// addonFiles returns the bundled add-on files by slash path relative to the add-on folder.
func addonFiles() (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(addonFS, addonSource, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := addonFS.ReadFile(name)
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(name, addonSource+"/")] = data
		return nil
	})
	return files, err
}

// blenderVersion is a Blender release series; its user configuration lives in a folder named after it.
type blenderVersion struct{ major, minor int }

// parseBlenderVersion reads "5.2" or "5.2.2".
func parseBlenderVersion(text string) (blenderVersion, bool) {
	parts := strings.Split(text, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return blenderVersion{}, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return blenderVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return blenderVersion{}, false
	}
	return blenderVersion{major, minor}, true
}

// Series is the release series of a Blender version such as 5.2.2, or empty when it cannot be read.
func Series(version string) string {
	if v, ok := parseBlenderVersion(version); ok {
		return v.String()
	}
	return ""
}

func (v blenderVersion) String() string {
	return strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor)
}

func (v blenderVersion) compare(other blenderVersion) int {
	return cmp.Or(cmp.Compare(v.major, other.major), cmp.Compare(v.minor, other.minor))
}

// extensions reports whether the release installs add-ons as extensions, which Blender 4.2 introduced.
func (v blenderVersion) extensions() bool {
	return v.compare(blenderVersion{4, 2}) >= 0
}

// supported reports whether the add-on runs on the release; its bl_info asks for 3.6.
func (v blenderVersion) supported() bool {
	return v.compare(blenderVersion{3, 6}) >= 0
}

// addonModule is the Python module name Blender enables the add-on under.
func (v blenderVersion) addonModule() string {
	if v.extensions() {
		return "bl_ext.user_default." + addonID
	}
	return addonID
}
