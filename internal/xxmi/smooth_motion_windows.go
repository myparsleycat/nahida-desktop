//go:build windows

package xxmi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	nvapiUnicodeStringMax = 2048
	// nvDRSValueBytes is sizeof(NVDRS_BINARY_SETTING): a uint32 length plus 4096 bytes.
	// Under nvapi.h pack(4) that is also the size of the setting value union.
	nvDRSValueBytes             = 4100
	nvDRSDWORDType              = 0
	nvDRSCurrentProfileLocation = 0

	nvapiOK                   int32 = 0
	nvapiLibraryNotFound      int32 = -2
	nvapiNVIDIADeviceNotFound int32 = -6
	nvapiSettingNotFound      int32 = -160
	nvapiProfileNotFound      int32 = -163
	nvapiProfileNameInUse     int32 = -164
	nvapiExecutableNotFound   int32 = -166

	// QueryInterface ids published with NvAPI. nvapi64.dll exports only nvapi_QueryInterface.
	nvapiInitializeID              = 0x0150E828
	nvapiUnloadID                  = 0xD22BDD7E
	nvapiDRSCreateSessionID        = 0x0694D52E
	nvapiDRSDestroySessionID       = 0x0DAD9CFF8
	nvapiDRSLoadSettingsID         = 0x375DBD6B
	nvapiDRSSaveSettingsID         = 0xFCBC7E14
	nvapiDRSLoadSettingsFromFileID = 0xD3EDE889
	nvapiDRSGetBaseProfileID       = 0xDA8466A0
	nvapiDRSFindApplicationID      = 0xEEE566B2
	nvapiDRSGetSettingID           = 0x73BF8338
	nvapiDRSSetSettingID           = 0x577DD202
	nvapiDRSCreateProfileID        = 0xCC176068
	nvapiDRSCreateApplicationID    = 0x4347A9DE
	nvapiDRSFindProfileByNameID    = 0x7E4A9A0B
)

// nvDRSSetting matches NVDRS_SETTING_V1 from nvapi.h (#pragma pack(4)).
type nvDRSSetting struct {
	version             uint32
	settingName         [nvapiUnicodeStringMax]uint16
	settingID           uint32
	settingType         int32
	settingLocation     int32
	isCurrentPredefined uint32
	isPredefinedValid   uint32
	predefined          [nvDRSValueBytes]byte
	current             [nvDRSValueBytes]byte
}

// nvDRSApplication matches NVDRS_APPLICATION_V4. The header pack is 8 and every field is naturally aligned.
type nvDRSApplication struct {
	version          uint32
	isPredefined     uint32
	appName          [nvapiUnicodeStringMax]uint16
	userFriendlyName [nvapiUnicodeStringMax]uint16
	launcher         [nvapiUnicodeStringMax]uint16
	fileInFolder     [nvapiUnicodeStringMax]uint16
	flags            uint32
	commandLine      [nvapiUnicodeStringMax]uint16
}

// nvDRSProfile matches NVDRS_PROFILE_V1. gpuSupport bit 0 is the GeForce flag.
type nvDRSProfile struct {
	version       uint32
	profileName   [nvapiUnicodeStringMax]uint16
	gpuSupport    uint32
	isPredefined  uint32
	numOfApps     uint32
	numOfSettings uint32
}

func nvStructVersion(size uintptr, ver uint32) uint32 {
	return uint32(size) | (ver << 16)
}

var (
	nvDRSSettingVersion     = nvStructVersion(unsafe.Sizeof(nvDRSSetting{}), 1)
	nvDRSApplicationVersion = nvStructVersion(unsafe.Sizeof(nvDRSApplication{}), 4)
	nvDRSProfileVersion     = nvStructVersion(unsafe.Sizeof(nvDRSProfile{}), 1)

	drsMu      sync.Mutex
	nvapiDLL   = syscall.NewLazyDLL("nvapi64.dll")
	nvapiQuery = nvapiDLL.NewProc("nvapi_QueryInterface")
)

