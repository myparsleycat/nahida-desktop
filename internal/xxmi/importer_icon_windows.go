//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gen2brain/avif"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"nahida.live/desktop/internal/infra"
)

// ImporterIcon is the icon the XXMI pages and the Quick Start shortcut show for an importer.
type ImporterIcon struct {
	Key    string `json:"key"`
	Custom bool   `json:"custom"`
	// Path is the converted 256x256 PNG of a custom icon; it is empty for the importer's own icon.
	Path string `json:"path"`
	// Unavailable reports a custom icon whose converted image is missing or damaged. The importer's own icon is
	// shown instead until the selection is changed or reset.
	Unavailable bool `json:"unavailable"`
}

type ImporterIconChange struct {
	// ShortcutUpdated reports that an existing desktop shortcut was rewritten with the new icon.
	ShortcutUpdated bool `json:"shortcutUpdated"`
}

// GetImporterIcons returns the icon of every importer, sorted by key.
func (x *XXMI) GetImporterIcons(ctx context.Context) ([]ImporterIcon, error) {
	dir, err := x.shortcutIconRoot()
	if err != nil {
		return nil, err
	}

	// The lock keeps a selection and its preview together while changeImporterIcon replaces and prunes them.
	x.shortcutMu.Lock()
	defer x.shortcutMu.Unlock()
	icons := make([]ImporterIcon, 0, len(importerPackages))
	var unavailable []error
	for _, key := range slices.Sorted(maps.Keys(importerPackages)) {
		id, err := x.importerIconID(ctx, key)
		if err != nil {
			return nil, err
		}
		icon := ImporterIcon{Key: key, Custom: id != ""}
		if icon.Custom {
			if _, err := readCustomImporterIcon(dir, key, id); err != nil {
				unavailable = append(unavailable, fmt.Errorf("%s custom icon %s: %w", key, id, err))
				icon.Unavailable = true
			} else {
				icon.Path = customImporterIconPNG(dir, key, id)
			}
		}
		icons = append(icons, icon)
	}
	x.iconDiagnostic.Report(x.log, errors.Join(unavailable...), "XXMI.GetImporterIcons", infra.Diagnostic{
		Operation: "read-importer-icons", Stage: "read-custom-icon", Fields: map[string]any{"icon_dir": dir},
	})
	return icons, nil
}

// SetImporterIcon converts a PNG, JPEG, WebP, or AVIF image into the importer's icon and applies it to the desktop
// shortcut when one exists.
func (x *XXMI) SetImporterIcon(ctx context.Context, key, imagePath string) (ImporterIconChange, error) {
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return ImporterIconChange{}, errors.New("XXMI_SHORTCUT_ICON_INVALID: no image path")
	}
	return x.changeImporterIcon(ctx, key, imagePath, "XXMI.SetImporterIcon")
}

// ResetImporterIcon returns the importer and its Quick Start shortcut to the importer's own icon.
func (x *XXMI) ResetImporterIcon(ctx context.Context, key string) (ImporterIconChange, error) {
	return x.changeImporterIcon(ctx, key, "", "XXMI.ResetImporterIcon")
}

