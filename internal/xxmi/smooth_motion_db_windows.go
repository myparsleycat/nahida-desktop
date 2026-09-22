//go:build windows

package xxmi

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf16"
)

const (
	drsBaseProfileName  = "Base Profile"
	drsProfileMarker    = 0x0053
	drsDwordChunkMarker = 0x00A4
	drsWideChunkMarker  = 0x00A5
	// drsUserDwordFlags is the flag word stored on a user dword override.
	// Driver-written Smooth Motion values use the same flags.
	drsUserDwordFlags = 0x00001002
)

// errDRSProfileMissing means the executable's driver profile is not in the active nvdrsdb file.
var errDRSProfileMissing = errors.New("nvidia profile is not in the driver database")

// drsProfileSettings reads the dword settings of a named profile in an nvdrsdb image.
// NvAPI's GetSetting omits Smooth Motion (0xB0D384C0) even when the active driver
// database has it on the Base Profile, so the on-disk record is the source for that ID.
// The name is matched through structurally valid profiles so a name that merely appears
// inside another string cannot be read as the profile.
func drsProfileSettings(data []byte, profileName string) (map[uint32]uint32, bool) {
	for _, profile := range drsValidProfiles(data) {
		if profile.name == profileName {
			return profileSettingValues(data, profile), true
		}
	}
	return nil, false
}

// drsValidProfiles returns every structurally valid profile in an nvdrsdb image. Unlike
// drsDatabaseProfiles it does not require the profiles to be linked in a single chain, so a
// read is not lost when a chain link is malformed.
func drsValidProfiles(data []byte) []drsProfile {
	var profiles []drsProfile
	for offset := 0; offset+16 <= len(data); offset += 2 {
		if binary.LittleEndian.Uint16(data[offset:]) != drsProfileMarker {
			continue
		}
		if profile, ok := drsProfileAt(data, offset); ok {
			profiles = append(profiles, profile)
		}
	}
	return profiles
}

// profileSettingValues reads the dword chunks of a profile already validated by drsProfileAt.
func profileSettingValues(data []byte, profile drsProfile) map[uint32]uint32 {
	settings := make(map[uint32]uint32, profile.count)
	pos := profile.header + 16
	for range profile.count {
		marker := binary.LittleEndian.Uint16(data[pos:])
		size := int(binary.LittleEndian.Uint16(data[pos+2:]))
		if marker == drsDwordChunkMarker && size == 16 {
			id := binary.LittleEndian.Uint32(data[pos+4:])
			value := binary.LittleEndian.Uint32(data[pos+12:])
			settings[id] = value
		}
		pos += size
	}
	return settings
}

func applyFileSmoothMotion(
	sample smoothMotionSample,
	profileName string,
	profiles map[string]map[uint32]uint32,
) smoothMotionSample {
	if sample.profileFound && !sample.profile.present {
		if value, ok := profileValue(profiles, profileName); ok {
			sample.profile = storedDword{present: true, explicit: true, value: value}
		}
	}
	if !sample.global.present {
		if value, ok := profileValue(profiles, drsBaseProfileName); ok {
			sample.global = storedDword{present: true, explicit: true, value: value}
		}
	}
	return sample
}

func profileValue(profiles map[string]map[uint32]uint32, name string) (uint32, bool) {
	settings, ok := profiles[name]
	if !ok {
		return 0, false
	}
	value, ok := settings[smoothMotionSettingID]
	return value, ok
}

func readActiveDRSProfiles(names ...string) (map[string]map[uint32]uint32, error) {
	path, err := activeDRSDatabasePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	profiles := make(map[string]map[uint32]uint32, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		settings, found := drsProfileSettings(data, name)
		if found {
			profiles[name] = settings
		}
	}
	return profiles, nil
}

// activeDRSDatabasePath returns the active nvdrsdb file. ProgramData is required: joining an
// empty value would silently resolve a relative path against the working directory.
func activeDRSDatabasePath() (string, error) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		return "", errors.New("ProgramData is not set")
	}
	dir := filepath.Join(programData, "NVIDIA Corporation", "Drs")
	index := byte(0)
	if selector, err := os.ReadFile(
		filepath.Join(dir, "nvdrssel.bin"),
	); err == nil && len(selector) > 0 &&
		selector[0] == 1 {
		index = 1
	}
	return filepath.Join(dir, fmt.Sprintf("nvdrsdb%d.bin", index)), nil
}

