//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
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

func TestConvertShortcutIcon(t *testing.T) {
	dir := t.TempDir()

	// A wide JPEG keeps its aspect ratio and is centered between transparent bands.
	wide := image.NewRGBA(image.Rect(0, 0, 300, 100))
	draw.Draw(wide, wide.Bounds(), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, wide, nil); err != nil {
		t.Fatal(err)
	}
	jpegPath := filepath.Join(dir, "wide.jpg")
	if err := os.WriteFile(jpegPath, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := convertShortcutIcon(jpegPath)
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
	if red, _, _, alpha := converted.At(128, 128).RGBA(); alpha != 0xffff || red < 0xf000 {
		t.Fatalf("image center = red %d alpha %d, want opaque red", red, alpha)
	}
	assertWindowsLoadsIcon(t, filepath.Join(dir, "wide.ico"), data)

	// EXIF orientation 6 turns the stored wide image upright, so it is displayed tall. The JPEG marker carrying it
	// follows a fill byte, and the PNG carries it in an eXIf chunk after IHDR.
	tiff := []byte{
		'I', 'I', 0x2a, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00, 0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x06, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	app1 := append([]byte{0xff, 0xff, 0xe1, 0x00, byte(2 + 6 + len(tiff)), 'E', 'x', 'i', 'f', 0, 0}, tiff...)
	rotatedJPEG := append(append(append([]byte{}, encoded.Bytes()[:2]...), app1...), encoded.Bytes()[2:]...)

	encoded.Reset()
	if err := png.Encode(&encoded, wide); err != nil {
		t.Fatal(err)
	}
	const ihdrEnd = 8 + 4 + 4 + 13 + 4
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(tiff)))
	chunk = append(append(chunk, "eXIf"...), tiff...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	rotatedPNG := append(append(append([]byte{}, encoded.Bytes()[:ihdrEnd]...), chunk...), encoded.Bytes()[ihdrEnd:]...)

	for name, rotated := range map[string][]byte{"rotated.jpg": rotatedJPEG, "rotated.png": rotatedPNG} {
		rotatedPath := filepath.Join(dir, name)
		if err := os.WriteFile(rotatedPath, rotated, 0o600); err != nil {
			t.Fatal(err)
		}
		if data, err = convertShortcutIcon(rotatedPath); err != nil {
			t.Fatalf("convert %s: %v", name, err)
		}
		if converted, err = png.Decode(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		if _, _, _, alpha := converted.At(10, 128).RGBA(); alpha != 0 {
			t.Fatalf("%s: band left of the upright image has alpha %d, want transparent", name, alpha)
		}
		if _, _, _, alpha := converted.At(128, 10).RGBA(); alpha != 0xffff {
			t.Fatalf("%s: top of the upright image has alpha %d, want opaque", name, alpha)
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
	if _, err := convertShortcutIcon(webpPath); err != nil {
		t.Fatalf("convert WebP: %v", err)
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
	if _, err := convertShortcutIcon(large); err == nil ||
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
	if err != nil || !strings.EqualFold(filepath.Base(icon), "SRMI.ico") {
		t.Fatalf("shortcut icon = %q, want %q, error = %v", icon, second, err)
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
