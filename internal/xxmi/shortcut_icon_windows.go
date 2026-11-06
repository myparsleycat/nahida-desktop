//go:build windows

package xxmi

import (
	"bytes"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"nahida.live/desktop/internal/platform"
)

// shortcutIcons holds one 256x256 PNG per importer, named after the importer key.
//
//go:embed shortcut_icons/*.png
var shortcutIcons embed.FS

const (
	shortcutIconDir = "shortcut-icons"
	// shortcutCustomIconDir holds the converted PNG of each custom icon, which the settings page previews.
	shortcutCustomIconDir = "custom"
	// iconHeaderSize is the icon directory header followed by its single entry.
	iconHeaderSize = 6 + 16
	// shortcutIconPixels is the side of the only image an icon file carries.
	shortcutIconPixels = 256
	// shortcutIconSourceLimit and shortcutIconPixelLimit are checked before decoding, which allocates up to
	// eight bytes per pixel for 16-bit images.
	shortcutIconSourceLimit = 32 << 20
	shortcutIconPixelLimit  = 16 << 20
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

// convertShortcutIcon decodes a PNG, JPEG, or WebP image and fits it, centered and with its aspect ratio kept,
// on a transparent 256x256 canvas encoded as PNG.
func convertShortcutIcon(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > shortcutIconSourceLimit {
		return nil, errors.New("XXMI_SHORTCUT_ICON_INVALID: not a regular file or too large")
	}
	data, err := io.ReadAll(io.LimitReader(file, shortcutIconSourceLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > shortcutIconSourceLimit {
		return nil, errors.New("XXMI_SHORTCUT_ICON_INVALID: not a regular file or too large")
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: %w", err)
	}
	if format != "png" && format != "jpeg" && format != "webp" {
		return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: unsupported format %s", format)
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > shortcutIconPixelLimit {
		return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: image is %dx%d", config.Width, config.Height)
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: %w", err)
	}
	orientation := exifOrientation(exifPayload(format, data))

	// Orientations 5 to 8 turn the stored image a quarter, so it is displayed with its sides swapped.
	bounds := source.Bounds()
	shownWidth, shownHeight := bounds.Dx(), bounds.Dy()
	if orientation >= 5 {
		shownWidth, shownHeight = shownHeight, shownWidth
	}
	width, height := shortcutIconPixels, shortcutIconPixels
	if shownWidth > shownHeight {
		height = max(1, shownHeight*shortcutIconPixels/shownWidth)
	} else {
		width = max(1, shownWidth*shortcutIconPixels/shownHeight)
	}

	// Scaling first keeps the reorientation loop to at most 256x256 pixels.
	scaled := image.NewNRGBA(image.Rect(0, 0, width, height))
	if orientation >= 5 {
		scaled = image.NewNRGBA(image.Rect(0, 0, height, width))
	}
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), source, bounds, draw.Src, nil)
	left, top := (shortcutIconPixels-width)/2, (shortcutIconPixels-height)/2
	canvas := image.NewNRGBA(image.Rect(0, 0, shortcutIconPixels, shortcutIconPixels))
	for y := range height {
		for x := range width {
			storedX, storedY := orientedSource(orientation, x, y, width, height)
			from, to := scaled.PixOffset(storedX, storedY), canvas.PixOffset(left+x, top+y)
			copy(canvas.Pix[to:to+4], scaled.Pix[from:from+4])
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

// orientedSource maps a pixel of the displayed width x height image to the stored pixel that EXIF orientation
// puts there.
func orientedSource(orientation, x, y, width, height int) (int, int) {
	switch orientation {
	case 2:
		return width - 1 - x, y
	case 3:
		return width - 1 - x, height - 1 - y
	case 4:
		return x, height - 1 - y
	case 5:
		return y, x
	case 6:
		return y, width - 1 - x
	case 7:
		return height - 1 - y, width - 1 - x
	case 8:
		return height - 1 - y, x
	default:
		return x, y
	}
}

// exifPayload returns the TIFF-structured EXIF data of a JPEG APP1 segment, a PNG eXIf chunk, or a WebP EXIF
// chunk, or nil when the image carries none.
func exifPayload(format string, data []byte) []byte {
	exifHeader := []byte("Exif\x00\x00")
	switch format {
	case "jpeg":
		for offset := 2; offset+4 <= len(data) && data[offset] == 0xff; {
			marker := data[offset+1]
			if marker == 0xff {
				// A marker may be preceded by any number of 0xFF fill bytes.
				offset++
				continue
			}
			if marker == 0xda || marker == 0xd9 {
				return nil
			}
			end := offset + 2 + int(binary.BigEndian.Uint16(data[offset+2:]))
			if end > len(data) || end < offset+4 {
				return nil
			}
			segment := data[offset+4 : end]
			offset = end
			if marker == 0xe1 && bytes.HasPrefix(segment, exifHeader) {
				return segment[len(exifHeader):]
			}
		}
	case "png":
		for offset := 8; offset+12 <= len(data); {
			start := offset + 8
			end := start + int(binary.BigEndian.Uint32(data[offset:]))
			if end+4 > len(data) || end < start {
				return nil
			}
			switch string(data[offset+4 : start]) {
			case "eXIf":
				return data[start:end]
			case "IEND":
				return nil
			}
			offset = end + 4
		}
	case "webp":
		for offset := 12; offset+8 <= len(data); {
			start := offset + 8
			size := int(binary.LittleEndian.Uint32(data[offset+4:]))
			end := start + size
			if end > len(data) || end < start {
				return nil
			}
			if string(data[offset:offset+4]) == "EXIF" {
				// Some writers keep the JPEG APP1 header in front of the TIFF data.
				return bytes.TrimPrefix(data[start:end], exifHeader)
			}
			offset = end + size&1
		}
	}
	return nil
}

// exifOrientation reads the orientation tag from TIFF-structured EXIF data and returns 1 when it is absent or
// malformed.
func exifOrientation(tiff []byte) int {
	const orientationTag = 0x0112
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	directory := int(order.Uint32(tiff[4:]))
	if directory < 8 || directory+2 > len(tiff) {
		return 1
	}
	count := int(order.Uint16(tiff[directory:]))
	for entry := directory + 2; entry+12 <= len(tiff) && count > 0; entry, count = entry+12, count-1 {
		if order.Uint16(tiff[entry:]) == orientationTag {
			if value := int(order.Uint16(tiff[entry+8:])); value >= 1 && value <= 8 {
				return value
			}
			return 1
		}
	}
	return 1
}

func customShortcutIconPNG(dir, key, id string) string {
	return filepath.Join(dir, shortcutCustomIconDir, key+"-"+id+".png")
}

func customShortcutIconFile(dir, key, id string) string {
	return filepath.Join(dir, key+"-"+id+".ico")
}

// readCustomShortcutIcon returns the converted PNG of a custom icon after checking it against its ID.
func readCustomShortcutIcon(dir, key, id string) ([]byte, error) {
	if !isHashID(id) {
		return nil, fmt.Errorf("invalid shortcut icon %q", id)
	}
	data, err := os.ReadFile(customShortcutIconPNG(dir, key, id))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(hashBytes(data), id) {
		return nil, errors.New("shortcut icon hash mismatch")
	}
	return data, nil
}

// pruneShortcutIcons removes the importer's custom icon files other than keep, which may be empty, and temporary
// files of interrupted writes. The caller must hold shortcutMu, which every write to these folders holds.
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
			stale := (ok && isHashID(id) && id != keep) || strings.HasPrefix(name, atomicWriteTempPrefix)
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