type drsProfile struct {
	header      int
	count       int
	nameOff     int
	settingsEnd int
	name        string
}

// setProfileDword sets one dword on a profile inside an nvdrsdb image.
// NvAPI rejects Smooth Motion with NVAPI_SETTING_NOT_FOUND, so the explicit
// game-profile value has to be written in the same records the driver stores.
// An existing dword is updated in place. A missing one is inserted, and every
// absolute profile or name offset past the insertion point moves with it.
func setProfileDword(data []byte, profileName string, id, value uint32) ([]byte, error) {
	profiles := drsDatabaseProfiles(data)
	target, err := profileByName(profiles, profileName)
	if err != nil {
		return nil, err
	}
	if offset, ok := dwordSettingOffset(data, target, id); ok {
		out := bytes.Clone(data)
		binary.LittleEndian.PutUint32(out[offset+12:], value)
		return out, nil
	}

	insertAt := target.header + 16
	out := bytes.Clone(data)
	shiftProfileOffsets(out, profiles, insertAt)
	if err := growProfileHeader(out, target.header); err != nil {
		return nil, err
	}
	count := binary.LittleEndian.Uint32(out[target.header+8:])
	binary.LittleEndian.PutUint32(out[target.header+8:], count+1)
	return slices.Insert(out, insertAt, dwordSettingChunk(id, value)...), nil
}

func profileByName(profiles []drsProfile, name string) (drsProfile, error) {
	var (
		found   drsProfile
		matches int
	)
	for _, profile := range profiles {
		if profile.name != name {
			continue
		}
		matches++
		found = profile
	}
	if matches == 0 {
		return drsProfile{}, errDRSProfileMissing
	}
	if matches > 1 {
		return drsProfile{}, fmt.Errorf("nvidia profile %q appears more than once", name)
	}
	return found, nil
}

func dwordSettingOffset(data []byte, profile drsProfile, id uint32) (int, bool) {
	pos := profile.header + 16
	for range profile.count {
		marker := binary.LittleEndian.Uint16(data[pos:])
		size := int(binary.LittleEndian.Uint16(data[pos+2:]))
		settingID := binary.LittleEndian.Uint32(data[pos+4:])
		if settingID == id && marker == drsDwordChunkMarker && size == 16 {
			return pos, true
		}
		pos += size
	}
	return 0, false
}

func shiftProfileOffsets(data []byte, profiles []drsProfile, insertAt int) {
	anchors := make(map[uint32]struct{}, len(profiles)*2)
	for _, profile := range profiles {
		anchors[uint32(profile.header)] = struct{}{}
		anchors[uint32(profile.nameOff)] = struct{}{}
		if profile.nameOff >= insertAt {
			binary.LittleEndian.PutUint32(data[profile.header+12:], uint32(profile.nameOff)+16)
		}
	}
	extents := profileExtents(profiles)
	// Executable records start with 0x016E and store one absolute profile or name offset at +12.
	for offset := 0; offset+16 <= len(data); offset += 2 {
		if binary.LittleEndian.Uint16(data[offset:]) != 0x016E {
			continue
		}
		// 0x016E is a bare marker, so only trust records that sit outside every parsed
		// profile. A stray marker inside a setting or name must not be rewritten.
		if overlapsProfile(extents, offset) {
			continue
		}
		pointer := binary.LittleEndian.Uint32(data[offset+12:])
		if pointer < uint32(insertAt) {
			continue
		}
		if _, ok := anchors[pointer]; !ok {
			continue
		}
		binary.LittleEndian.PutUint32(data[offset+12:], pointer+16)
	}
}

// profileExtents returns the byte range each parsed profile occupies, from its header
// through the null terminator of its name.
func profileExtents(profiles []drsProfile) [][2]int {
	extents := make([][2]int, 0, len(profiles))
	for _, profile := range profiles {
		end := profile.nameOff + len(encodeUTF16(profile.name)) + 2
		extents = append(extents, [2]int{profile.header, end})
	}
	return extents
}

func overlapsProfile(extents [][2]int, offset int) bool {
	for _, extent := range extents {
		if offset < extent[1] && offset+16 > extent[0] {
			return true
		}
	}
	return false
}

func growProfileHeader(data []byte, header int) error {
	second := binary.LittleEndian.Uint16(data[header+2:])
	if second > 0xFFFF-16 {
		return errors.New("nvidia profile record is too large to extend")
	}
	binary.LittleEndian.PutUint16(data[header+2:], second+16)
	return nil
}

