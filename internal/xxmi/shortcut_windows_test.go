//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/gen2brain/avif"
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
	"github.com/rodrigocfd/windigo/x/cosh"
	"github.com/rodrigocfd/windigo/x/winsh"
	"golang.org/x/sys/windows"
)

func TestCreateWindowsShortcut(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("original path", func(t *testing.T) {
		testCreateWindowsShortcut(t, executable)
	})
	t.Run("uppercase path", func(t *testing.T) {
		testCreateWindowsShortcut(t, strings.ToUpper(executable))
	})
	t.Run("short path", func(t *testing.T) {
		longPath, err := windows.UTF16PtrFromString(executable)
		if err != nil {
			t.Fatal(err)
		}
		buffer := make([]uint16, len(executable)+1)
		length, err := windows.GetShortPathName(longPath, &buffer[0], uint32(len(buffer)))
		if err != nil || length >= uint32(len(buffer)) {
			t.Fatalf("short executable path length = %d, error = %v", length, err)
		}
		shortPath := windows.UTF16ToString(buffer)
		if strings.EqualFold(shortPath, executable) {
			t.Skip("executable has no distinct 8.3 path on this filesystem")
		}
		testCreateWindowsShortcut(t, shortPath)
	})
}

func testCreateWindowsShortcut(t *testing.T, executable string) {
	t.Helper()
	executableInfo, err := os.Stat(executable)
	if err != nil {
		t.Fatal(err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE); err != nil {
		t.Fatal(err)
	}
	defer win.CoUninitialize()
	iconPath, err := writeShortcutIcon(t.TempDir(), "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	wantIconInfo, err := os.Stat(iconPath)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "GIMI Quick Start.lnk")
	err = createWindowsShortcut(context.Background(), path, executable, "--xxmi-launch GIMI", iconPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("shortcut = %v, %v", info, err)
	}
	releaser := win.NewOleReleaser()
	defer releaser.Release()
	link := loadShortcutLink(t, releaser, path)
	target, err := link.GetPath(nil, cosh.SLGP_RAWPATH)
	if err != nil || !filepath.IsAbs(target) {
		t.Fatalf("shortcut target = %q, want executable = %q, error = %v", target, executable, err)
	}

	// Shell links can expand 8.3 names and normalize casing; compare file identity.
	targetInfo, err := os.Stat(target)
	if err != nil || !os.SameFile(targetInfo, executableInfo) {
		t.Fatalf("shortcut target = %q, want executable = %q, error = %v", target, executable, err)
	}
	args, err := link.GetArguments()
	if err != nil || args != "--xxmi-launch GIMI" {
		t.Fatalf("shortcut arguments = %q, error = %v", args, err)
	}
	icon, index, err := link.GetIconLocation()
	if err != nil || index != 0 {
		t.Fatalf("shortcut icon = %q, want = %q, index = %d, error = %v", icon, iconPath, index, err)
	}
	iconInfo, err := os.Stat(icon)
	if err != nil || !os.SameFile(iconInfo, wantIconInfo) {
		t.Fatalf("shortcut icon = %q, want = %q, index = %d, error = %v", icon, iconPath, index, err)
	}
}

func TestWriteShortcutIconForEveryImporter(t *testing.T) {
	const (
		imageIcon      = 1
		loadFromFile   = 0x10
		largeIconPixel = 256
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	loadImage, destroyIcon := user32.NewProc("LoadImageW"), user32.NewProc("DestroyIcon")

	dir := t.TempDir()
	for key := range importerPackages {
		image, err := shortcutIcons.Open("shortcut_icons/" + key + ".png")
		if err != nil {
			t.Fatalf("%s has no shortcut icon: %v", key, err)
		}
		config, err := png.DecodeConfig(image)
		_ = image.Close()
		if err != nil || config.Width != largeIconPixel || config.Height != largeIconPixel {
			t.Fatalf("%s shortcut icon = %dx%d, want 256x256, error = %v", key, config.Width, config.Height, err)
		}

		path, err := writeShortcutIcon(dir, strings.ToLower(key))
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, key+".ico"); path != want {
			t.Fatalf("%s shortcut icon path = %q, want %q", key, path, want)
		}
		pathPtr, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		handle, _, callErr := loadImage.Call(
			0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, largeIconPixel, largeIconPixel, loadFromFile,
		)
		if handle == 0 {
			t.Fatalf("Windows cannot load %s shortcut icon %q: %v", key, path, callErr)
		}
		_, _, _ = destroyIcon.Call(handle)

		// Creating the same shortcut again must leave the stored icon in place.
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writeShortcutIcon(dir, key); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) {
			t.Fatalf("%s shortcut icon was rewritten: %v", key, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(importerPackages) {
		t.Fatalf(
			"shortcut icon directory holds %d entries, want %d, error = %v",
			len(entries),
			len(importerPackages),
			err,
		)
	}
	if _, err := writeShortcutIcon(dir, "unknown"); err == nil {
		t.Fatal("unknown importer produced a shortcut icon")
	}
}

func TestConvertImporterIcon(t *testing.T) {
	dir := t.TempDir()

	// A wide JPEG keeps its aspect ratio and is centered between transparent bands. Its left half is red and its
	// right half blue, so where each half lands shows which way the image was turned.
	red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
	wide := image.NewRGBA(image.Rect(0, 0, 300, 100))
	draw.Draw(wide, wide.Bounds(), image.NewUniform(blue), image.Point{}, draw.Src)
	draw.Draw(wide, image.Rect(0, 0, 150, 100), image.NewUniform(red), image.Point{}, draw.Src)
	assertPixel := func(name string, icon image.Image, x, y int, want color.RGBA) {
		t.Helper()
		r, _, b, alpha := icon.At(x, y).RGBA()
		if alpha != 0xffff || (r > b) != (want == red) {
			t.Fatalf("%s: pixel (%d, %d) = red %d blue %d alpha %d, want opaque %v", name, x, y, r, b, alpha, want)
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, wide, nil); err != nil {
		t.Fatal(err)
	}
	wideJPEG := bytes.Clone(encoded.Bytes())
	jpegPath := filepath.Join(dir, "wide.jpg")
	if err := os.WriteFile(jpegPath, wideJPEG, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := convertImporterIcon(jpegPath)
	if err != nil {
		t.Fatal(err)
	}
	converted, err := png.Decode(bytes.NewReader(data))
	if err != nil || converted.Bounds() != image.Rect(0, 0, 256, 256) {
		t.Fatalf("converted icon = %v, error = %v", converted.Bounds(), err)
	}
	if _, _, _, alpha := converted.At(128, 10).RGBA(); alpha != 0 {
		t.Fatalf("band above the image has alpha %d, want transparent", alpha)
	}
	assertPixel("wide.jpg", converted, 64, 128, red)
	assertPixel("wide.jpg", converted, 192, 128, blue)
	assertWindowsLoadsIcon(t, filepath.Join(dir, "wide.ico"), data)

	encoded.Reset()
	if err := png.Encode(&encoded, wide); err != nil {
		t.Fatal(err)
	}
	widePNG := encoded.Bytes()

	// EXIF orientations 6 and 8 turn the stored wide image upright in opposite directions, so it is displayed tall
	// with the stored left half on top for 6 and at the bottom for 8. The JPEG marker carrying the orientation
	// follows a fill byte, and the PNG carries it in an eXIf chunk after IHDR.
	for _, orientation := range []byte{6, 8} {
		tiff := []byte{
			'I', 'I', 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00,
			0x01, 0x00, 0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, orientation, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		}
		app1 := append([]byte{0xff, 0xff, 0xe1, 0x00, byte(2 + 6 + len(tiff)), 'E', 'x', 'i', 'f', 0, 0}, tiff...)
		rotatedJPEG := append(append(append([]byte{}, wideJPEG[:2]...), app1...), wideJPEG[2:]...)

		const ihdrEnd = 8 + 4 + 4 + 13 + 4
		chunk := binary.BigEndian.AppendUint32(nil, uint32(len(tiff)))
		chunk = append(append(chunk, "eXIf"...), tiff...)
		chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
		rotatedPNG := append(append(append([]byte{}, widePNG[:ihdrEnd]...), chunk...), widePNG[ihdrEnd:]...)

		top, bottom := red, blue
		if orientation == 8 {
			top, bottom = blue, red
		}
		for extension, rotated := range map[string][]byte{"jpg": rotatedJPEG, "png": rotatedPNG} {
			name := fmt.Sprintf("orientation-%d.%s", orientation, extension)
			rotatedPath := filepath.Join(dir, name)
			if err := os.WriteFile(rotatedPath, rotated, 0o600); err != nil {
				t.Fatal(err)
			}
			if data, err = convertImporterIcon(rotatedPath); err != nil {
				t.Fatalf("convert %s: %v", name, err)
			}
			if converted, err = png.Decode(bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			if _, _, _, alpha := converted.At(10, 128).RGBA(); alpha != 0 {
				t.Fatalf("%s: band left of the upright image has alpha %d, want transparent", name, alpha)
			}
			assertPixel(name, converted, 128, 64, top)
			assertPixel(name, converted, 128, 192, bottom)
		}
	}

	// WebP decoding is registered by import side effect only.
	webpPath := filepath.Join(dir, "pixel.webp")
	pixel, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(webpPath, pixel, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := convertImporterIcon(webpPath); err != nil {
		t.Fatalf("convert WebP: %v", err)
	}

	// AVIF goes through its own decoder, which must still fit the image like the other formats.
	encoded.Reset()
	if err := avif.Encode(&encoded, wide, avif.Options{Lossless: true}); err != nil {
		t.Fatal(err)
	}
	avifPath := filepath.Join(dir, "wide.avif")
	if err := os.WriteFile(avifPath, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err = convertImporterIcon(avifPath); err != nil {
		t.Fatalf("convert AVIF: %v", err)
	}
	if converted, err = png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	assertPixel("wide.avif", converted, 64, 128, red)
	assertPixel("wide.avif", converted, 192, 128, blue)

	// An irot angle of 1 turns the image a quarter anticlockwise, which puts the stored left half at the bottom.
	rotatedPath := filepath.Join(dir, "rotated.avif")
	if err := os.WriteFile(rotatedPath, withAVIFRotation(t, encoded.Bytes(), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err = convertImporterIcon(rotatedPath); err != nil {
		t.Fatalf("convert rotated AVIF: %v", err)
	}
	if converted, err = png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, alpha := converted.At(10, 128).RGBA(); alpha != 0 {
		t.Fatalf("rotated.avif: band left of the upright image has alpha %d, want transparent", alpha)
	}
	assertPixel("rotated.avif", converted, 128, 64, blue)
	assertPixel("rotated.avif", converted, 128, 192, red)

	// Many AVIF files have the major brand mif1 and name avif only among the compatible brands.
	mif1 := bytes.Clone(encoded.Bytes())
	copy(mif1[8:12], "mif1")
	mif1Path := filepath.Join(dir, "mif1.avif")
	if err := os.WriteFile(mif1Path, mif1, 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err = convertImporterIcon(mif1Path); err != nil {
		t.Fatalf("convert AVIF with major brand mif1: %v", err)
	}
	if converted, err = png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	assertPixel("mif1.avif", converted, 64, 128, red)

	t.Run("rejects oversized AVIF sequence", func(t *testing.T) {
		// The primary item's ispe reports 1x1, but the sequence track has two 4097x4096 frames.
		path := filepath.Join("testdata", "oversized-avif-sequence.avif")
		if _, err := convertImporterIcon(path); err == nil ||
			!strings.HasPrefix(err.Error(), "XXMI_SHORTCUT_ICON_INVALID:") {
			t.Fatalf("convert oversized AVIF sequence error = %v", err)
		}
	})

	// An empty pitm box panics the AVIF box parser; the file is rejected instead of closing the application.
	malformed := filepath.Join(dir, "malformed.avif")
	if err := os.WriteFile(malformed, []byte{
		0, 0, 0, 16, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f', 0, 0, 0, 0,
		0, 0, 0, 36, 'm', 'e', 't', 'a', 0, 0, 0, 0,
		0, 0, 0, 8, 'p', 'i', 't', 'm',
		0, 0, 0, 16, 'i', 'p', 'r', 'p', 0, 0, 0, 8, 'i', 'p', 'c', 'o',
	}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := convertImporterIcon(malformed); err == nil ||
		!strings.HasPrefix(err.Error(), "XXMI_SHORTCUT_ICON_INVALID:") {
		t.Fatalf("convert malformed AVIF error = %v", err)
	}

	// A pixel count past the limit is rejected from the header, before the pixels are decoded.
	large := filepath.Join(dir, "large.png")
	encoded.Reset()
	if err := png.Encode(&encoded, image.NewGray(image.Rect(0, 0, 4097, 4097))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(large, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := convertImporterIcon(large); err == nil ||
		!strings.HasPrefix(err.Error(), "XXMI_SHORTCUT_ICON_INVALID:") {
		t.Fatalf("convert oversized image error = %v", err)
	}
}

func assertWindowsLoadsIcon(t *testing.T, path string, data []byte) {
	t.Helper()
	const (
		imageIcon    = 1
		loadFromFile = 0x10
	)
	if err := writeIconFile(path, data); err != nil {
		t.Fatal(err)
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, _, callErr := user32.NewProc("LoadImageW").Call(
		0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 256, 256, loadFromFile,
	)
	if handle == 0 {
		t.Fatalf("Windows cannot load icon %q: %v", path, callErr)
	}
	_, _, _ = user32.NewProc("DestroyIcon").Call(handle)
}

func TestUpdateExistingShortcut(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "GIMI Quick Start.lnk")
	first, err := writeShortcutIcon(dir, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	second, err := writeShortcutIcon(dir, "SRMI")
	if err != nil {
		t.Fatal(err)
	}

	if updated, err := updateExistingShortcut(context.Background(), path, first); err != nil || updated {
		t.Fatalf("update of a missing shortcut = %v, error = %v", updated, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing shortcut was created: %v", err)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, err := win.CoInitializeEx(co.COINIT_APARTMENTTHREADED | co.COINIT_DISABLE_OLE1DDE); err != nil {
		t.Fatal(err)
	}
	defer win.CoUninitialize()

	// A shortcut whose arguments the user edited keeps them; only the icon changes.
	const args = "--xxmi-launch GIMI --user-edited"
	if err := createWindowsShortcut(context.Background(), path, executable, args, first); err != nil {
		t.Fatal(err)
	}
	if updated, err := updateExistingShortcut(context.Background(), path, second); err != nil || !updated {
		t.Fatalf("update of an existing shortcut = %v, error = %v", updated, err)
	}
	releaser := win.NewOleReleaser()
	defer releaser.Release()
	link := loadShortcutLink(t, releaser, path)
	icon, _, err := link.GetIconLocation()
	if err != nil {
		t.Fatal(err)
	}

	// Shell links can store an alias of the icon path; compare file identity.
	iconInfo, err := os.Stat(icon)
	if err != nil {
		t.Fatalf("shortcut icon %q: %v", icon, err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(iconInfo, secondInfo) {
		t.Fatalf("shortcut icon = %q, want %q", icon, second)
	}
	if got, err := link.GetArguments(); err != nil || got != args {
		t.Fatalf("shortcut arguments = %q, want %q, error = %v", got, args, err)
	}
}

// loadShortcutLink reads the shortcut at path through IPersistFile.Load. The caller owns COM initialization.
func loadShortcutLink(t *testing.T, releaser *win.OleReleaser, path string) *winsh.IShellLink {
	t.Helper()
	var link *winsh.IShellLink
	if err := win.CoCreateInstance(releaser, &cosh.CLSID_ShellLink, nil, co.CLSCTX_INPROC_SERVER, &link); err != nil {
		t.Fatal(err)
	}
	var persist *shortcutPersistFile
	if err := link.QueryInterface(releaser, &persist); err != nil {
		t.Fatal(err)
	}
	if err := persist.call(persistFileLoad, path, 0); err != nil {
		t.Fatal(err)
	}
	return link
}

func TestGetImporterIconsFallsBackForDamagedIcon(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()
	service := New()
	service.UseClient(newXXMITestClient(t))
	dir, err := shortcutIconRoot()
	if err != nil {
		t.Fatal(err)
	}

	// SRMI keeps a valid custom icon, GIMI names one whose preview is gone, and ZZMI one whose preview no longer
	// matches its ID.
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	valid := hashBytes(encoded.Bytes())[:12]
	if err := writeShortcutFile(customImporterIconPNG(dir, "SRMI", valid), encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	const damaged = "0123456789ab"
	if err := writeShortcutFile(customImporterIconPNG(dir, "ZZMI", damaged), encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	for key, id := range map[string]string{"GIMI": "abcdef012345", "SRMI": valid, "ZZMI": damaged} {
		if err := service.client.Settings.Upsert(ctx, importerIconSettingKey(key), &id); err != nil {
			t.Fatal(err)
		}
	}

	icons, err := service.GetImporterIcons(ctx)
	if err != nil || len(icons) != len(importerPackages) {
		t.Fatalf("icons = %+v, error = %v", icons, err)
	}
	for _, icon := range icons {
		want := ImporterIcon{Key: icon.Key}
		switch icon.Key {
		case "SRMI":
			want.Custom, want.Path = true, customImporterIconPNG(dir, "SRMI", valid)
		case "GIMI", "ZZMI":
			want.Custom, want.Unavailable = true, true
		}
		if icon != want {
			t.Fatalf("%s icon = %+v, want %+v", icon.Key, icon, want)
		}
	}
}

// withAVIFRotation adds an irot property with angle to the primary item of an AVIF written by avif.Encode. The new
// bytes sit inside meta, ahead of mdat, so the item locations in iloc move by the same amount.
func withAVIFRotation(t *testing.T, data []byte, angle byte) []byte {
	t.Helper()
	type box struct{ start, payload, end int }
	children := func(from, to int) map[string]box {
		found := map[string]box{}
		for offset := from; offset+8 <= to; {
			size := int(binary.BigEndian.Uint32(data[offset:]))
			if size < 8 || offset+size > to {
				t.Fatalf("AVIF box at %d has size %d", offset, size)
			}
			found[string(data[offset+4:offset+8])] = box{offset, offset + 8, offset + size}
			offset += size
		}
		return found
	}
	meta, ok := children(0, len(data))["meta"]
	if !ok {
		t.Fatal("AVIF has no meta box")
	}
	inMeta := children(meta.payload+4, meta.end)
	iprp, iloc, pitm := inMeta["iprp"], inMeta["iloc"], inMeta["pitm"]
	inIprp := children(iprp.payload, iprp.end)
	ipco, ipma := inIprp["ipco"], inIprp["ipma"]
	if iprp.end == 0 || iloc.end == 0 || pitm.end == 0 || ipco.end == 0 || ipma.end == 0 {
		t.Fatalf("AVIF meta lacks iprp, iloc, pitm, ipco, or ipma: %v %v", inMeta, inIprp)
	}
	property := len(children(ipco.payload, ipco.end)) + 1
	primary := binary.BigEndian.Uint16(data[pitm.payload+4:])

	// Version 0 ipma with 7-bit property indices: item ID, association count, then one byte per association.
	if data[ipma.payload] != 0 || data[ipma.payload+3]&1 != 0 || property >= 0x80 {
		t.Fatal("ipma layout is not the one this helper writes")
	}
	association, count := -1, 0
	for entry, offset := 0, ipma.payload+8; entry < int(binary.BigEndian.Uint32(data[ipma.payload+4:])); entry++ {
		count = int(data[offset+2])
		if binary.BigEndian.Uint16(data[offset:]) == primary {
			association = offset
			break
		}
		offset += 3 + count
	}
	if association < 0 {
		t.Fatal("ipma has no entry for the primary item")
	}

	// Version 0 iloc with 4-byte extent offsets and no base offset.
	const grown = 9 + 1
	patched := bytes.Clone(data)
	if data[iloc.payload] != 0 || data[iloc.payload+4] != 0x44 || data[iloc.payload+5]>>4 != 0 {
		t.Fatal("iloc layout is not the one this helper writes")
	}
	for item, offset := 0, iloc.payload+8; item < int(binary.BigEndian.Uint16(data[iloc.payload+6:])); item++ {
		extents := int(binary.BigEndian.Uint16(data[offset+4:]))
		for extent := range extents {
			at := offset + 6 + extent*8
			binary.BigEndian.PutUint32(patched[at:], binary.BigEndian.Uint32(data[at:])+grown)
		}
		offset += 6 + extents*8
	}
	for _, header := range []struct {
		at, by int
	}{{meta.start, grown}, {iprp.start, grown}, {ipco.start, 9}, {ipma.start, 1}} {
		binary.BigEndian.PutUint32(patched[header.at:], binary.BigEndian.Uint32(data[header.at:])+uint32(header.by))
	}
	patched[association+2]++

	irot := []byte{0, 0, 0, 9, 'i', 'r', 'o', 't', angle}
	associationEnd := association + 3 + count
	rotated := append(bytes.Clone(patched[:ipco.end]), irot...)
	rotated = append(rotated, patched[ipco.end:associationEnd]...)
	rotated = append(rotated, 0x80|byte(property))
	return append(rotated, patched[associationEnd:]...)
}
