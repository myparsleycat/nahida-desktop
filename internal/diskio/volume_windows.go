package diskio

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

const (
	ioctlStorageQueryProperty        = 0x002D1400
	storageDeviceSeekPenaltyProperty = 7
)

// storagePropertyQuery is STORAGE_PROPERTY_QUERY asking for PropertyStandardQuery.
type storagePropertyQuery struct {
	propertyID           uint32
	queryType            uint32
	additionalParameters [1]byte
}

// deviceSeekPenaltyDescriptor is DEVICE_SEEK_PENALTY_DESCRIPTOR.
type deviceSeekPenaltyDescriptor struct {
	version           uint32
	size              uint32
	incursSeekPenalty bool
}

// resolveVolume returns the upper-case volume name the directory resolves to.
// A directory that does not exist yet belongs to the volume of its nearest
// existing ancestor; one that cannot be resolved at all keeps its lexical volume.
func resolveVolume(dir string) string {
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	for current := dir; ; {
		if final, err := platform.FinalPath(current); err == nil {
			return volumeName(final)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return volumeName(dir)
}

// volumeName drops the extended-length prefix Windows puts on a resolved path,
// so that `\\?\C:\Mods` and `C:\Mods` name the same volume.
func volumeName(path string) string {
	if rest, ok := strings.CutPrefix(path, `\\?\UNC\`); ok {
		path = `\\` + rest
	} else {
		path = strings.TrimPrefix(path, `\\?\`)
	}
	return strings.ToUpper(filepath.VolumeName(path))
}

// incursSeekPenalty reports whether a drive-letter volume sits on a rotational
// disk. Network shares and volumes without a drive letter are not limited. The
// query needs no access rights on the volume, so it works without elevation.
func incursSeekPenalty(volume string) (bool, error) {
	if len(volume) != 2 || volume[1] != ':' {
		return false, nil
	}
	root, err := windows.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return false, err
	}
	if kind := windows.GetDriveType(root); kind != windows.DRIVE_FIXED && kind != windows.DRIVE_REMOVABLE {
		return false, nil
	}

	device, err := windows.UTF16PtrFromString(`\\.\` + volume)
	if err != nil {
		return false, err
	}
	handle, err := windows.CreateFile(
		device, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return false, fmt.Errorf("open volume %s: %w", volume, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	query := storagePropertyQuery{propertyID: storageDeviceSeekPenaltyProperty}
	var descriptor deviceSeekPenaltyDescriptor
	var returned uint32
	if err := windows.DeviceIoControl(
		handle, ioctlStorageQueryProperty,
		(*byte)(unsafe.Pointer(&query)), uint32(unsafe.Sizeof(query)),
		(*byte)(unsafe.Pointer(&descriptor)), uint32(unsafe.Sizeof(descriptor)),
		&returned, nil,
	); err != nil {
		return false, fmt.Errorf("query seek penalty of volume %s: %w", volume, err)
	}
	return descriptor.incursSeekPenalty, nil
}
