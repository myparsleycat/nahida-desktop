package liquidemitter

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/platform"
)

var modKeyRE = regexp.MustCompile(`^[0-9a-f]{12}$`)

// profileIndex records which character mod owns each global profile slot.
type profileIndex struct {
	SchemaVersion int              `json:"schema_version"`
	Owners        map[string][]int `json:"owners"`
}

type profileEmitter struct {
	PID        int           `json:"pid"`
	INIPath    string        `json:"ini_path"`
	Section    string        `json:"section"`
	FirstIndex int           `json:"first_index"`
	BaseVertex int           `json:"base_vertex"`
	Row        [12]float64   `json:"gpu_row"`
	Offset     [3]float64    `json:"offset_local_m"`
	Anchor     EmitterAnchor `json:"anchor"`
}

// profileDocument is read by LoadProfilesJSON on the GPU, which scans only the
// first 2048 bytes for "enabled" and "gpu_data"; field order keeps them first.
type profileDocument struct {
	SchemaVersion int              `json:"schema_version"`
	Enabled       bool             `json:"enabled"`
	GPUData       [][]float64      `json:"gpu_data"`
	ModPath       string           `json:"mod_path"`
	Emitters      []profileEmitter `json:"emitters"`
	Look          *Look            `json:"look,omitempty"`
}

// A Look starts from and stays within the runtime's own settings of the same
// meaning, so a custom color starts from what the game shows.
const (
	settingStrength   = "strength"
	settingTint       = "tint"
	settingAbsorption = "absorptionDepth"
)

func runtimeSetting(id string) modruntime.Setting {
	for _, setting := range runtimeSettings() {
		if setting.ID == id {
			return setting
		}
	}
	panic("liquid emitter runtime has no setting " + id)
}

func defaultLook() Look {
	var look Look
	for index, channel := range strings.Fields(runtimeSetting(settingTint).Default) {
		if index < len(look.Tint) {
			look.Tint[index], _ = strconv.ParseFloat(channel, 64)
		}
	}
	look.AbsorptionMM, _ = strconv.ParseFloat(runtimeSetting(settingAbsorption).Default, 64)
	look.StreamWidth = 1
	return look
}

func validLook(look Look) bool {
	strength := runtimeSetting(settingStrength)
	if span := int(strength.Max - strength.Min); look.StrengthOffset < -span || look.StrengthOffset > span {
		return false
	}
	for _, channel := range look.Tint {
		if math.IsNaN(channel) || channel < 0 || channel > 1 {
			return false
		}
	}
	if !(look.StreamWidth >= minStreamWidth && look.StreamWidth <= maxStreamWidth) {
		return false
	}
	absorption := runtimeSetting(settingAbsorption)
	return look.AbsorptionMM >= absorption.Min && look.AbsorptionMM <= absorption.Max
}

// lookRecord is the tail LoadProfilesJSON reads after an emitter's 16 numbers:
// strength offset, custom color flag, tint, absorption depth in meters and
// stream width. A mod without its own look writes no tail, and the runtime
// then applies its global settings.
func lookRecord(look *Look) []float64 {
	if look == nil || look.StrengthOffset == 0 && !look.CustomColor && look.StreamWidth == 1 {
		return nil
	}
	custom := 0.0
	if look.CustomColor {
		custom = 1
	}
	return []float64{
		float64(look.StrengthOffset), custom,
		look.Tint[0], look.Tint[1], look.Tint[2],
		look.AbsorptionMM / 1000,
		look.StreamWidth,
	}
}

// modMarker ties a mod folder to its profile. ModPath is where the marker was
// written: a copied folder carries a marker naming another folder, which
// still shows after the profile itself was lost with the runtime folder.
type modMarker struct {
	Version int    `json:"version"`
	ModKey  string `json:"modKey"`
	ModPath string `json:"modPath,omitempty"`
	// Source is the original mod a new-mod apply copied this folder from and
	// disabled; ReenableSource is false when it was already disabled.
	Source         string `json:"source,omitempty"`
	ReenableSource bool   `json:"reenableSource,omitempty"`
}