// changeImporterIcon stores the icon converted from imagePath, or forgets the custom icon when imagePath is empty.
func (x *XXMI) changeImporterIcon(
	ctx context.Context,
	key, imagePath, where string,
) (change ImporterIconChange, returnErr error) {
	stage := "resolve"
	id, icon, shortcut, rollback := "", "", "", ""
	// created lists the custom icon files this call added; they are removed again on failure unless the shortcut
	// was left pointing at the new icon.
	var created []string
	iconInUse := false
	defer func() {
		if returnErr != nil && !iconInUse {
			for _, file := range created {
				if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
					rollback = strings.TrimPrefix(rollback+"; remove "+file+": "+err.Error(), "; ")
				}
			}
		}
		if infra.IsCancellationError(returnErr) {
			return
		}
		returnErr = infra.ReportError(x.log, returnErr, where, infra.Diagnostic{
			Operation: "change-importer-icon", Stage: stage,
			Fields: map[string]any{
				"importer": key, "source_path": installDiagnosticValue(imagePath), "icon_id": id, "icon_path": icon,
				"shortcut_path": shortcut, "rollback": rollback,
			},
		})
	}()
	spec, ok := lookupImporterPackage(key)
	if !ok {
		return ImporterIconChange{}, fmt.Errorf("unknown importer %q", key)
	}
	client, err := x.settingsClient()
	if err != nil {
		return ImporterIconChange{}, err
	}
	dir, err := x.shortcutIconRoot()
	if err != nil {
		return ImporterIconChange{}, err
	}

	var data []byte
	if imagePath != "" {
		stage = "convert"
		if data, err = convertImporterIcon(imagePath); err != nil {
			return ImporterIconChange{}, err
		}
		id = hashBytes(data)[:12]
	}

	x.shortcutMu.Lock()
	defer x.shortcutMu.Unlock()
	stage = "store"
	if id == "" {
		icon, err = writeShortcutIcon(dir, spec.key)
	} else {
		preview := customImporterIconPNG(dir, spec.key, id)
		icon = customShortcutIconFile(dir, spec.key, id)
		for _, file := range []string{preview, icon} {
			if _, statErr := os.Lstat(file); errors.Is(statErr, os.ErrNotExist) {
				created = append(created, file)
			}
		}
		if err = writeShortcutFile(preview, data); err == nil {
			err = writeIconFile(icon, data)
		}
	}
	if err != nil {
		return ImporterIconChange{}, err
	}

	stage = "update-shortcut"
	err = withShellCOM(func() error {
		path, pathErr := desktopShortcutPath(spec.key)
		if pathErr != nil {
			return pathErr
		}
		shortcut = path
		updated, updateErr := updateExistingShortcut(ctx, path, icon)
		change.ShortcutUpdated = updated
		return updateErr
	})
	if err != nil {
		return ImporterIconChange{}, err
	}
	iconInUse = change.ShortcutUpdated

	// The shortcut and the stored selection change together: a selection that cannot be saved puts the shortcut
	// back on the icon the selection still names.
	stage = "save-setting"
	settingKey := importerIconSettingKey(spec.key)
	if id == "" {
		err = client.Settings.Delete(ctx, settingKey)
	} else {
		err = client.Settings.Upsert(ctx, settingKey, &id)
	}
	if err != nil {
		if change.ShortcutUpdated {
			restoreCtx := context.WithoutCancel(ctx)
			previous, restoreErr := x.resolveShortcutIcon(restoreCtx, dir, spec.key)
			if restoreErr == nil {
				_, restoreErr = updateExistingShortcut(restoreCtx, shortcut, previous)
			}
			rollback = "restored"
			iconInUse = false
			if restoreErr != nil {
				rollback = "failed: " + restoreErr.Error()
				iconInUse = true
			}
		}
		return ImporterIconChange{}, err
	}

	x.reportCleanup(pruneShortcutIcons(dir, spec.key, id), "prune-shortcut-icons")
	x.log.Info(map[string]any{
		"importer": spec.key, "icon_id": id, "shortcut_updated": change.ShortcutUpdated,
	}, where)
	return change, nil
}

func (x *XXMI) importerIconID(ctx context.Context, key string) (string, error) {
	client, err := x.settingsClient()
	if err != nil {
		return "", err
	}
	value, err := client.Settings.GetValue(ctx, importerIconSettingKey(key))
	if err != nil || value == nil {
		return "", err
	}
	return strings.TrimSpace(*value), nil
}