type drsProcs struct {
	initialize           uintptr
	unload               uintptr
	createSession        uintptr
	destroySession       uintptr
	loadSettings         uintptr
	loadSettingsFromFile uintptr
	saveSettings         uintptr
	getBaseProfile       uintptr
	findApplication      uintptr
	getSetting           uintptr
	setSetting           uintptr
	createProfile        uintptr
	createApplication    uintptr
	findProfileByName    uintptr
}

func readSmoothMotion(exe string) (smoothMotionSample, error) {
	nvAppEnabled, nvAppHandled, nvAppErr := readNVAppSmoothMotion(exe)
	if sample, handled, err := nvAppSmoothMotionSample(exe, nvAppEnabled, nvAppHandled, nvAppErr); handled {
		return sample, err
	}

	var sample smoothMotionSample
	var gameProfile string
	err := withDRS(func(session uintptr, procs *drsProcs) error {
		profile, found, err := findApplication(session, procs, exe)
		if err != nil {
			return err
		}
		sample.profileFound = found
		if found {
			sample.profile, err = readStoredDword(session, procs, profile)
			if err != nil {
				return err
			}
			gameProfile, err = profileName(session, procs, profile)
			if err != nil {
				return err
			}
		}
		base, err := baseProfile(session, procs)
		if err != nil {
			return err
		}
		sample.global, err = readStoredDword(session, procs, base)
		return err
	})
	if err != nil {
		return smoothMotionSample{}, err
	}
	return mergeFileSmoothMotion(sample, gameProfile)
}

func nvAppSmoothMotionSample(
	exe string,
	enabled, handled bool,
	err error,
) (smoothMotionSample, bool, error) {
	if errors.Is(err, errNVAppApplicationAmbiguous) {
		// A basename shared by multiple NVIDIA App applications is unknown, not a launch
		// blocker. Do not fall back to DRS: NVIDIA App may still be authoritative for the
		// intended application, and DRS could incorrectly report the setting as disabled.
		return smoothMotionSample{}, true, nil
	}
	if err != nil {
		return smoothMotionSample{}, true, fmt.Errorf("read NVIDIA App smooth motion for %s: %w", exe, err)
	}
	if !handled {
		return smoothMotionSample{}, false, nil
	}

	value := uint32(0)
	if enabled {
		value = 1
	}
	return smoothMotionSample{
		profileFound: true,
		profile:      storedDword{present: true, explicit: true, value: value},
	}, true, nil
}

func writeSmoothMotionOff(exe string) error {
	handled, err := writeNVAppSmoothMotionOff(exe)
	if handled {
		if err != nil {
			return fmt.Errorf("disable nvidia smooth motion through NVIDIA App: %w", err)
		}
		return nil
	}
	return writeSmoothMotionOffWith(
		func() (bool, error) {
			return persistSmoothMotionOff(exe)
		},
		func() error {
			return persistUnlistedSmoothMotionOff(exe)
		},
		func() (bool, error) {
			sample, err := readSmoothMotion(exe)
			if err != nil {
				return false, err
			}
			return smoothMotionApplied(sample), nil
		},
	)
}

var errSmoothMotionWriteNotApplied = errors.New("nvidia smooth motion is still enabled after saving")

func writeSmoothMotionOffWith(
	persist func() (bool, error),
	persistUnlisted func() error,
	readApplied func() (bool, error),
) error {
	usedUnlisted, err := persist()
	if err != nil {
		return err
	}
	enabled, err := readApplied()
	if err != nil {
		return fmt.Errorf("verify nvidia smooth motion after saving: %w", err)
	}
	if !enabled {
		return nil
	}
	if usedUnlisted {
		return fmt.Errorf("%w with the imported game profile override", errSmoothMotionWriteNotApplied)
	}

	if err := persistUnlisted(); err != nil {
		return fmt.Errorf("write imported nvidia smooth motion override: %w", err)
	}
	enabled, err = readApplied()
	if err != nil {
		return fmt.Errorf("verify nvidia smooth motion after importing the game profile override: %w", err)
	}
	if enabled {
		return fmt.Errorf("%w with both direct and imported game profile overrides", errSmoothMotionWriteNotApplied)
	}
	return nil
}