type runtimeLayout struct{ root string }

func (r runtimeLayout) indexPath() string {
	return filepath.Join(r.root, profilesDir, profileIndexFile)
}
func (r runtimeLayout) loaderPath() string {
	return filepath.Join(r.root, filepath.FromSlash(profileLoaderFile))
}

func (r runtimeLayout) profilePath(key string) string {
	return filepath.Join(r.root, profilesDir, key+".json")
}

func readJSON(path string, target any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(bytesTrimBOM(data), target); err != nil {
		return false, fmt.Errorf("parse %q: %w", path, err)
	}
	return true, nil
}

func bytesTrimBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		return data[3:]
	}
	return data
}

func encodeJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// readIndex reads the slot table. When it is missing, as after the runtime
// folder was deleted and reinstalled, the hooks still left in marked mods
// keep their slots, so the table is rebuilt from them instead of starting
// empty and handing those slots to another mod.
func readIndex(layout runtimeLayout, modsRoot string) (profileIndex, error) {
	index := profileIndex{SchemaVersion: 1, Owners: map[string][]int{}}
	found, err := readJSON(layout.indexPath(), &index)
	if err != nil {
		return index, err
	}
	if index.Owners == nil {
		index.Owners = map[string][]int{}
	}
	if found {
		return index, nil
	}
	return index, recoverOwners(index, modsRoot)
}

func recoverOwners(index profileIndex, modsRoot string) error {
	const maxDepth = 6
	modsRoot = physicalPath(modsRoot)
	return filepath.WalkDir(modsRoot, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if name == modsRoot {
				return err
			}
			return nil
		}
		if entry.IsDir() {
			depth := strings.Count(strings.TrimPrefix(name, modsRoot), string(filepath.Separator))
			if name != modsRoot && (strings.HasPrefix(entry.Name(), ".") || depth > maxDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != modMarkerFile {
			return nil
		}
		// A marker that cannot be read reserves nothing.
		if key, markerErr := readModMarker(filepath.Dir(name)); markerErr == nil && key != "" {
			for _, hooks := range modHooks(filepath.Dir(name)) {
				for pid := range hooks {
					if !slices.Contains(index.Owners[key], pid) && index.ownerOf(pid) == "" {
						index.Owners[key] = append(index.Owners[key], pid)
					}
				}
			}
			slices.Sort(index.Owners[key])
		}
		return nil
	})
}

// modHooks maps each INI below root, by slash path relative to root, to the
// hooks it holds and the section holding each one. Unreadable files are
// skipped; this only reports what is there.
func modHooks(root string) map[string]map[int]string {
	hooks := make(map[string]map[int]string)
	_ = filepath.WalkDir(root, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr == nil && !entry.IsDir() && strings.EqualFold(filepath.Ext(name), ".ini") {
			if sections := fileHookSections(name); len(sections) > 0 {
				if rel, relErr := filepath.Rel(root, name); relErr == nil {
					hooks[filepath.ToSlash(rel)] = sections
				}
			}
		}
		return nil
	})
	return hooks
}

func fileHookSections(name string) map[int]string {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil
	}
	text, err := parseINIText(data)
	if err != nil {
		return nil
	}
	return text.hookSections()
}

// allocate returns the lowest slot no mod owns.
func (i profileIndex) allocate() (int, error) {
	used := make(map[int]bool)
	for _, pids := range i.Owners {
		for _, pid := range pids {
			used[pid] = true
		}
	}
	for pid := range maxProfiles {
		if !used[pid] {
			return pid, nil
		}
	}
	return 0, infra.ContractError(ErrorSlotsExhausted)
}

func (i profileIndex) ownerOf(pid int) string {
	for key, pids := range i.Owners {
		if slices.Contains(pids, pid) {
			return key
		}
	}
	return ""
}

func readProfile(layout runtimeLayout, key string) (profileDocument, error) {
	doc := profileDocument{SchemaVersion: 1, Enabled: true}
	_, err := readJSON(layout.profilePath(key), &doc)
	if doc.Look != nil && doc.Look.StreamWidth == 0 {
		doc.Look.StreamWidth = 1
	}
	return doc, err
}

