package xxmi

import (
	"context"
	"errors"
	"fmt"
)

const xxmiSmoothMotionWhere = "XXMI.smoothMotion"

// errNVIDIAUnavailable means nvapi64.dll is missing or no NVIDIA GPU is driving a display.
// Smooth Motion cannot be on in that case.
var errNVIDIAUnavailable = errors.New("nvidia driver settings are unavailable")

// smoothMotionSettingID is Smooth Motion - Enable (FGX) in the NVIDIA driver profile database.
const smoothMotionSettingID = 0xB0D384C0

type storedDword struct {
	present  bool
	explicit bool
	value    uint32
}

// smoothMotionSample is the driver state for one executable.
// An explicit game-profile value wins. Otherwise the base profile applies.
// A missing value on both profiles is off. NVIDIA App's FGX backend is the
// authoritative path when available; the active nvdrsdb file supplies the
// hidden setting when only NvAPI is available.
type smoothMotionSample struct {
	profileFound bool
	profile      storedDword
	global       storedDword
}

func smoothMotionApplied(sample smoothMotionSample) bool {
	if sample.profileFound && sample.profile.present && sample.profile.explicit {
		return sample.profile.value != 0
	}
	if sample.global.present {
		return sample.global.value != 0
	}
	return false
}

func (x *XXMI) smoothMotionEnabled(ctx context.Context, exe string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	sample, err := readSmoothMotion(exe)
	if errors.Is(err, errNVIDIAUnavailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return smoothMotionApplied(sample), nil
}

func (x *XXMI) disableSmoothMotion(ctx context.Context, exe string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := writeSmoothMotionOff(exe)
	if errors.Is(err, errNVIDIAUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Disabled NVIDIA Smooth Motion for %s", exe), xxmiSmoothMotionWhere)
	}
	return nil
}
