//go:build windows

package xxmi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// createJunction creates link as a directory junction to target. Junctions need no elevation, unlike symbolic
// links, and 3DMigoto follows them like ordinary folders.
func createJunction(link, target string) (returnErr error) {
	target, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if len(filepath.VolumeName(target)) != 2 || strings.HasPrefix(target, `\\`) {
		return fmt.Errorf("junction target must be on a local drive: %q", target)
	}

	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, os.Remove(link))
		}
	}()
	path, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		path, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0,
	)
	if err != nil {
		return fmt.Errorf("open junction %q: %w", link, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	// REPARSE_DATA_BUFFER for IO_REPARSE_TAG_MOUNT_POINT: an 8-byte header, four name offsets and lengths, then
	// the NT substitute name and the display name, each NUL-terminated.
	substitute := utf16.Encode([]rune(`\??\` + target))
	display := utf16.Encode([]rune(target))
	names := append(append(append(substitute, 0), display...), 0)
	buffer := make([]byte, 16+len(names)*2)
	binary.LittleEndian.PutUint32(buffer[0:], windows.IO_REPARSE_TAG_MOUNT_POINT)
	binary.LittleEndian.PutUint16(buffer[4:], uint16(8+len(names)*2))
	binary.LittleEndian.PutUint16(buffer[10:], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(buffer[12:], uint16((len(substitute)+1)*2))
	binary.LittleEndian.PutUint16(buffer[14:], uint16(len(display)*2))
	for i, unit := range names {
		binary.LittleEndian.PutUint16(buffer[16+i*2:], unit)
	}
	if len(buffer) > windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE {
		return fmt.Errorf("junction target path is too long: %q", target)
	}

	var returned uint32
	if err := windows.DeviceIoControl(
		handle, windows.FSCTL_SET_REPARSE_POINT, &buffer[0], uint32(len(buffer)), nil, 0, &returned, nil,
	); err != nil {
		return fmt.Errorf("create junction %q -> %q: %w", link, target, err)
	}
	return nil
}