func encodeProfile(doc profileDocument) ([]byte, error) {
	slices.SortFunc(doc.Emitters, func(a, b profileEmitter) int { return a.PID - b.PID })
	doc.SchemaVersion, doc.GPUData = 1, make([][]float64, 0, len(doc.Emitters))
	tail := lookRecord(doc.Look)
	for _, emitter := range doc.Emitters {
		record := make([]float64, 0, 16+len(tail))
		record = append(record, float64(emitter.PID))
		record = append(record, emitter.Row[:]...)
		record = append(record, emitter.Offset[:]...)
		doc.GPUData = append(doc.GPUData, append(record, tail...))
	}
	return encodeJSON(doc)
}

// loaderINI matches the binding list AttachmentEditor generated: one JSON
// byte buffer and one parse dispatch per registered mod.
func loaderINI(keys []string) []byte {
	lines := []string{
		"; Generated by Nahida Desktop. Emitter profiles are JSON files in ../Profiles.",
		`namespace = LiquidGravityProbe\JSONProfiles`,
		"[Constants]",
		"global $loaded = 0",
		"[CommandListInitialize]",
		"if !$loaded",
		`    clear = Resource\LiquidGravityProbe\SurfaceProfiles`,
		`    clear = Resource\LiquidGravityProbe\SurfaceModelOffsets`,
	}
	for index := range keys {
		lines = append(lines, fmt.Sprintf("    run = CustomShaderRead%d", index))
	}
	lines = append(lines, "    $loaded = 1", "endif", "")
	for index, key := range keys {
		lines = append(lines,
			fmt.Sprintf("[ResourceJSON%d]", index),
			"type = Buffer",
			"format = R8_UINT",
			fmt.Sprintf("filename = ../%s/%s.json", profilesDir, key),
			"",
			fmt.Sprintf("[CustomShaderRead%d]", index),
			"cs = LoadProfilesJSON.hlsl",
			"vs = null",
			"ps = null",
			"gs = null",
			"hs = null",
			"ds = null",
			fmt.Sprintf("cs-t0 = ResourceJSON%d", index),
			`cs-u0 = Resource\LiquidGravityProbe\SurfaceProfiles`,
			`cs-u1 = Resource\LiquidGravityProbe\SurfaceModelOffsets`,
			"dispatch = 1,1,1",
			"cs-t0 = null",
			"cs-u0 = null",
			"cs-u1 = null",
			"",
		)
	}
	return []byte(strings.Join(lines, "\r\n"))
}

// loaderKeys lists the owners with a profile file to bind. current is the
// owner whose file is being written (hasProfile) or dropped. Slots recovered
// from hooks after the profiles were lost stay reserved but have no file, and
// a binding to a missing file would fail to load.
func loaderKeys(layout runtimeLayout, index profileIndex, current string, hasProfile bool) []string {
	keys := make([]string, 0, len(index.Owners))
	for key, pids := range index.Owners {
		if len(pids) == 0 {
			continue
		}
		if key == current {
			if hasProfile {
				keys = append(keys, key)
			}
			continue
		}
		if _, err := os.Stat(layout.profilePath(key)); err == nil {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

func readModMarker(modRoot string) (string, error) {
	marker, err := readMarker(modRoot)
	return marker.ModKey, err
}

func readMarker(modRoot string) (modMarker, error) {
	var marker modMarker
	found, err := readJSON(filepath.Join(modRoot, modMarkerFile), &marker)
	if err != nil || !found {
		return modMarker{}, err
	}
	if !modKeyRE.MatchString(marker.ModKey) {
		return modMarker{}, fmt.Errorf("invalid liquid emitter key in %q", filepath.Join(modRoot, modMarkerFile))
	}
	return marker, nil
}

func samePhysicalPath(left, right string) bool {
	return platform.SamePathFold(physicalPath(left), physicalPath(right))
}

func newModKey() (string, error) {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