func dwordSettingChunk(id, value uint32) []byte {
	chunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(chunk[0:], drsDwordChunkMarker)
	binary.LittleEndian.PutUint16(chunk[2:], 16)
	binary.LittleEndian.PutUint32(chunk[4:], id)
	binary.LittleEndian.PutUint32(chunk[8:], drsUserDwordFlags)
	binary.LittleEndian.PutUint32(chunk[12:], value)
	return chunk
}

func drsDatabaseProfiles(data []byte) []drsProfile {
	var best []drsProfile
	for offset := 0; offset+16 <= len(data); offset += 2 {
		if binary.LittleEndian.Uint16(data[offset:]) != drsProfileMarker {
			continue
		}
		chain := profileChain(data, offset)
		if len(chain) > len(best) {
			best = chain
		}
		if len(chain) > 0 {
			offset = chain[len(chain)-1].header
		}
	}
	return best
}

// profileChain follows the uint16 at header+2, which is the distance to the next profile header.
func profileChain(data []byte, offset int) []drsProfile {
	profiles := make([]drsProfile, 0)
	seen := make(map[int]struct{})
	for offset+16 <= len(data) {
		if _, ok := seen[offset]; ok {
			break
		}
		profile, ok := drsProfileAt(data, offset)
		if !ok {
			break
		}
		seen[offset] = struct{}{}
		profiles = append(profiles, profile)
		step := int(binary.LittleEndian.Uint16(data[offset+2:]))
		if step < 16 {
			break
		}
		next := offset + step
		if next <= offset || next+2 > len(data) || binary.LittleEndian.Uint16(data[next:]) != drsProfileMarker {
			break
		}
		offset = next
	}
	return profiles
}

func drsProfileAt(data []byte, offset int) (drsProfile, bool) {
	if binary.LittleEndian.Uint16(data[offset:]) != drsProfileMarker {
		return drsProfile{}, false
	}
	count := binary.LittleEndian.Uint32(data[offset+8:])
	nameOff := binary.LittleEndian.Uint32(data[offset+12:])
	if count > 4096 || nameOff < uint32(offset+16) || int(nameOff)+2 > len(data) {
		return drsProfile{}, false
	}
	settingsEnd, ok := consumeSettingChunks(data, offset+16, int(count), int(nameOff))
	if !ok || !onlyZeros(data[settingsEnd:nameOff]) {
		return drsProfile{}, false
	}
	name, ok := utf16ZAt(data, int(nameOff))
	if !ok {
		return drsProfile{}, false
	}
	return drsProfile{
		header:      offset,
		count:       int(count),
		nameOff:     int(nameOff),
		settingsEnd: settingsEnd,
		name:        name,
	}, true
}

func consumeSettingChunks(data []byte, start, count, limit int) (int, bool) {
	pos := start
	for range count {
		if pos+12 > len(data) || pos+12 > limit {
			return 0, false
		}
		marker := binary.LittleEndian.Uint16(data[pos:])
		size := int(binary.LittleEndian.Uint16(data[pos+2:]))
		validMarker := marker == drsDwordChunkMarker || marker == drsWideChunkMarker
		if !validMarker || size < 12 || size%4 != 0 || pos+size > limit || pos+size > len(data) {
			return 0, false
		}
		pos += size
	}
	return pos, true
}

func onlyZeros(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

func utf16ZAt(data []byte, offset int) (string, bool) {
	if offset < 0 || offset%2 != 0 || offset+2 > len(data) {
		return "", false
	}
	units := make([]uint16, 0, 32)
	for pos := offset; pos+1 < len(data) && len(units) < 1024; pos += 2 {
		unit := binary.LittleEndian.Uint16(data[pos:])
		if unit == 0 {
			if len(units) == 0 {
				return "", false
			}
			return string(utf16.Decode(units)), true
		}
		units = append(units, unit)
	}
	return "", false
}

func encodeUTF16(value string) []byte {
	units := utf16.Encode([]rune(value))
	out := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(out[i*2:], unit)
	}
	return out
}

func mergeFileSmoothMotion(sample smoothMotionSample, profileName string) (smoothMotionSample, error) {
	profiles, err := readActiveDRSProfiles(profileName, drsBaseProfileName)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sample, nil
		}
		return smoothMotionSample{}, fmt.Errorf("read nvidia driver profile database: %w", err)
	}
	return applyFileSmoothMotion(sample, profileName, profiles), nil
}