// convertImporterIcon decodes a PNG, JPEG, WebP, or AVIF image and fits it, centered and with its aspect ratio kept,
// on a transparent 256x256 canvas encoded as PNG.
func convertImporterIcon(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > importerIconSourceLimit {
		return nil, errors.New("XXMI_SHORTCUT_ICON_INVALID: not a regular file or too large")
	}
	data, err := io.ReadAll(io.LimitReader(file, importerIconSourceLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > importerIconSourceLimit {
		return nil, errors.New("XXMI_SHORTCUT_ICON_INVALID: not a regular file or too large")
	}

	format := "avif"
	var config image.Config
	if len(data) >= 8 && string(data[4:8]) == "ftyp" {
		if err := validateStillAVIF(data); err != nil {
			return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: %w", err)
		}
		config, err = recoverAVIF(func() (image.Config, error) { return avif.DecodeConfig(bytes.NewReader(data)) })
	} else {
		config, format, err = image.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil {
		return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: %w", err)
	}
	if format != "png" && format != "jpeg" && format != "webp" && format != "avif" {
		return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: unsupported format %s", format)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > importerIconPixelLimit/config.Height {
		return nil, fmt.Errorf("XXMI_SHORTCUT_ICON_INVALID: image is %dx%d", config.Width, config.Height)
	}
	var source image.Image
	if format == "avif" {
		// AVIF orientation is carried by irot and imir boxes, not EXIF, so the decoder applies it.
		source, err = recoverAVIF(func() (image.Image, error) {
			return avif.Decode(bytes.NewReader(data), avif.Options{AutoRotate: true})
		})
	} else {
		source, _, err = image.Decode(bytes.NewReader(data))
	}
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

// DecodeConfig can read the primary item's dimensions while Decode chooses a track in moov. Reject tracks
// before either decoder call so the primary item is the only possible source of pixels.
func validateStillAVIF(data []byte) error {
	for offset := 0; offset < len(data); {
		box := data[offset:]
		if len(box) < 8 {
			return errors.New("truncated AVIF box header")
		}
		header := 8
		size := uint64(binary.BigEndian.Uint32(box))
		switch size {
		case 0:
			size = uint64(len(box))
		case 1:
			header = 16
			if len(box) < header {
				return errors.New("truncated AVIF box header")
			}
			size = binary.BigEndian.Uint64(box[8:])
		}
		if size < uint64(header) || size > uint64(len(box)) {
			return errors.New("invalid AVIF box size")
		}
		if offset == 0 {
			brands := box[header:int(size)]
			if len(brands) < 8 || len(brands)%4 != 0 {
				return errors.New("invalid AVIF file type")
			}
			// Files with a major brand of mif1 can list avif only among the compatible brands.
			hasAVIF := false
			for i := 0; i < len(brands); i += 4 {
				if brand := string(brands[i : i+4]); i != 4 && (brand == "avif" || brand == "avis") {
					hasAVIF = true
					break
				}
			}
			if !hasAVIF {
				return errors.New("unsupported image format")
			}
		}
		if string(box[4:8]) == "moov" {
			return errors.New("AVIF sequences are not supported for importer icons")
		}
		offset += int(size)
	}
	return nil
}

// recoverAVIF returns a panic of the AVIF decoder as an error. Its box parser indexes into boxes without checking
// their length, and an unrecovered panic in a service call closes the application.
func recoverAVIF[T any](decode func() (T, error)) (result T, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("malformed AVIF: %v", recovered)
		}
	}()
	return decode()
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
func customImporterIconPNG(dir, key, id string) string {
	return filepath.Join(dir, shortcutCustomIconDir, key+"-"+id+".png")
}

// readCustomImporterIcon returns the converted PNG of a custom icon after checking it against its ID.
func readCustomImporterIcon(dir, key, id string) ([]byte, error) {
	if !isHashID(id) {
		return nil, fmt.Errorf("invalid importer icon %q", id)
	}
	data, err := os.ReadFile(customImporterIconPNG(dir, key, id))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(hashBytes(data), id) {
		return nil, errors.New("importer icon hash mismatch")
	}
	return data, nil
}
