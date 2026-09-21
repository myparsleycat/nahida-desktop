//go:build windows

package xxmi

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestFindNVAppApplication(t *testing.T) {
	t.Parallel()
	storage := nvAppApplicationStorage{Applications: []nvAppStoredApplication{
		{
			LocalID: 101,
			Application: nvAppApplicationInfo{
				DriverProfile: `F:\Games\Genshin Impact game\GenshinImpact.exe`,
				ImageFiles:    []string{`F:\Games\Genshin Impact game\GenshinImpact.exe`},
			},
		},
		{
			LocalID: 202,
			Application: nvAppApplicationInfo{
				DriverProfile: `F:\Games\Wuthering Waves Game\Wuthering Waves.exe`,
				ImageFiles: []string{
					`F:\Games\Wuthering Waves Game\Client\Binaries\Win64\Client-Win64-Shipping.exe`,
				},
			},
		},
	}}
	data, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		exe       string
		wantFound bool
		wantID    uint32
	}{
		{name: "full path", exe: `F:\Games\Genshin Impact game\GenshinImpact.exe`, wantFound: true, wantID: 101},
		{name: "driver profile basename", exe: "GenshinImpact.exe", wantFound: true, wantID: 101},
		{name: "image basename", exe: "Client-Win64-Shipping.exe", wantFound: true, wantID: 202},
		{name: "case insensitive", exe: "genshinimpact.EXE", wantFound: true, wantID: 101},
		{name: "not found", exe: "Missing.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app, found, err := findNVAppApplication(data, tt.exe)
			if err != nil {
				t.Fatal(err)
			}
			if found != tt.wantFound || app.LocalID != tt.wantID {
				t.Fatalf("application = %#v found=%v, want id=%d found=%v", app, found, tt.wantID, tt.wantFound)
			}
		})
	}
}

func TestFindNVAppApplicationRejectsAmbiguousBasename(t *testing.T) {
	t.Parallel()
	storage := nvAppApplicationStorage{Applications: []nvAppStoredApplication{
		{LocalID: 101, Application: nvAppApplicationInfo{DriverProfile: `F:\One\Game.exe`}},
		{LocalID: 202, Application: nvAppApplicationInfo{DriverProfile: `F:\Two\Game.exe`}},
	}}
	data, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = findNVAppApplication(data, "Game.exe")
	if err == nil || !strings.Contains(err.Error(), "multiple NVIDIA App applications") {
		t.Fatalf("error = %v", err)
	}
}

func TestFindNVAppApplicationRejectsBasenameAcrossApplications(t *testing.T) {
	t.Parallel()
	// The WWMI client executable is named Client-Win64-Shipping.exe and appears in the target
	// game's ImageFiles. Another application lists the same basename as its DriverProfile. The
	// profile field must not make the second application win, or launching the game would look
	// at the wrong profile and could disable Smooth Motion for an unrelated title.
	storage := nvAppApplicationStorage{Applications: []nvAppStoredApplication{
		{
			LocalID: 202,
			Application: nvAppApplicationInfo{
				DriverProfile: `F:\Games\Wuthering Waves Game\Wuthering Waves.exe`,
				ImageFiles: []string{
					`F:\Games\Wuthering Waves Game\Client\Binaries\Win64\Client-Win64-Shipping.exe`,
				},
			},
		},
		{
			LocalID: 303,
			Application: nvAppApplicationInfo{
				DriverProfile: `F:\Other\Client-Win64-Shipping.exe`,
			},
		},
	}}
	data, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = findNVAppApplication(data, "Client-Win64-Shipping.exe")
	if err == nil || !strings.Contains(err.Error(), "multiple NVIDIA App applications") {
		t.Fatalf("error = %v", err)
	}

	// An absolute path is precise and still resolves to the intended application.
	app, found, err := findNVAppApplication(
		data,
		`F:\Games\Wuthering Waves Game\Client\Binaries\Win64\Client-Win64-Shipping.exe`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || app.LocalID != 202 {
		t.Fatalf("application = %#v found=%v, want id=202", app, found)
	}
}

func TestNVAppFGXStateLayout(t *testing.T) {
	t.Parallel()
	if got := unsafe.Sizeof(nvAppFGXState{}); got != 2 {
		t.Fatalf("NVIDIA App FGX state size = %d, want 2", got)
	}
}

func TestNVAppFGXReadIntegration(t *testing.T) {
	exe := os.Getenv("NHD_NVAPP_FGX_EXE")
	if exe == "" {
		t.Skip("NHD_NVAPP_FGX_EXE is not set")
	}
	enabled, handled, err := readNVAppSmoothMotion(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatalf("NVIDIA App did not resolve %s", exe)
	}
	t.Logf("NVIDIA App Smooth Motion enabled for %s: %v", exe, enabled)
}

func TestWriteSmoothMotionOffWith(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		usedUnlisted        bool
		persistErr          error
		persistUnlistedErr  error
		applied             []bool
		readErr             error
		wantErr             error
		wantErrorText       string
		wantPersistUnlisted int
	}{
		{
			name:    "direct write applies",
			applied: []bool{false},
		},
		{
			name:         "initial imported override applies",
			usedUnlisted: true,
			applied:      []bool{false},
		},
		{
			name:                "direct write falls back to imported override",
			applied:             []bool{true, false},
			wantPersistUnlisted: 1,
		},
		{
			name:         "initial imported override remains enabled",
			usedUnlisted: true,
			applied:      []bool{true},
			wantErr:      errSmoothMotionWriteNotApplied,
		},
		{
			name:          "initial write fails",
			persistErr:    errors.New("save failed"),
			wantErrorText: "save failed",
		},
		{
			name:                "fallback write fails",
			applied:             []bool{true},
			persistUnlistedErr:  errors.New("import failed"),
			wantErrorText:       "import failed",
			wantPersistUnlisted: 1,
		},
		{
			name:                "fallback remains enabled",
			applied:             []bool{true, true},
			wantErr:             errSmoothMotionWriteNotApplied,
			wantPersistUnlisted: 1,
		},
		{
			name:          "verification fails",
			readErr:       errors.New("read failed"),
			wantErrorText: "verify nvidia smooth motion after saving: read failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			persistUnlistedCalls := 0
			readCalls := 0
			err := writeSmoothMotionOffWith(
				func() (bool, error) {
					return tt.usedUnlisted, tt.persistErr
				},
				func() error {
					persistUnlistedCalls++
					return tt.persistUnlistedErr
				},
				func() (bool, error) {
					if tt.readErr != nil {
						return false, tt.readErr
					}
					if readCalls >= len(tt.applied) {
						t.Fatalf("unexpected read %d", readCalls+1)
					}
					applied := tt.applied[readCalls]
					readCalls++
					return applied, nil
				},
			)
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErrorText != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErrorText)) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErrorText)
			}
			if tt.wantErr == nil && tt.wantErrorText == "" && err != nil {
				t.Fatal(err)
			}
			if persistUnlistedCalls != tt.wantPersistUnlisted {
				t.Fatalf("imported override writes = %d, want %d", persistUnlistedCalls, tt.wantPersistUnlisted)
			}
		})
	}
}