func persistSmoothMotionOff(exe string) (bool, error) {
	usedUnlisted := false
	err := withDRS(func(session uintptr, procs *drsProcs) error {
		profile, found, err := findApplication(session, procs, exe)
		if err != nil {
			return err
		}
		if !found {
			profile, err = createGameProfile(session, procs, exe)
			if err != nil {
				return err
			}
		}
		switch status := setSmoothMotionOff(session, procs, profile); status {
		case nvapiOK:
			return saveDRSSettings(procs, session)
		case nvapiSettingNotFound:
			usedUnlisted = true
			// This driver keeps Smooth Motion in the profile database but does not
			// list the setting id, so SetSetting cannot create the game override.
			name, err := profileName(session, procs, profile)
			if err != nil {
				return err
			}
			return writeUnlistedSmoothMotionOff(session, procs, name, !found)
		default:
			return fmt.Errorf("nvidia set smooth motion: status %d", status)
		}
	})
	return usedUnlisted, err
}

func persistUnlistedSmoothMotionOff(exe string) error {
	return withDRS(func(session uintptr, procs *drsProcs) error {
		profile, found, err := findApplication(session, procs, exe)
		if err != nil {
			return err
		}
		if !found {
			profile, err = createGameProfile(session, procs, exe)
			if err != nil {
				return err
			}
		}
		name, err := profileName(session, procs, profile)
		if err != nil {
			return err
		}
		return writeUnlistedSmoothMotionOff(session, procs, name, !found)
	})
}

func withDRS(fn func(uintptr, *drsProcs) error) (err error) {
	drsMu.Lock()
	defer drsMu.Unlock()
	if loadErr := nvapiDLL.Load(); loadErr != nil {
		return errNVIDIAUnavailable
	}
	procs, err := loadDRSProcs()
	if err != nil {
		return err
	}
	switch status := nvCall(procs.initialize); status {
	case nvapiOK:
		defer func() {
			if unloadErr := unloadNVAPI(&procs); unloadErr != nil {
				if err != nil {
					err = errors.Join(err, unloadErr)
					return
				}
				err = unloadErr
			}
		}()
	case nvapiLibraryNotFound, nvapiNVIDIADeviceNotFound:
		return errNVIDIAUnavailable
	default:
		return fmt.Errorf("nvidia initialize: status %d", status)
	}
	var session uintptr
	if status := nvCall(procs.createSession, uintptr(unsafe.Pointer(&session))); status != nvapiOK {
		return fmt.Errorf("nvidia create session: status %d", status)
	}
	if session == 0 {
		return errors.New("nvidia create session returned an empty handle")
	}
	if status := nvCall(procs.loadSettings, session); status != nvapiOK {
		destroyErr := destroyDRS(&procs, session)
		loadErr := fmt.Errorf("nvidia load settings: status %d", status)
		if destroyErr != nil {
			return errors.Join(loadErr, destroyErr)
		}
		return loadErr
	}
	err = fn(session, &procs)
	if destroyErr := destroyDRS(&procs, session); destroyErr != nil {
		if err != nil {
			return errors.Join(err, destroyErr)
		}
		return destroyErr
	}
	return err
}

func unloadNVAPI(procs *drsProcs) error {
	if status := nvCall(procs.unload); status != nvapiOK {
		return fmt.Errorf("nvidia unload: status %d", status)
	}
	return nil
}

func destroyDRS(procs *drsProcs, session uintptr) error {
	if status := nvCall(procs.destroySession, session); status != nvapiOK {
		return fmt.Errorf("nvidia destroy session: status %d", status)
	}
	return nil
}

