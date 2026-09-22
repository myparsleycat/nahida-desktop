//go:build windows

package xxmi

import (
	"encoding/binary"
	"testing"
)

func TestDRSProfileSettingsReadsProfileRecords(t *testing.T) {
	t.Parallel()
	file := []byte("pad.")
	file, _, _ = appendTestProfile(
		file,
		"Base Profile",
		0x0230,
		[][2]uint32{{0x1057EB71, 1}, {smoothMotionSettingID, 1}},
	)
	file, _, _ = appendTestProfile(file, "Genshin Impact Launcher", 0x0230, [][2]uint32{{0xB0CC0875, 0}})
	file, _, _ = appendTestProfile(file, "Genshin Impact", 0x0230, [][2]uint32{{0x10E41E06, 0}})

	base, found := drsProfileSettings(file, "Base Profile")
	if !found || base[smoothMotionSettingID] != 1 {
		t.Fatalf("base = %v found=%v", base, found)
	}
	game, found := drsProfileSettings(file, "Genshin Impact")
	if !found {
		t.Fatal("genshin profile was not found")
	}
	if _, ok := game[smoothMotionSettingID]; ok {
		t.Fatalf("genshin profile absorbed another profile's setting: %v", game)
	}
	if game[0x10E41E06] != 0 {
		t.Fatalf("genshin setting = %v", game)
	}
	launcher, found := drsProfileSettings(file, "Genshin Impact Launcher")
	if !found || launcher[0xB0CC0875] != 0 {
		t.Fatalf("launcher = %v found=%v", launcher, found)
	}
}

func TestDRSProfileSettingsIgnoresNamesOutsideProfiles(t *testing.T) {
	t.Parallel()
	// A profile name that also ends another string must not be read as a profile record.
	file := dwordSettingChunk(0xDEADBEEF, 1)
	file = append(file, utf16Z("Old Base Profile")...)
	file, _, _ = appendTestProfile(file, "Base Profile", 0x0230, [][2]uint32{{smoothMotionSettingID, 1}})

	base, found := drsProfileSettings(file, "Base Profile")
	if !found || base[smoothMotionSettingID] != 1 {
		t.Fatalf("base = %v found=%v", base, found)
	}
}

func TestApplyFileSmoothMotionUsesBaseWhenTheGameHasNoValue(t *testing.T) {
	t.Parallel()
	sample := applyFileSmoothMotion(
		smoothMotionSample{profileFound: true},
		"Genshin Impact",
		map[string]map[uint32]uint32{
			"Base Profile":   {smoothMotionSettingID: 1},
			"Genshin Impact": {0x10E41E06: 0},
		},
	)
	if !smoothMotionApplied(sample) {
		t.Fatalf("sample = %+v", sample)
	}

	explicitOff := applyFileSmoothMotion(
		smoothMotionSample{profileFound: true},
		"Genshin Impact",
		map[string]map[uint32]uint32{
			"Base Profile":   {smoothMotionSettingID: 1},
			"Genshin Impact": {smoothMotionSettingID: 0},
		},
	)
	if smoothMotionApplied(explicitOff) {
		t.Fatalf("explicit off was ignored: %+v", explicitOff)
	}
}

func TestSetProfileDwordInsertsAndKeepsLaterProfiles(t *testing.T) {
	t.Parallel()
	file := append(exePointer(0), append(exePointer(0), exePointer(0)...)...)
	var gameAt, baseAt, baseName int
	file, gameAt, _ = appendTestProfile(file, "Game", 0x0110, [][2]uint32{{0, 7}, {0x10E41E06, 4}})
	file, baseAt, baseName = appendTestProfile(file, "Base Profile", 0x0230, [][2]uint32{{smoothMotionSettingID, 1}})
	binary.LittleEndian.PutUint32(file[12:], uint32(gameAt))
	binary.LittleEndian.PutUint32(file[32:], uint32(baseAt))
	binary.LittleEndian.PutUint32(file[52:], uint32(baseName))
	binary.LittleEndian.PutUint16(file[gameAt+2:], uint16(baseAt-gameAt))
	binary.LittleEndian.PutUint16(file[baseAt+2:], uint16(len(file)-baseAt))
	// A setting id that numerically matches the later header must stay put.
	binary.LittleEndian.PutUint32(file[gameAt+16+4:], uint32(baseAt))

	patched, err := setProfileDword(file, "Game", 0x0005F543, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(patched[12:]); got != uint32(gameAt) {
		t.Fatalf("game header pointer = %#x, want %#x", got, uint32(gameAt))
	}
	if got := binary.LittleEndian.Uint32(patched[32:]); got != uint32(baseAt)+16 {
		t.Fatalf("later header pointer = %#x, want %#x", got, uint32(baseAt)+16)
	}
	if got := binary.LittleEndian.Uint32(patched[52:]); got != uint32(baseName)+16 {
		t.Fatalf("later name pointer = %#x, want %#x", got, uint32(baseName)+16)
	}
	if got := binary.LittleEndian.Uint32(patched[gameAt+32+4:]); got != uint32(baseAt) {
		t.Fatalf("colliding setting id changed to %#x", got)
	}
	settings, found := drsProfileSettings(patched, "Game")
	if !found || settings[0x0005F543] != 2 || settings[0x10E41E06] != 4 {
		t.Fatalf("game settings = %v found=%v", settings, found)
	}
	baseSettings, found := drsProfileSettings(patched, "Base Profile")
	if !found || baseSettings[smoothMotionSettingID] != 1 {
		t.Fatalf("base settings = %v found=%v", baseSettings, found)
	}
	profiles := drsDatabaseProfiles(patched)
	if len(profiles) != 2 || profiles[0].count != 3 {
		t.Fatalf("profiles=%+v baseBytes=%x", profiles, patched[baseAt+16:baseAt+16+48])
	}

	updated, err := setProfileDword(patched, "Game", 0x0005F543, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != len(patched) {
		t.Fatalf("in-place update changed size %d -> %d", len(patched), len(updated))
	}
	settings, found = drsProfileSettings(updated, "Game")
	if !found || settings[0x0005F543] != 0 {
		t.Fatalf("updated game settings = %v found=%v", settings, found)
	}
}

func exePointer(target uint32) []byte {
	record := make([]byte, 20)
	binary.LittleEndian.PutUint16(record[0:], 0x016E)
	binary.LittleEndian.PutUint32(record[12:], target)
	return record
}

func appendTestProfile(file []byte, name string, kind uint32, settings [][2]uint32) ([]byte, int, int) {
	header := len(file)
	nameBytes := utf16Z(name)
	record := make([]byte, 16+16*len(settings)+len(nameBytes))
	binary.LittleEndian.PutUint16(record[0:], drsProfileMarker)
	binary.LittleEndian.PutUint16(record[2:], uint16(len(record)))
	binary.LittleEndian.PutUint32(record[4:], kind)
	binary.LittleEndian.PutUint32(record[8:], uint32(len(settings)))
	nameOff := header + 16 + 16*len(settings)
	binary.LittleEndian.PutUint32(record[12:], uint32(nameOff))
	for i, setting := range settings {
		copy(record[16+16*i:], dwordSettingChunk(setting[0], setting[1]))
	}
	copy(record[16+16*len(settings):], nameBytes)
	for len(record)%4 != 0 {
		record = append(record, 0)
	}
	return append(file, record...), header, nameOff
}

func utf16Z(value string) []byte {
	raw := append(encodeUTF16(value), 0, 0)
	return raw
}
