//go:build windows

package xxmi

import (
	"bytes"
	"embed"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/platform"
)

// shortcutIcons holds one 256x256 PNG per importer, named after the importer key.
//
//go:embed shortcut_icons/*.png
var shortcutIcons embed.FS

const (
	shortcutIconDir = "shortcut-icons"
	// iconHeaderSize is the icon directory header followed by its single entry.
	iconHeaderSize = 6 + 16
)

// writeShortcutIcon stores the importer's icon as an .ico file in dir and returns its path.
// A shortcut refers to its icon by path, so the file has to stay there after the call.
func writeShortcutIcon(dir, key string) (string, error) {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return "", fmt.Errorf("unknown importer %q", key)
	}
	image, err := shortcutIcons.ReadFile("shortcut_icons/" + spec.key + ".png")
	if err != nil {
		return "", fmt.Errorf("read %s shortcut icon: %w", spec.key, err)
	}

	// An icon file can carry a 256x256 image as PNG; its width and height bytes stay zero for 256.
	icon := make([]byte, iconHeaderSize, iconHeaderSize+len(image))
	binary.LittleEndian.PutUint16(icon[2:], 1)
	binary.LittleEndian.PutUint16(icon[4:], 1)
	binary.LittleEndian.PutUint16(icon[10:], 1)
	binary.LittleEndian.PutUint16(icon[12:], 32)
	binary.LittleEndian.PutUint32(icon[14:], uint32(len(image)))
	binary.LittleEndian.PutUint32(icon[18:], iconHeaderSize)
	icon = append(icon, image...)

	path := filepath.Join(dir, spec.key+".ico")
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, icon) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(dir, spec.key+"-*.tmp")
	if err != nil {
		return "", err
	}
	_, err = temp.Write(icon)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = platform.ReplaceAtomic(temp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(temp.Name())
		return "", fmt.Errorf("write %s shortcut icon: %w", spec.key, err)
	}
	return path, nil
}