// Opt-in integration test. It reads the installed driver profile database and loads a patched
// copy into the live NVIDIA DRS session, so it must not run under the default `go test ./...`.
func TestPatchedSmoothMotionReloadsIntegration(t *testing.T) {
	if os.Getenv("NHD_NVDRS_RELOAD") == "" {
		t.Skip("NHD_NVDRS_RELOAD is not set")
	}
	path, err := activeDRSDatabasePath()
	if err != nil {
		t.Skip(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Skip(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	originalBase, originalBaseFound := drsProfileSettings(data, drsBaseProfileName)
	patched, err := setProfileDword(data, "Genshin Impact", smoothMotionSettingID, 0)
	if err != nil {
		t.Skip(err)
	}
	tmp, err := os.CreateTemp("", "nahida-nvdrs-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	tmpPath := tmp.Name()
	t.Cleanup(func() { _ = os.Remove(tmpPath) })
	if _, err := tmp.Write(patched); err != nil {
		_ = tmp.Close()
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	exported, err := os.CreateTemp("", "nahida-nvdrs-out-*.bin")
	if err != nil {
		t.Fatal(err)
	}
	exportedPath := exported.Name()
	_ = exported.Close()
	t.Cleanup(func() { _ = os.Remove(exportedPath) })

	err = withDRS(func(session uintptr, procs *drsProcs) error {
		if err := loadDRSSettingsFromFile(procs, session, tmpPath); err != nil {
			return err
		}
		file, err := windows.UTF16PtrFromString(exportedPath)
		if err != nil {
			return err
		}
		saveToFile, err := nvProc(0x2BE25DF8)
		if err != nil {
			return err
		}
		if status := nvCall(saveToFile, session, uintptr(unsafe.Pointer(file))); status != nvapiOK {
			return fmt.Errorf("nvidia save settings file: status %d", status)
		}
		return nil
	})
	if errors.Is(err, errNVIDIAUnavailable) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatalf("active driver database changed: %s -> %s", before.ModTime(), after.ModTime())
	}
	reloaded, err := os.ReadFile(exportedPath)
	if err != nil {
		t.Fatal(err)
	}
	game, found := drsProfileSettings(reloaded, "Genshin Impact")
	if !found || game[smoothMotionSettingID] != 0 {
		t.Fatalf("reloaded genshin = %v found=%v", game, found)
	}
	base, found := drsProfileSettings(reloaded, drsBaseProfileName)
	if found != originalBaseFound || (found && base[smoothMotionSettingID] != originalBase[smoothMotionSettingID]) {
		t.Fatalf("reloaded base = %v found=%v, original %v found=%v", base, found, originalBase, originalBaseFound)
	}
}

func TestNVDRSStructLayout(t *testing.T) {
	t.Parallel()
	if got, want := unsafe.Sizeof(nvDRSSetting{}), uintptr(12320); got != want {
		t.Fatalf("NVDRS_SETTING size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(nvDRSApplication{}), uintptr(20492); got != want {
		t.Fatalf("NVDRS_APPLICATION_V4 size = %d, want %d", got, want)
	}
	if got, want := unsafe.Sizeof(nvDRSProfile{}), uintptr(4116); got != want {
		t.Fatalf("NVDRS_PROFILE size = %d, want %d", got, want)
	}
	if nvDRSSettingVersion != 12320|(1<<16) {
		t.Fatalf("setting version = %#x", nvDRSSettingVersion)
	}
	if nvDRSApplicationVersion != 20492|(4<<16) {
		t.Fatalf("application version = %#x", nvDRSApplicationVersion)
	}
	if nvDRSProfileVersion != 4116|(1<<16) {
		t.Fatalf("profile version = %#x", nvDRSProfileVersion)
	}
}
