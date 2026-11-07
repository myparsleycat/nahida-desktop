//go:build windows

package xxmi

import (
	"bytes"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/platform"
)

// shortcutIcons holds one 256x256 PNG per importer, named after the importer key.
//
//go:embed shortcut_icons/*.png
var shortcutIcons embed.FS

const (
	// shortcutIconDir also holds custom importer icons. Existing shortcuts refer to the .ico files in it by path,
	// so it keeps the name it had when custom icons applied only to the shortcut.
	shortcutIconDir = "shortcut-icons"
	// shortcutCustomIconDir holds the converted PNG of each custom icon, which the XXMI pages show.
	shortcutCustomIconDir = "custom"
	// iconHeaderSize is the icon directory header followed by its single entry.
	iconHeaderSize = 6 + 16
	// shortcutIconPixels is the side of the only image an icon file carries.
	shortcutIconPixels = 256
	// importerIconSourceLimit and importerIconPixelLimit are checked before decoding, which allocates up to
	// eight bytes per pixel for 16-bit images.
	importerIconSourceLimit = 32 << 20
	importerIconPixelLimit  = 16 << 20
	// atomicWriteTempPrefix names the temporary files platform.WriteFileAtomic leaves behind when interrupted.
	atomicWriteTempPrefix = ".nhd-write-"
)

// writeShortcutIcon stores the importer's icon as an .ico file in dir and returns its path.
// A shortcut refers to its icon by path, so the file has to stay there after the call.
func writeShortcutIcon(dir, key string) (string, error) {
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return "", fmt.Errorf("unknown importer %q", key)
	}
	data, err := shortcutIcons.ReadFile("shortcut_icons/" + spec.key + ".png")
	if err != nil {
		return "", fmt.Errorf("read %s shortcut icon: %w", spec.key, err)
	}
	path := filepath.Join(dir, spec.key+".ico")
	if err := writeIconFile(path, data); err != nil {
		return "", fmt.Errorf("write %s shortcut icon: %w", spec.key, err)
	}
	return path, nil
}

// writeIconFile wraps a 256x256 PNG in an icon file at path.
func writeIconFile(path string, data []byte) error {
	// An icon file can carry a 256x256 image as PNG; its width and height bytes stay zero for 256.
	icon := make([]byte, iconHeaderSize, iconHeaderSize+len(data))
	binary.LittleEndian.PutUint16(icon[2:], 1)
	binary.LittleEndian.PutUint16(icon[4:], 1)
	binary.LittleEndian.PutUint16(icon[10:], 1)
	binary.LittleEndian.PutUint16(icon[12:], 32)
	binary.LittleEndian.PutUint32(icon[14:], uint32(len(data)))
	binary.LittleEndian.PutUint32(icon[18:], iconHeaderSize)
	icon = append(icon, data...)
	return writeShortcutFile(path, icon)
}

// writeShortcutFile replaces path with data unless it already holds exactly that.
func writeShortcutFile(path string, data []byte) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return platform.WriteFileAtomic(path, data, 0o600)
}

func customShortcutIconFile(dir, key, id string) string {
	return filepath.Join(dir, key+"-"+id+".ico")
}

// pruneShortcutIcons removes the importer's custom icon previews other than keep, which may be empty, and temporary
// files of interrupted writes. The .ico files stay: a shortcut copied or pinned elsewhere, which cannot be found,
// may still show one. The caller must hold shortcutMu, which every write to these folders holds.
func pruneShortcutIcons(dir, key, keep string) error {
	var errs []error
	for _, folder := range []string{dir, filepath.Join(dir, shortcutCustomIconDir)} {
		entries, err := os.ReadDir(folder)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			id, ok := strings.CutPrefix(strings.TrimSuffix(name, filepath.Ext(name)), key+"-")
			preview := ok && isHashID(id) && id != keep && strings.EqualFold(filepath.Ext(name), ".png")
			stale := preview || strings.HasPrefix(name, atomicWriteTempPrefix)
			if entry.IsDir() || !stale {
				continue
			}
			if err := os.Remove(filepath.Join(folder, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