func loadDRSProcs() (drsProcs, error) {
	var procs drsProcs
	var err error
	procs.initialize, err = nvProc(nvapiInitializeID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.unload, err = nvProc(nvapiUnloadID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.createSession, err = nvProc(nvapiDRSCreateSessionID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.destroySession, err = nvProc(nvapiDRSDestroySessionID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.loadSettings, err = nvProc(nvapiDRSLoadSettingsID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.saveSettings, err = nvProc(nvapiDRSSaveSettingsID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.loadSettingsFromFile, err = nvProc(nvapiDRSLoadSettingsFromFileID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.getBaseProfile, err = nvProc(nvapiDRSGetBaseProfileID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.findApplication, err = nvProc(nvapiDRSFindApplicationID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.getSetting, err = nvProc(nvapiDRSGetSettingID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.setSetting, err = nvProc(nvapiDRSSetSettingID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.createProfile, err = nvProc(nvapiDRSCreateProfileID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.createApplication, err = nvProc(nvapiDRSCreateApplicationID)
	if err != nil {
		return drsProcs{}, err
	}
	procs.findProfileByName, err = nvProc(nvapiDRSFindProfileByNameID)
	if err != nil {
		return drsProcs{}, err
	}
	return procs, nil
}

func nvProc(id uint32) (uintptr, error) {
	address, _, callErr := nvapiQuery.Call(uintptr(id))
	if address == 0 {
		if callErr != nil && !errors.Is(callErr, syscall.Errno(0)) {
			return 0, fmt.Errorf("nvidia query interface 0x%08X: %w", id, callErr)
		}
		return 0, fmt.Errorf("nvidia query interface 0x%08X returned nil", id)
	}
	return address, nil
}

func nvCall(proc uintptr, args ...uintptr) int32 {
	result, _, _ := syscall.SyscallN(proc, args...)
	return int32(result)
}

func profileName(session uintptr, procs *drsProcs, profile uintptr) (string, error) {
	getInfo, err := nvProc(0x61CD6FD6)
	if err != nil {
		return "", err
	}
	info := nvDRSProfile{version: nvDRSProfileVersion}
	status := nvCall(getInfo, session, profile, uintptr(unsafe.Pointer(&info)))
	if status != nvapiOK {
		return "", fmt.Errorf("nvidia get profile info: status %d", status)
	}
	return windows.UTF16ToString(info.profileName[:]), nil
}

func baseProfile(session uintptr, procs *drsProcs) (uintptr, error) {
	var profile uintptr
	status := nvCall(procs.getBaseProfile, session, uintptr(unsafe.Pointer(&profile)))
	if status != nvapiOK {
		return 0, fmt.Errorf("nvidia get base profile: status %d", status)
	}
	if profile == 0 {
		return 0, errors.New("nvidia get base profile returned an empty handle")
	}
	return profile, nil
}

func findApplication(session uintptr, procs *drsProcs, exe string) (uintptr, bool, error) {
	name, err := utf16Fixed(exe)
	if err != nil {
		return 0, false, err
	}
	app := nvDRSApplication{version: nvDRSApplicationVersion}
	var profile uintptr
	status := nvCall(
		procs.findApplication,
		session,
		uintptr(unsafe.Pointer(&name[0])),
		uintptr(unsafe.Pointer(&profile)),
		uintptr(unsafe.Pointer(&app)),
	)
	switch status {
	case nvapiOK:
		if profile == 0 {
			return 0, false, fmt.Errorf("nvidia find application %s returned an empty profile", exe)
		}
		return profile, true, nil
	case nvapiExecutableNotFound, nvapiProfileNotFound:
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("nvidia find application %s: status %d", exe, status)
	}
}

func readStoredDword(session uintptr, procs *drsProcs, profile uintptr) (storedDword, error) {
	setting := nvDRSSetting{version: nvDRSSettingVersion}
	status := nvCall(
		procs.getSetting,
		session,
		profile,
		uintptr(smoothMotionSettingID),
		uintptr(unsafe.Pointer(&setting)),
	)
	if status == nvapiSettingNotFound {
		return storedDword{}, nil
	}
	if status != nvapiOK {
		return storedDword{}, fmt.Errorf("nvidia get smooth motion setting: status %d", status)
	}
	if setting.settingType != nvDRSDWORDType {
		return storedDword{}, fmt.Errorf("nvidia smooth motion setting type %d", setting.settingType)
	}
	return storedDword{
		present:  true,
		explicit: setting.settingLocation == nvDRSCurrentProfileLocation,
		value:    binary.LittleEndian.Uint32(setting.current[:4]),
	}, nil
}

func setSmoothMotionOff(session uintptr, procs *drsProcs, profile uintptr) int32 {
	setting := nvDRSSetting{
		version:         nvDRSSettingVersion,
		settingID:       smoothMotionSettingID,
		settingType:     nvDRSDWORDType,
		settingLocation: nvDRSCurrentProfileLocation,
	}
	return nvCall(procs.setSetting, session, profile, uintptr(unsafe.Pointer(&setting)))
}

func saveDRSSettings(procs *drsProcs, session uintptr) error {
	if status := nvCall(procs.saveSettings, session); status != nvapiOK {
		return fmt.Errorf("nvidia save settings: status %d", status)
	}
	return nil
}

func loadDRSSettingsFromFile(procs *drsProcs, session uintptr, path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if status := nvCall(procs.loadSettingsFromFile, session, uintptr(unsafe.Pointer(file))); status != nvapiOK {
		return fmt.Errorf("nvidia load settings file: status %d", status)
	}
	return nil
}

func writeUnlistedSmoothMotionOff(
	session uintptr,
	procs *drsProcs,
	profileName string,
	created bool,
) (err error) {
	if created {
		if err = saveDRSSettings(procs, session); err != nil {
			return err
		}
	}
	path, pathErr := activeDRSDatabasePath()
	if pathErr != nil {
		return pathErr
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read nvidia driver profile database: %w", err)
	}
	patched, err := setProfileDword(data, profileName, smoothMotionSettingID, 0)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "nahida-nvdrs-*.bin")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		removeErr := os.Remove(tmpPath)
		if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
			return
		}
		err = errors.Join(err, fmt.Errorf("remove nvidia profile patch: %w", removeErr))
	}()
	if _, err := tmp.Write(patched); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := loadDRSSettingsFromFile(procs, session, tmpPath); err != nil {
		return err
	}
	return saveDRSSettings(procs, session)
}

func createGameProfile(session uintptr, procs *drsProcs, exe string) (uintptr, error) {
	name, err := utf16Fixed(exe)
	if err != nil {
		return 0, err
	}
	info := nvDRSProfile{
		version:     nvDRSProfileVersion,
		profileName: name,
		gpuSupport:  1,
	}
	var profile uintptr
	status := nvCall(
		procs.createProfile,
		session,
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Pointer(&profile)),
	)
	if status == nvapiProfileNameInUse {
		status = nvCall(
			procs.findProfileByName,
			session,
			uintptr(unsafe.Pointer(&name[0])),
			uintptr(unsafe.Pointer(&profile)),
		)
	}
	if status != nvapiOK {
		return 0, fmt.Errorf("nvidia create profile for %s: status %d", exe, status)
	}
	if profile == 0 {
		return 0, fmt.Errorf("nvidia create profile for %s returned an empty handle", exe)
	}
	app := nvDRSApplication{
		version:          nvDRSApplicationVersion,
		appName:          name,
		userFriendlyName: name,
	}
	status = nvCall(procs.createApplication, session, profile, uintptr(unsafe.Pointer(&app)))
	if status != nvapiOK {
		return 0, fmt.Errorf("nvidia create application %s: status %d", exe, status)
	}
	return profile, nil
}

func utf16Fixed(value string) ([nvapiUnicodeStringMax]uint16, error) {
	encoded, err := windows.UTF16FromString(value)
	if err != nil {
		return [nvapiUnicodeStringMax]uint16{}, err
	}
	if len(encoded) > nvapiUnicodeStringMax {
		return [nvapiUnicodeStringMax]uint16{}, errors.New("nvidia name is too long")
	}
	var out [nvapiUnicodeStringMax]uint16
	copy(out[:], encoded)
	return out, nil
}
